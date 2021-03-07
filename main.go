package main

import (
	"context"
	"fmt"
	"log"
	"lucrum/config"
	"lucrum/ratelimit"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"
	"gorm.io/gorm"
)

// DB is a global DB connection to be shared
var DB *gorm.DB

// Used to actually run the bot
func run(ctx context.Context, conf config.Config) {
	// Handle signal interrupts
	defer func() {
		select {
		case <-ctx.Done():
			log.Println("Received an interrupt. Shutting down gracefully")
		// There was no signal but the program is done anyways
		default:
		}
		// Try to remove the PID file
		if conf.Daemon.Daemonize {
			err := os.Remove(conf.Daemon.PidFile)
			if err != nil {
				log.Println(err)
			}
		}
	}()

	// Create the DB
	DB = CreateDB(conf.DB.Name)

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
	if len(config.CLI.HistoricRates) > 0 {
		// Loop through file names
		for _, file := range config.CLI.HistoricRates {
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
				err = SaveHistoricalRates(client, rateLimiter, param)
				if err != nil {
					log.Println(err)
				}
			}
		}

		return
	}

	products, err := client.GetProducts()
	Check(err)
	for _, product := range products {
		if strings.HasSuffix(product.ID, "USD") {
			fmt.Println(product.ID)
		}
	}
	log.Println(len(products))
}

func main() {
	// Set up sig handler
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load in config file
	conf, err := config.Parse("config.toml")
	Check(err)

	// Run command line parsing
	config.ArgParse()

	// Override sandbox configuration
	if config.CLI.Sandbox {
		conf.Bot.IsSandbox = true
	} else if config.CLI.UnSandbox {
		conf.Bot.IsSandbox = false
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
