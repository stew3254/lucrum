package main

import (
	"container/list"
	"context"
	"log"
	"lucrum/config"
	"lucrum/database"
	"lucrum/lib"
	"lucrum/websocket"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/stew3254/ratelimit"
	"gorm.io/gorm"
)

// DB is a global DB connection to be shared
var DB *gorm.DB

// Used for testing stuff with the bot for now
// I'll fill this in as I want to test stuff
func testingStuff(client *coinbasepro.Client, conf config.Config) {
	// Get the sequence ids
	sequences := make([]int64, 0, 2)
	DB.Distinct("sequence").Table("order_book_snapshots").Scan(&sequences)
	tempBooks := make([][]database.OrderBookSnapshot, 2)
	books := make([]*websocket.Book, 2)
	// Get both order books
	for i := 0; i < 2; i++ {
		DB.Where("sequence = ?", sequences[i]).Find(&tempBooks[i])
		books[i] = &websocket.Book{
			Sequence: tempBooks[i][0].Sequence,
			Buys:     list.New(),
			Sells:    list.New(),
		}

		// Make real books
		for _, book := range tempBooks[i] {
			if book.IsAsk {
				books[i].Sells.PushBack(book)
			} else {
				books[i].Buys.PushBack(book)
			}
		}
	}
	// Get all messages
	var messages []database.L3OrderMessage
	DB.Find(&messages)

	// Simulate the order book
	for _, msg := range messages[:len(messages)-1] {
		websocket.UpdateOrderBook(books[0], database.FromOrderMessage(msg))
	}

	// See if they're equal
	if books[0].Sequence != books[1].Sequence {
		log.Fatalln("Failed: Sequences don't match")
	}
	if books[0].Buys.Len() != books[1].Buys.Len() || books[0].Sells.Len() != books[1].Sells.Len() {
		log.Fatalln("Failed: Book lengths don't match")
	}

	ids1 := make([]string, books[0].Buys.Len())
	ids2 := make([]string, books[0].Buys.Len())
	e1 := books[0].Buys.Front()
	e2 := books[1].Buys.Front()
	for i := 0; i < books[0].Buys.Len(); i++ {
		v1 := e1.Value.(database.OrderBookSnapshot)
		v2 := e2.Value.(database.OrderBookSnapshot)
		ids1[i] = v1.OrderID
		ids2[i] = v2.OrderID
		e1 = e1.Next()
		e2 = e2.Next()
	}

	// Sort the slices
	sort.Slice(ids1, func(i, j int) bool {
		return ids1[i] < ids1[j]
	})
	sort.Slice(ids2, func(i, j int) bool {
		return ids2[i] < ids2[j]
	})

	// See if they're the same
	for i := 0; i < len(ids1); i++ {
		if ids1[i] != ids2[i] {
			log.Println(ids1[i], ids2[i])
		}
	}

	ids1 = make([]string, books[0].Sells.Len())
	ids2 = make([]string, books[0].Sells.Len())
	e1 = books[0].Sells.Front()
	e2 = books[1].Sells.Front()
	for i := 0; i < books[0].Sells.Len(); i++ {
		v1 := e1.Value.(database.OrderBookSnapshot)
		v2 := e2.Value.(database.OrderBookSnapshot)
		ids1[i] = v1.OrderID
		ids2[i] = v2.OrderID
		e1 = e1.Next()
		e2 = e2.Next()
	}

	// Sort the slices
	sort.Slice(ids1, func(i, j int) bool {
		return ids1[i] < ids1[j]
	})
	sort.Slice(ids2, func(i, j int) bool {
		return ids2[i] < ids2[j]
	})

	// See if they're the same
	for i := 0; i < len(ids1); i++ {
		if ids1[i] != ids2[i] {
			log.Println(ids1[i], ids2[i])
		}
	}
	log.Println("They're the same")
}

