package websocket

import (
	"container/list"
	"encoding/json"
	"github.com/stew3254/ratelimit"
	"gorm.io/gorm/clause"
	"log"
	"lucrum/database"
	"sync"
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

func getOrderBooks(
	client *coinbasepro.Client,
	rl *ratelimit.RateLimiter,
	productIds []string,
) (snapshot map[string]*list.List) {
	wg := &sync.WaitGroup{}
	books := make([]coinbasepro.Book, 0)
	for _, product := range productIds {
		// Wait for each order book
		wg.Add(1)
		go func(product string) {
			rl.Lock()
			book, err := client.GetBook(product, 3)
			rl.Unlock()
			for err != nil && err.Error() == "Public rate limit exceeded" {
				// Bump up the limit
				rl.Increase()
				// Be brief on the critical section (although in 1 thread right now this doesn't matter)
				rl.Lock()
				// Get the historic rates
				book, err = client.GetBook(product, 3)
				rl.Unlock()
			}
			if err == nil {
				rl.Decrease()
			} else {
				log.Fatalln(err)
			}
			rl.Lock()
			books = append(books, book)
			rl.Unlock()
			wg.Done()
		}(product)
	}

	// Wait for all the order books to be populated
	wg.Wait()

	// Create a slice of snapshot entries to add to the database
	now := time.Now()
	snapshot = make(map[string]*list.List)
	for i := 0; i < len(productIds); i++ {
		// Create the function to create a snapshot
		toSnapshot := func(isAsk bool, entry coinbasepro.BookEntry) database.OrderBookSnapshot {
			return database.OrderBookSnapshot{
				ProductId: productIds[i],
				Sequence:  books[i].Sequence,
				IsAsk:     isAsk,
				Time:      now,
				Price:     entry.Price,
				Size:      entry.Size,
				OrderID:   entry.OrderID,
			}
		}
		// Add all the asks and bids for this coin to the slice
		entries := list.New()
		for _, ask := range books[i].Asks {
			entries.PushFront(toSnapshot(true, ask))
		}
		for _, bid := range books[i].Bids {
			entries.PushFront(toSnapshot(false, bid))
		}
		// Add the list to the map
		snapshot[productIds[i]] = entries
	}
	return snapshot
}

// Gradually converts an order book list to a slice and adds it to the database
func obListToDB(l *list.List, db *gorm.DB, size int) {
	// Convert the list to a slice
	e := l.Front()
	entries := make([]database.OrderBookSnapshot, size)
	for {
		for i := 0; i < size; i++ {
			// There are no entries left so add remainders and break out of the loop
			if e == nil {
				db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "order_id"}},
					DoNothing: true,
				}).Create(entries)
				return
			}

			entries[i] = e.Value.(database.OrderBookSnapshot)
			e = e.Next()
		}
		// Add entries to the database
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "order_id"}},
			DoNothing: true,
		}).Create(entries)
	}
}

// L3Handler handles level 3 order book data
func L3Handler(
	db *gorm.DB,
	msgChannel chan coinbasepro.Message,
	productIds []string,
) {
	// Look for the first message saying we're listening for messages
	<-msgChannel
	// Now we can get the current state of the order books
	client := coinbasepro.NewClient()

	// Create a RateLimiter to not overwhelm the API
	rl := ratelimit.NewRateLimiter(
		10,
		10,
		100*time.Millisecond,
		time.Second,
	)

	// Get the order books
	snapshot := getOrderBooks(client, rl, productIds)
	// Add all entries to the database
	for _, v := range snapshot {
		obListToDB(v, db, 10000)
	}

	// Forever look for updates
	var lastSequence int64
	received := make(map[string]coinbasepro.Message)

	for {
		select {
		case msg := <-msgChannel:
			// Make sure to set the initial sequence on start
			if lastSequence == 0 {
				lastSequence = msg.Sequence - 1
			}

			// TODO add proper handling for out of order messages
			// This accounts for gaps in the sequence
			if msg.Sequence > lastSequence+1 {
				// Add all entries to the database because we can't account for the gap that occurred
				snapshot = getOrderBooks(client, rl, productIds)
				// Add all entries to the database
				for _, v := range snapshot {
					obListToDB(v, db, 10000)
				}
			} else if msg.Sequence == lastSequence+1 {
				// Update the sequence
				lastSequence = msg.Sequence

				// Save the message to the database
				m := database.ToOrderMessage(msg)
				db.Create(&m)

				// Handle based on the message type
				switch msg.Type {
				case "received":
					// Add the message to the received map, so we can match with it later
					received[msg.OrderID] = msg
				case "open":
					// Note: If the size of received and remaining size of the open message aren't the same,
					// then the order was partially filled

					// Create the order book snapshot
					now := time.Now()
					entry := database.OrderBookSnapshot{
						ProductId: msg.ProductID,
						Sequence:  msg.Sequence,
						Time:      now,
						Price:     msg.Price,
						Size:      msg.RemainingSize,
						OrderID:   msg.OrderID,
					}
					if msg.Side == "buy" {
						entry.IsAsk = false
					} else {
						entry.IsAsk = true
					}

					// Add the entry to the book
					snapshot[msg.ProductID].PushFront(entry)
				case "done":
					// Remove the received message since this has been filled
					delete(received, msg.OrderID)

					// Find the message on the order book if it's there and delete it
					entries := snapshot[msg.ProductID]
					for e := entries.Front(); e != nil; e = e.Next() {
						if e.Value.(database.OrderBookSnapshot).OrderID == msg.OrderID {
							// Remove the entry
							entries.Remove(e)
							log.Println("Removed:", e.Value.(database.OrderBookSnapshot).OrderID)
							break
						}
					}
				case "match":
					// Do nothing for now. Done takes care of removing the received messages and removing
					// from the order book
				case "change":
					// Look through the order book to see if it's changing a resting order
					entries := snapshot[msg.ProductID]
					for e := entries.Front(); e != nil; e = e.Next() {
						v := e.Value.(database.OrderBookSnapshot)
						if v.OrderID == msg.OrderID {
							// Update the new size
							v.Size = msg.NewSize
							// If we care about limit vs market price check here
							break
						}
					}
				case "activate":
					// Do nothing for now. We don't care about stop orders, and they don't trigger the book
					// through this type of message
					// log.Println(msg)
				}
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
