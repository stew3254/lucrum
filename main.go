package main

import (
	"context"
	"log"
	"lucrum/config"
	"lucrum/websocket"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/stew3254/ratelimit"
	"gorm.io/gorm"
)

// DB is a global DB connection to be shared
var DB *gorm.DB

const SIGNAL int = 1

// Used for testing stuff with the bot for now
// I'll fill this in as I want to test stuff
func testingStuff(client *coinbasepro.Client, conf config.Config) {
	order := coinbasepro.Order{
		Type:      "market",
		Size:      ".001",
		Side:      "buy",
		ProductID: "ETC-USD",
	}
	order, err := client.CreateOrder(&order)
	log.Println(order)
	Check(err)
}

// Used to actually run the bot
func run(ctx context.Context, conf config.Config) {
	// Handle signal interrupts
	defer func() {
		select {
		case <-ctx.Done():
			log.Println("Received an interrupt. Shutting down gracefully")
			os.Exit(SIGNAL)
		// There was no signal but the program is done anyways
		default:
		}
	}()

	// Create the DB
	DB = ConnectDB(ctx, conf.DB)

	// Drop and recreate the tables
	if conf.CLI.DropTables {
		DropTables(DB)
		CreateTables(DB)
	}

	// Get our coinbase client
	client := coinbasepro.NewClient()

	// Check if the bot is in a sandbox
	if conf.Bot.IsSandbox {
		log.Println("RUNNING IN SANDBOX MODE")
		// Authenticate client configuration can be updated with ClientConfig
		client.UpdateConfig(&coinbasepro.ClientConfig{
			BaseURL:    conf.Bot.Sandbox.URL,
			Key:        conf.Bot.Sandbox.Key,
			Secret:     conf.Bot.Sandbox.Secret,
			Passphrase: conf.Bot.Sandbox.Passphrase,
		})
	} else {
		// Alert the user they are not in a sandbox
		err := AlertUser()
		// If this errors we probably are daemonized and already asked for input
		if err != nil {
			log.Fatalln(err)
		}

		// Authenticate client configuration can be updated with ClientConfig
		client.UpdateConfig(&coinbasepro.ClientConfig{
			BaseURL:    conf.Bot.Coinbase.URL,
			Key:        conf.Bot.Coinbase.Key,
			Secret:     conf.Bot.Coinbase.Secret,
			Passphrase: conf.Bot.Coinbase.Passphrase,
		})
	}

	// Get historic rates
	if len(conf.CLI.HistoricRates) > 0 {
		// Loop through file names
		for _, file := range conf.CLI.HistoricRates {
			// Read in file
			params, err := ReadRateFile(file)
			Check(err)

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
	if conf.Bot.IsSandbox {
		testingStuff(client, conf)
	}
}

func main() {
	// Set up sig handler
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load in config file
	conf, err := config.Parse()
	Check(err)

	// Check if we should just run the websocket
	if conf.CLI.WS {
		if conf.Bot.IsSandbox {
			log.Println("RUNNING IN SANDBOX MODE")
		} else {
			log.Println("RUNNING IN PRODUCTION MODE")
		}
		// Have to pass in channels due to the weird way the coinbase channels work
		// It doesn't let you differentiate between user and full channels
		websocket.WSDispatcher(ctx, conf, conf.Bot.Ws.Channels)
	}

	// See if we need to daemonize
	// Note we don't care to use the websocket if it's not as a daemon (for now)
	if conf.Daemon.Daemonize {
		runner := func(ctx context.Context, coinbaseConf config.Config, child *os.Process) {
			// Run the real program if it's the child
			// If it's the parent we are done and can exit the program
			if child == nil {
				run(ctx, conf)
			}
		}

		// Determine if we're sandboxed or not
		if conf.Bot.IsSandbox {
			daemonize(ctx, conf, conf.Daemon, runner)
		} else {
			// Alert the user they are not in a sandbox
			err = AlertUser()
			// This shouldn't have an error
			if err != nil {
				log.Fatalln(err)
			}
			daemonize(ctx, conf, conf.Daemon, runner)
		}

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