// Used to actually run the bot
func run(ctx context.Context, conf config.Config) {
	// Handle signal interrupts
	defer func() {
		select {
		case <-ctx.Done():
			log.Println("Received an interrupt. Shutting down gracefully")
			os.Exit(1)
		// There was no signal but the program is done anyways
		default:
		}
	}()

	// Check to see if we're sandboxed or not ot make it easier to use
	// the different bot configs
	var botConf config.Bot
	if conf.Conf.IsSandbox {
		botConf = conf.Conf.Sandbox
	} else {
		botConf = conf.Conf.Production
	}

	// Get our coinbase client
	client := coinbasepro.NewClient()

	// Check if the bot is in a sandbox
	if conf.Conf.IsSandbox {
		log.Println("RUNNING IN SANDBOX MODE")
	} else {
		// Alert the user they are not in a sandbox
		err := lib.AlertUser()
		// If this errors we probably are daemonized and already asked for input
		if err != nil {
			log.Fatalln(err)
		}
	}

	// Authenticate client configuration can be updated with ClientConfig
	client.UpdateConfig(&coinbasepro.ClientConfig{
		BaseURL:    botConf.Coinbase.URL,
		Key:        botConf.Coinbase.Key,
		Secret:     botConf.Coinbase.Secret,
		Passphrase: botConf.Coinbase.Passphrase,
	})

	// Get historic rates
	if len(conf.CLI.HistoricRates) > 0 {
		// Loop through file names
		for _, file := range conf.CLI.HistoricRates {
			// Read in file
			params, err := ReadRateFile(file)
			if err != nil {
				log.Fatalln(err)
			}

			// Create a RateLimiter to not overwhelm the API
			rateLimiter := ratelimit.NewRateLimiter(
				10,
				10,
				100*time.Millisecond,
				time.Second,
			)

			for _, param := range params {
				err = SaveHistoricalRates(ctx, client, rateLimiter, param)
				if err != nil {
					log.Println(err)
				}
			}
		}

		return
	}

	// Only run this in the sandbox because I don't want to screw up when messing around
	if conf.Conf.IsSandbox {
		testingStuff(client, conf)
	}
}

func main() {
	// Set up sig handler
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load in config file
	conf, err := config.Parse()
	if err != nil {
		log.Fatalln(err)
	}

	// Check to see if we're sandboxed or not ot make it easier to use
	// the different bot configs
	var botConf config.Bot
	if conf.Conf.IsSandbox {
		botConf = conf.Conf.Sandbox
	} else {
		botConf = conf.Conf.Production
	}

	// Connect to the DB including tables if necessary
	DB = database.ConnectDB(ctx, botConf.DB)

	// Drop and recreate the tables
	if conf.CLI.DropTables {
		database.DropTables(DB)
	}

	database.CreateTables(DB)

	// Check if we should just run the websocket
	if conf.CLI.WS {
		if conf.Conf.IsSandbox {
			log.Println("RUNNING IN SANDBOX MODE")
		} else {
			log.Println("RUNNING IN PRODUCTION MODE")
		}

		// Have to pass in channels due to the weird way the coinbase channels work
		// It doesn't let you differentiate between user and full channels
		websocket.WSDispatcher(ctx, conf, DB, conf.Conf.Ws.Channels)
	}

	// See if we need to daemonize
	if conf.Daemon.Daemonize {
		runner := func(ctx context.Context, coinbaseConf config.Config, child *os.Process) {
			// Run the real program if it's the child
			// If it's the parent we are done and can exit the program
			if child == nil {
				run(ctx, conf)
			}
		}

		// Determine if we're sandboxed or not
		if !conf.Conf.IsSandbox {
			// Alert the user they are not in a sandbox
			err = lib.AlertUser()
			// This shouldn't have an error
			if err != nil {
				log.Fatalln(err)
			}
		}

		// Daemonize now
		daemonize(ctx, conf, conf.Daemon, runner)

		// We don't want to call run() and will return here
		return
	}

	// Check if we should daemonize the websocket
	if conf.WsDaemon.Daemonize {
		log.Println("Daemonizing websocket dispatcher")
		daemonize(ctx, conf, conf.WsDaemon, wsDaemonHelper)
	}

	// Run the main part of the program
	run(ctx, conf)
}
