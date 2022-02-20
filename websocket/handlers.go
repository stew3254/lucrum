package websocket

import (
	"container/list"
	"encoding/json"
	"github.com/stew3254/ratelimit"
	"log"
	"lucrum/config"
	"lucrum/database"
	"net/http"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/preichenberger/go-coinbasepro/v2"
)

// Handle status messages
func HandleStatus(db *gorm.DB, msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// Handle heartbeat messages
func HandleHeartbeat(db *gorm.DB, msg coinbasepro.Message) {
	// Do nothing because it's simply so the websocket doesn't close
}

// Handle ticker messages
func HandleTicker(db *gorm.DB, msg coinbasepro.Message) {
	log.Println(msg.Price)
}

// Handle matches from level 2 order book
func HandleMatches(db *gorm.DB, msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// L2Handler handles data from the level 2 order book
func L2Handler(
	db *gorm.DB,
	msgChannel chan coinbasepro.Message,
) {
	// Ignore the initial message sent saying we've started listening
	<-msgChannel

	// Forever look for updates
	var lastSequence int64
	for {
		select {
		case msg := <-msgChannel:
			// Is this a mistake to assume it can't be 0?
			// This fixes the bug with level2 channels not using sequences
			if msg.Sequence == 0 || msg.Sequence > lastSequence {
				out, err := json.Marshal(msg)
				if err != nil {
					log.Println(err)
				}
				log.Println(string(out))
			}
		}
	}
}

// L3Handler handles level 3 order book data
func L3Handler(
	botConf config.Bot,
	db *gorm.DB,
	msgChannel chan coinbasepro.Message,
	productIds []string,
) {
	// Look for the first message saying we're listening for messages
	<-msgChannel

	// Now we can get the current state of the order books
	client := &coinbasepro.Client{
		BaseURL:    botConf.Coinbase.URL,
		Key:        botConf.Coinbase.Key,
		Secret:     botConf.Coinbase.Secret,
		Passphrase: botConf.Coinbase.Passphrase,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		RetryCount: 3,
	}

	// Create a RateLimiter to not overwhelm the API
	rl := ratelimit.NewRateLimiter(
		10,
		10,
		100*time.Millisecond,
		time.Second,
	)

	// Get the order books
	Books = GetOrderBooks(client, rl, productIds)
	// Add all entries to the database
	Books.Save(db)

	// Initialize transactions
	openTransactions := NewTransactions(productIds)
	doneTransactions := NewTransactions(productIds)

	// Add transactions from the order book
	for _, productId := range productIds {
		book := Books.Get(productId)
		addTransactions := func(l *list.List) {
			for e := l.Front(); e != nil; e = e.Next() {
				v := e.Value.(database.OrderBookSnapshot)
				transaction := &database.Transaction{
					OrderID:       v.OrderID,
					ProductId:     v.ProductId,
					Time:          v.Time,
					Price:         v.Price,
					Side:          "buy",
					Size:          v.Size,
					AddedToBook:   true,
					RemainingSize: v.Size,
				}
				openTransactions.Set(v.ProductId, v.OrderID, transaction)
			}
		}
		addTransactions(book.Buys)
		addTransactions(book.Sells)
	}

	// Periodically done transactions to the db
	// go func(transactions *Transactions, db *gorm.DB) {
	// 	// Once a minute flush done transactions to the db
	// 	time.Sleep(60 * time.Second)
	// 	for _, productId := range productIds {
	// 		tmp := transactions.Pop(productId)
	// 		db.Create(&tmp)
	// 	}
	// }(doneTransactions, db)

	count := 0
	// Initialize sequences
	lastSequence := make(map[string]int64)
	for _, productId := range productIds {
		lastSequence[productId] = Books.Get(productId).Sequence
	}
	// Forever look for incoming messages
	for {
		select {
		case msg := <-msgChannel:
			// TODO add proper handling for out of order messages
			// This accounts for gaps in the sequence
			if msg.Sequence > lastSequence[msg.ProductID]+1 {
				// Add all entries to the database because we can't account for the gap that occurred
				Books = GetOrderBooks(client, rl, productIds)
				// Add all entries to the database
				Books.Save(db)
			} else if msg.Sequence == lastSequence[msg.ProductID]+1 {
				// Update the sequence
				lastSequence[msg.ProductID] = msg.Sequence

				// Save the message to the database
				m := database.ToOrderMessage(msg)
				db.Create(&m)

				// Update the order book
				UpdateOrderBook(Books.Get(msg.ProductID), msg)

				switch msg.Type {
				case "received":
					// Create the new transaction
					openTransactions.Set(msg.ProductID, msg.OrderID, &database.Transaction{
						OrderID:      msg.OrderID,
						ProductId:    msg.ProductID,
						Time:         msg.Time.Time().UnixMicro(),
						Price:        msg.Price,
						Side:         msg.Side,
						Size:         msg.Size,
						Funds:        msg.Funds,
						OrderType:    msg.OrderType,
						AddedToBook:  false,
						OrderChanged: false,
					})
				case "open":
					// Update the old transaction
					openTransactions.Update(msg.ProductID, msg.OrderID,
						func(transaction *database.Transaction) {
							transaction.AddedToBook = true
							transaction.RemainingSize = msg.RemainingSize
						})
				case "done":
					// Update the old transaction
					t := openTransactions.Get(msg.ProductID, msg.OrderID)
					t.ClosedAt = msg.Time.Time().UnixMicro()
					t.Reason = msg.Reason
					// Add it to done transactions
					doneTransactions.Set(msg.ProductID, msg.OrderID, t)
					// Remove it from the open transactions
					openTransactions.Remove(msg.ProductID, msg.OrderID)
				case "match":
					openTransactions.Update(msg.ProductID, msg.TakerOrderID,
						func(transaction *database.Transaction) {
							transaction.MatchId = msg.MakerOrderID
						})
					openTransactions.Update(msg.ProductID, msg.MakerOrderID,
						func(transaction *database.Transaction) {
							transaction.MatchId = msg.TakerOrderID
						})
				case "change":
					openTransactions.Update(msg.ProductID, msg.OrderID,
						func(transaction *database.Transaction) {
							transaction.OrderChanged = true
							transaction.NewSize = msg.NewSize
						})
				case "activate":
					// We can't actually do anything with this
				}

				// DEBUG
				if count%1000 == 0 {
					log.Println(count / 1000)
				}

				if count == 100000 {
					// Flush out all the transactions that have occurred
					for _, productId := range productIds {
						tmp := openTransactions.Pop(productId)
						db.Create(&tmp)
						tmp = doneTransactions.Pop(productId)
						db.Create(&tmp)
					}
					os.Exit(0)
				}
				count++
			}
			// Simply ignore old messages if this somehow ever occurred
		}
	}
}

// UserHandler handles full messages only concerning the authenticated user
func UserHandler(
	db *gorm.DB,
	msgChannel chan coinbasepro.Message,
) {
	// Forever look for updates
	var lastSequence int64
	for {
		select {
		case msg := <-msgChannel:
			// Is this a mistake to assume it can't be 0?
			// This fixes the bug with level2 channels not using sequences
			if msg.Sequence == 0 || msg.Sequence > lastSequence {
				out, err := json.Marshal(msg)
				if err != nil {
					log.Println(err)
				}
				log.Println(string(out))
			}
		}
	}
}
