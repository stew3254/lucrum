package websocket

import (
	"container/list"
	"encoding/json"
	"github.com/stew3254/ratelimit"
	"log"
	"lucrum/database"
	"time"

	"gorm.io/gorm"

	"github.com/preichenberger/go-coinbasepro/v2"
)

var Books map[string]Book

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

// UpdateOrderBook handles interpreting messages from the full channel
// and applying them to the internal order book
func UpdateOrderBook(book *Book, msg coinbasepro.Message) {
	// Ignore old messages
	if msg.Sequence <= book.Sequence {
		return
	} else if msg.Sequence > book.Sequence+1 {
		// The messages are too new, so we missed something. Get a snapshot from the API
	}
	// Handle based on the message type
	switch msg.Type {
	case "open":
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

		// Add the order to the front of the book
		if msg.Side == "buy" {
			entry.IsAsk = false
			book.Buys.PushFront(entry)
		} else {
			entry.IsAsk = true
			book.Sells.PushFront(entry)
		}
	case "done":
		// Get the entries we care about
		var entries *list.List
		if msg.Side == "buy" {
			entries = book.Buys
		} else {
			entries = book.Sells
		}

		// Remove the entry from the book if it exists
		for e := entries.Front(); e != nil; e = e.Next() {
			if e.Value.(database.OrderBookSnapshot).OrderID == msg.OrderID {
				// Remove the entry
				entries.Remove(e)
				break
			}
		}
	case "change":
		// Get the entries we care about
		var entries *list.List
		if msg.Side == "buy" {
			entries = book.Buys
		} else {
			entries = book.Sells
		}

		// Look through the order book to see if it's changing a resting order
		for e := entries.Front(); e != nil; e = e.Next() {
			v := e.Value.(database.OrderBookSnapshot)
			if v.OrderID == msg.OrderID {
				// Update the new size
				v.Size = msg.NewSize
				break
			}
			// Update the value again
			e.Value = v
		}
	}

	// Update the book's sequence number, so we know what the last message seen is
	book.Sequence = msg.Sequence
}

// SaveOrderBook writes out the book to the database
func SaveOrderBook(book *Book, db *gorm.DB) {
	update := func(l *list.List, sequence int64) {
		for e := l.Front(); e != nil; e = e.Next() {
			v := e.Value.(database.OrderBookSnapshot)
			v.Sequence = sequence
			e.Value = v
		}
		obListToDB(l, db, 10000)
	}
	update(book.Buys, book.Sequence)
	update(book.Sells, book.Sequence)
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
	books := getOrderBooks(client, rl, productIds)
	// Add all entries to the database
	for _, book := range books {
		obListToDB(book.Buys, db, 10000)
		obListToDB(book.Sells, db, 10000)
	}

	// Forever look for incoming messages
	var lastSequence int64
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
				books = getOrderBooks(client, rl, productIds)
				// Add all entries to the database
				for _, book := range books {
					obListToDB(book.Buys, db, 10000)
					obListToDB(book.Sells, db, 10000)
				}
			} else if msg.Sequence == lastSequence+1 {
				// Update the sequence
				lastSequence = msg.Sequence

				// Save the message to the database
				m := database.ToOrderMessage(msg)
				db.Create(&m)

				// Update the order book
				UpdateOrderBook(books[msg.ProductID], msg)

				switch msg.Type {
				case "received":
				case "open":
				case "done":
				case "match":
				case "change":
				case "activate":
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
