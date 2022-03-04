package main

import (
	"context"
	"log"
	"lucrum/config"
	"lucrum/database"
	"lucrum/lib"
	"lucrum/websocket"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/stew3254/ratelimit"
)

// Used for testing stuff with the bot for now
// I'll fill this in as I want to test stuff
func testingStuff(ctx context.Context) {
	log.Println("Nothing being tested now")
}

// Used to actually run the bot
func run(ctx context.Context) {
	// Handle signal interrupts
	defer func() {
		select {
		case <-ctx.Done():
			log.Println("Received an interrupt. Shutting down gracefully")
			os.Exit(1)
		// There was no signal but the program is done anyway
		default:
		}
	}()
	// Get the configuration
	conf := ctx.Value(lib.LucrumKey("conf")).(config.Configuration)
	botConf := ctx.Value(lib.LucrumKey("botConf")).(config.Coinbase)

	// Check if the bot is in a sandbox
	if conf.Type.IsSandbox {
		log.Println("RUNNING IN SANDBOX MODE")
	} else {
		// Alert the user they are not in a sandbox
		err := lib.AlertUser()
		// If this creates an error, we already daemonized and have asked for input
		if err != nil {
			log.Fatalln(err)
		}
	}

	// Make our coinbase client and update to the appropriate config
	client := &coinbasepro.Client{
		BaseURL:    botConf.URL,
		Key:        botConf.Key,
		Secret:     botConf.Secret,
		Passphrase: botConf.Passphrase,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		RetryCount: 3,
	}

	// Add the client to the context
	ctx = context.WithValue(ctx, lib.LucrumKey("client"), client)

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
	if conf.Type.IsSandbox {
		testingStuff(ctx)
	}
}

func main() {
	// Set up sig handler
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	once := sync.Once{}
	defer once.Do(stop)

	// Create wait group to wait for children to finish
	wg := &sync.WaitGroup{}

	// Load in config file
	conf, err := config.Parse()
	if err != nil {
		log.Fatalln(err)
	}

	// Add the config to the context
	ctx = context.WithValue(ctx, lib.LucrumKey("conf"), conf)

	// Check to see if we're sandboxed or not ot make it easier to use
	// the different bot configs
	var botConf config.Bot
	if conf.Type.IsSandbox {
		botConf = conf.Type.Sandbox
	} else {
		botConf = conf.Type.Production
	}
	// Add the bot conf and ws conf to the context
	ctx = context.WithValue(ctx, lib.LucrumKey("botConf"), botConf.Coinbase)
	ctx = context.WithValue(ctx, lib.LucrumKey("wsConf"), conf.Type.Ws)

	// Connect to the DB including tables if necessary and add to context
	db := database.ConnectDB(ctx, botConf.DB)

	// Add the db to the context
	ctx = context.WithValue(ctx, lib.LucrumKey("db"), db)

	// Drop and recreate the tables
	if conf.CLI.DropTables {
		database.DropTables(db)
	}

	database.CreateTables(db)

	// Check if we should just run the websocket
	if conf.CLI.WS {
		if conf.Type.IsSandbox {
			log.Println("RUNNING IN SANDBOX MODE")
		} else {
			log.Println("RUNNING IN PRODUCTION MODE")
		}

		// Have to pass in channels due to the weird way the coinbase channels work
		// It doesn't let you differentiate between user and full channels
		wg.Add(1)
		websocket.WSDispatcher(ctx, wg, conf.Type.Ws.Channels)
		select {
		case <-ctx.Done():
			log.Println("Received an interrupt. Shutting down gracefully")
		default:
			once.Do(stop)
		}
		wg.Wait()
		os.Exit(1)
	}

	// See if we need to daemonize
	if conf.Daemon.Daemonize {
		runner := func(ctx context.Context, wg *sync.WaitGroup, wsConf config.Websocket, child *os.Process) {
			// Run the real program if it's the child
			// If it's the parent we are done and can exit the program
			if child == nil {
				run(ctx)
			}
		}

		// Determine if we're sandboxed or not
		if !conf.Type.IsSandbox {
			// Alert the user they are not in a sandbox
			err = lib.AlertUser()
			// This shouldn't have an error
			if err != nil {
				log.Fatalln(err)
			}
		}

		// Daemonize now
		daemonize(ctx, wg, conf.Daemon, runner)

		// We don't want to call run() and will return here
		return
	}

	// Check if we should daemonize the websocket
	if conf.WsDaemon.Daemonize {
		log.Println("Daemonizing websocket dispatcher")
		daemonize(ctx, wg, conf.WsDaemon, wsDaemonHelper)
	}

	// Run the main part of the program
	run(ctx)
}
