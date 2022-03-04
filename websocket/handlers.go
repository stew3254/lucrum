package websocket

import (
	"context"
	"encoding/json"
	"github.com/stew3254/ratelimit"
	"gorm.io/gorm"
	"log"
	"lucrum/config"
	"lucrum/database"
	"lucrum/lib"
	"net/http"
	"sync"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"
)

// Handle status messages
func HandleStatus(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// Handle heartbeat messages
func HandleHeartbeat(msg coinbasepro.Message) {
	// Do nothing because it's simply so the websocket doesn't close
}

// Handle ticker messages
func HandleTicker(msg coinbasepro.Message) {
	log.Println(msg.Price)
}

// Handle matches from level 2 order book
func HandleMatches(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// L2Handler handles data from the level 2 order book
func L2Handler(
	ctx context.Context,
	wg *sync.WaitGroup,
	msgChannel chan coinbasepro.Message,
) {
	defer func() {
		log.Println("Closed L2 handler")
		wg.Done()
	}()

	// Ignore the initial message sent saying we've started listening or close since interrupted
	select {
	case _, ok := <-msgChannel:
		if !ok {
			return
		}
	case <-ctx.Done():
		return
	}

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
		case <-ctx.Done():
			return
		}
	}
}

// L3Handler handles level 3 order book data
func L3Handler(
	ctx context.Context,
	wg *sync.WaitGroup,
	msgChannel chan coinbasepro.Message,
	productIds []string,
) {
	// Get the configuration
	cli := ctx.Value(lib.LucrumKey("conf")).(config.Configuration).CLI
	wsConf := ctx.Value(lib.LucrumKey("conf")).(config.Configuration).Type.Ws

	// Tell the parent when we're done
	defer func() {
		if cli.Verbose {
			log.Println("Cleaned up L3 Handler")
		}
		wg.Done()
	}()

	// Initialize subscription channels
	SubChans = NewChannels(productIds)

	// Get the coinbase client or create one if it doesn't exist
	maybeClient := ctx.Value(lib.LucrumKey("client"))
	var client *coinbasepro.Client
	if maybeClient == nil {
		botConf := ctx.Value(lib.LucrumKey("botConf")).(config.Coinbase)
		client = &coinbasepro.Client{
			BaseURL:    botConf.URL,
			Key:        botConf.Key,
			Secret:     botConf.Secret,
			Passphrase: botConf.Passphrase,
			HTTPClient: &http.Client{
				Timeout: 60 * time.Second,
			},
			RetryCount: 3,
		}
	} else {
		client = maybeClient.(*coinbasepro.Client)
	}

	// Create a RateLimiter to not overwhelm the API
	rl := ratelimit.NewRateLimiter(
		10,
		10,
		100*time.Millisecond,
		time.Second,
	)

	// Get the database
	db := ctx.Value(lib.LucrumKey("db")).(*gorm.DB)

	// Look for the first message saying we're listening for messages
	// Must do this before getting the order book, so we don't miss messages
	// We can skip old ones, but you can't make up missed ones
	select {
	case _, ok := <-msgChannel:
		if !ok {
			return
		}
	case <-ctx.Done():
		return
	}

	if cli.Verbose {
		log.Println("Getting order book")
	}
	// Get the order books
	lib.GetOrderBooks(&lib.Books, client, rl, productIds)
	// Add all entries to the database
	lib.Books.Save(db, true)
	if cli.Verbose {
		log.Println("Order book created")
	}

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
	for _, productId := range productIds {
		seq := lib.Books.Get(productId, true).GetSequence(true)
		lastSequence[productId] = seq
		aggregateSequence[productId] = seq
		readerSequence[productId] = seq
	}

	// Aggregate transactions and save them to the database.DB
	aggregateAlertChannel := make(chan struct{}, 1)

	// Spin off aggregate as a separate goroutine to listen to incoming messages
	wg.Add(1)
	go AggregateTransactions(ctx, wg, aggregateAlertChannel, productIds, aggregateSequence)

	// Function to update the order book and possibly update transactions
	handleMsg := func(
		ctx context.Context,
		wg *sync.WaitGroup,
		openTransactions *lib.Transactions,
		doneTransactions *lib.Transactions,
		msgChan <-chan coinbasepro.Message,
		lastSequence int64,
	) {
		bufSize := 500
		var msgBuf []database.L3OrderMessage
		// Queue messages in a buffer before storing them in bulk
		if wsConf.RawMessages {
			msgBuf = make([]database.L3OrderMessage, bufSize)
		}
		done := func(count int) {
			// Save leftover messages to the database
			if wsConf.RawMessages && count > 0 {
				buf := msgBuf[:count]
				db.Save(&buf)
			}
			if cli.Verbose {
				log.Println("L3 msg consumer finished")
			}
			// Alert the parent we're done
			wg.Done()
		}

		if cli.Verbose {
			log.Println("Handling L3 messages now")
		}
		// Listen to messages forever
		count := 0
		for {
			select {
			case msg, ok := <-msgChan:
				// If the channel is closed just return
				if !ok {
					done(count)
					return
				}

				// TODO add proper handling for out of order messages
				// This accounts for gaps in the sequence
				if msg.Sequence > lastSequence+1 {
					// Add all entries to the database because we can't account for the gap that occurred
					lib.GetOrderBooks(&lib.Books, client, rl, productIds)
					// Add all entries to the database
					lib.Books.Save(db, true)
					// Fix last sequence
					lastSequence = msg.Sequence
				} else if msg.Sequence == lastSequence+1 {
					// Update the sequence
					lastSequence++

					// Save the message to the database if we want raw messages
					if wsConf.RawMessages {
						m := database.ToOrderMessage(msg)
						msgBuf[count] = m
						count = (count + 1) % bufSize
						if count == 0 {
							// Save the messages to the database
							db.Save(&msgBuf)
						}
					}

					// Update the order book
					lib.UpdateOrderBook(lib.Books.Get(msg.ProductID, true), msg)

					// Update the transactions
					if wsConf.StoreTransactions {
						lib.UpdateTransactions(openTransactions, doneTransactions, msg)
					}
				}
			// Simply ignore old messages

			case <-ctx.Done():
				done(count)
				return
			}
		}
	}

	// Spin off the goroutines to listen per product id
	for _, productId := range productIds {
		// Subscribe to the channel in this thread, so we know it's been added before spinning
		// off the goroutine and then the MsgReader
		subChan := SubChans.Add(productId)
		wg.Add(1)
		go handleMsg(ctx, wg, openTransactions, doneTransactions, subChan, lastSequence[productId])
	}

	// Make sure the aggregate function is running and ready to listen for incoming messages
	// This way no messages get dropped
	select {
	case _, ok := <-aggregateAlertChannel:
		if !ok {
			return
		}
	case <-ctx.Done():
		return
	}

	// Clean up stuff since we don't need it anymore
	lastSequence = nil
	aggregateSequence = nil
	aggregateAlertChannel = nil

	// TODO find a better place to put this
	// Now we're ready to read out all messages
	wg.Add(1)
	MsgReader(ctx, wg, msgChannel, readerSequence)
}

// UserHandler handles full messages only concerning the authenticated user
func UserHandler(
	ctx context.Context,
	wg *sync.WaitGroup,
	msgChannel chan coinbasepro.Message,
) {
	defer func() {
		log.Println("Closed user handler")
		wg.Done()
	}()

	// Ignore the initial message sent saying we've started listening or close since interrupted
	select {
	case _, ok := <-msgChannel:
		if !ok {
			return
		}
	case <-ctx.Done():
		return
	}

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
		case <-ctx.Done():
			return
		}
	}
}
