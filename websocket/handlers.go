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
	stop chan struct{},
) {
	defer close(stop)

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
	stop chan struct{},
	productIds []string,
) {
	// Initialize subscription channels
	SubChans = NewChannels(productIds)

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
	openTransactions := NewTransactions(productIds, Books)
	doneTransactions := NewTransactions(productIds, nil)

	// Initialize sequences and channels
	lastSequence := make(map[string]int64)
	stopChans := make(map[string]chan struct{})
	for _, productId := range productIds {
		lastSequence[productId] = Books.Get(productId).Sequence
		stopChans[productId] = make(chan struct{}, 1)
	}

	// Aggregate transactions and save them to the db
	aggregateSequence := make(map[string]int64)
	for k, v := range lastSequence {
		aggregateSequence[k] = v
	}
	aggregateStop := make(chan struct{}, 1)
	aggregateAlertChannel := make(chan struct{}, 1)
	readerStop := make(chan struct{}, 1)

	go AggregateTransactions(wsConf, db, aggregateStop, aggregateAlertChannel, productIds, aggregateSequence)

	// Forever look for incoming messages
	update := func(
		client *coinbasepro.Client,
		rl *ratelimit.RateLimiter,
		openTransactions *Transactions,
		doneTransactions *Transactions,
		msgChan <-chan coinbasepro.Message,
		stop chan struct{},
		lastSequence int64,
	) {
		// Clean up stop channel
		defer close(stop)

		log.Println("Going to send message")

		log.Println("Sent message")

		// Listen to messages forever
		for {
			select {
			case msg, ok := <-msgChan:
				if !ok {
					return
				}

				// TODO add proper handling for out of order messages
				// This accounts for gaps in the sequence
				if msg.Sequence > lastSequence+1 {
					// Add all entries to the database because we can't account for the gap that occurred
					Books = GetOrderBooks(client, rl, productIds)
					// Add all entries to the database
					Books.Save(db)
					// Fix last sequence
					lastSequence = msg.Sequence
				} else if msg.Sequence == lastSequence+1 {
					// Update the sequence
					lastSequence = msg.Sequence

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

			// If told to stop, make sure to end
			case <-stop:
				return
			}
		}
	}

	// Spin off the goroutines to listen per product id
	for _, productId := range productIds {
		subChan := SubChans.Add(productId)
		go update(
			client,
			rl,
			openTransactions,
			doneTransactions,
			subChan,
			stopChans[productId],
			lastSequence[productId],
		)
	}

	// Alert this function the aggregate has subscribed using the stop chan
	<-aggregateAlertChannel
	close(aggregateAlertChannel)

	// Clean up this map since we don't need it
	lastSequence = nil
	aggregateSequence = nil
	aggregateAlertChannel = nil

	// Now we're ready to start reading out messages
	go MsgReader(msgChannel, readerStop)

	// Wait on the stop message to alert all the other child goroutines
	select {
	case <-stop:
		close(stop)
		aggregateStop <- struct{}{}
		readerStop <- struct{}{}
		// Tell all the other goroutines to close
		for _, productId := range productIds {
			stopChans[productId] <- struct{}{}
		}
		return
	}
}

// UserHandler handles full messages only concerning the authenticated user
func UserHandler(
	db *gorm.DB,
	msgChannel chan coinbasepro.Message,
	stop chan struct{},
) {
	defer close(stop)
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
