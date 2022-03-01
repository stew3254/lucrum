package websocket

import (
	"encoding/json"
	"github.com/stew3254/ratelimit"
	"log"
	"lucrum/config"
	"lucrum/database"
	"lucrum/lib"
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
	stop chan struct{},
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
		case <-stop:
			return
		}
	}
}

// L3Handler handles level 3 order book data
func L3Handler(
	botConf config.Bot,
	wsConf config.Websocket,
	db *gorm.DB,
	msgChannel chan coinbasepro.Message,
	stop <-chan struct{},
	productIds []string,
) {
	// Initialize subscription channels
	SubChans = NewChannels(productIds)

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

	// Look for the first message saying we're listening for messages
	// Must do this before getting the order book, so we don't miss messages
	// We can skip old ones, but you can't make up missed ones
	<-msgChannel

	// Get the order books
	lib.GetOrderBooks(&lib.Books, client, rl, productIds)
	// Add all entries to the database
	go lib.Books.Save(db, true)

	var openTransactions *lib.Transactions
	var doneTransactions *lib.Transactions
	// Initialize transactions since we care about them
	if wsConf.StoreTransactions {
		openTransactions = lib.NewTransactions(productIds, lib.Books)
		doneTransactions = lib.NewTransactions(productIds, nil)
	}

	// Initialize sequences and channels
	lastSequence := make(map[string]int64)
	aggregateSequence := make(map[string]int64)
	readerSequence := make(map[string]int64)
	stopChans := make(map[string]chan struct{})
	for _, productId := range productIds {
		seq := lib.Books.Get(productId, true).GetSequence(true)
		lastSequence[productId] = seq
		aggregateSequence[productId] = seq
		readerSequence[productId] = seq
		stopChans[productId] = make(chan struct{}, 1)
	}

	// Aggregate transactions and save them to the db
	aggregateStop := make(chan struct{}, 1)
	aggregateAlertChannel := make(chan struct{}, 1)
	readerStop := make(chan struct{}, 1)
	// Spin off aggregate as a separate goroutine to listen to incoming messages
	go AggregateTransactions(wsConf, db, aggregateStop, aggregateAlertChannel, productIds, aggregateSequence)

	// Function to update the order book and possibly update transactions
	handleMsg := func(
		client *coinbasepro.Client,
		rl *ratelimit.RateLimiter,
		openTransactions *lib.Transactions,
		doneTransactions *lib.Transactions,
		msgChan <-chan coinbasepro.Message,
		stop chan struct{},
		lastSequence int64,
	) {
		// Listen to messages forever
		for {
			select {
			case msg, ok := <-msgChan:
				// If the channel is closed just return
				if !ok {
					return
				}

				// TODO add proper handling for out of order messages
				// This accounts for gaps in the sequence
				if msg.Sequence > lastSequence+1 {
					// Add all entries to the database because we can't account for the gap that occurred
					lib.GetOrderBooks(&lib.Books, client, rl, productIds)
					// Add all entries to the database
					go lib.Books.Save(db, true)
					// Fix last sequence
					lastSequence = msg.Sequence
				} else if msg.Sequence == lastSequence+1 {
					// Update the sequence
					lastSequence++

					// Save the message to the database if we want raw messages
					if wsConf.RawMessages {
						m := database.ToOrderMessage(msg)
						go db.Create(&m)
					}

					// Update the order book
					lib.UpdateOrderBook(lib.Books.Get(msg.ProductID, true), msg)

					// Update the transactions
					if wsConf.StoreTransactions {
						lib.UpdateTransactions(openTransactions, doneTransactions, msg)
					}
				}
			// Simply ignore old messages

			// If told to stop, make sure to end
			case <-stop:
				return
			}
		}
	}

	// Spin off the goroutines to listen per product id
	for _, productId := range productIds {
		// Subscribe to the subchan in this thread, so we know it's been added before spinning
		// off the goroutine and then the MsgReader
		subChan := SubChans.Add(productId)
		go handleMsg(
			client,
			rl,
			openTransactions,
			doneTransactions,
			subChan,
			stopChans[productId],
			lastSequence[productId],
		)
	}

	// Make sure the aggregate function is running and ready to listen for incoming messages
	// This way no messages get dropped
	<-aggregateAlertChannel

	// Clean up stuff since we don't need it anymore
	lastSequence = nil
	aggregateSequence = nil
	aggregateAlertChannel = nil

	// Now we're ready to start reading out messages
	go MsgReader(msgChannel, readerSequence, readerStop)

	// Wait on the stop message to alert all the other child goroutines
	select {
	case <-stop:
		aggregateStop <- struct{}{}
		readerStop <- struct{}{}
		close(aggregateStop)
		close(readerStop)
		// Tell all the other goroutines to close
		for _, productId := range productIds {
			stopChans[productId] <- struct{}{}
			close(stopChans[productId])
		}
		return
	}
}

// UserHandler handles full messages only concerning the authenticated user
func UserHandler(
	db *gorm.DB,
	msgChannel chan coinbasepro.Message,
	stop <-chan struct{},
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
		case <-stop:
			return
		}
	}
}
