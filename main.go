package main

import (
	"context"
	"log"
	"lucrum/config"
	"os"
	"os/signal"
	"syscall"

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
			return
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

	if len(config.CLI.HistoricRates) > 0 {
		for _, file := range config.CLI.HistoricRates {
			products, params, err := ReadRateFile(file)
			Check(err)
			for i, param := range params {
				rates, err := GetHistoricRates(client, products[i], param)
				if err != nil {
					log.Println(err)
				}
				log.Println(rates)
			}
		}
	}
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

	// See if we need to daemonize
	// Note we don't care to use the websocket if it's not as a daemon (for now)
	if conf.Daemon.Daemonize {
		runner := func(ctx context.Context, coinbaseConf config.Coinbase, child *os.Process) {
			// Run the real program if it's the child
			// If it's the parent we are done and can exit the program
			if child == nil {
				run(ctx, conf)
			}
		}

		// log.Println("Daemonizing lucrum")
		// Determine if we're sandboxed or not
		if conf.Bot.IsSandbox {
			daemonize(ctx, conf.Daemon, conf.Bot.Coinbase, runner)
		} else {
			// Alert the user they are not in a sandbox
			err = AlertUser()
			// This shouldn't have an error
			if err != nil {
				log.Fatalln(err)
			}
			daemonize(ctx, conf.Daemon, conf.Bot.Sandbox, runner)
		}

		// We don't want to call run() and will return here
		return

	}
	// Check if we should daemonize the websocket
	if conf.WsDaemon.Daemonize {
		log.Println("Daemonizing websocket dispatcher")
		daemonize(ctx, conf.WsDaemon, conf.Bot.Coinbase, wsDaemonHelper)
	}
	// Run the main part of the program
	run(ctx, conf)
}
