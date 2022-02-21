package websocket

import (
	"encoding/json"
	"github.com/stew3254/ratelimit"
	"log"
	"lucrum/config"
	"lucrum/database"
	"net/http"
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
	wsConf config.Websocket,
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
	go Books.Save(db)

	// Initialize transactions
	var openTransactions *Transactions
	var doneTransactions *Transactions
	if wsConf.StoreTransactions {
		openTransactions = NewTransactions(productIds, Books)
		doneTransactions = NewTransactions(productIds, nil)
	}

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
				if wsConf.RawMessages {
					m := database.ToOrderMessage(msg)
					go db.Create(&m)
				}

				// Update the order book
				UpdateOrderBook(Books.Get(msg.ProductID), msg)

				// Update the transactions
				if wsConf.StoreTransactions {
					UpdateTransactions(openTransactions, doneTransactions, msg)
				}
			}
			// Simply ignore old messages
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
