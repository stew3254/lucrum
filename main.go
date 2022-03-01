package main

import (
	"context"
	"log"
	"lucrum/config"
	"lucrum/database"
	"lucrum/lib"
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

// Used for testing stuff with the bot for now
// I'll fill this in as I want to test stuff
func testingStuff(client *coinbasepro.Client, conf config.Config) {
	log.Println("Nothing being tested now")
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
