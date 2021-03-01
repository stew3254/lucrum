package main

import (
	"bufio"
	"context"
	"log"
	"lucrum/config"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sevlyar/go-daemon"

	"github.com/preichenberger/go-coinbasepro/v2"
	"gorm.io/gorm"
)

// DB is a global DB connection to be shared
var DB *gorm.DB

func alertUser() (err error) {
	if daemon.WasReborn() {
		return nil
	}

	// Alert user they are not in a sandbox
	log.Println("YOU ARE NOT IN A SANDBOX! ARE YOU SURE YOU WANT TO CONTINUE? (Y/n)")

	// Try to read stdin
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')

	// Return the error
	if err != nil {
		return
	}

	// Get user input
	text = strings.Trim(strings.ToLower(text), "\n")
	if text != "y" && text != "yes" {
		log.Println("Okay, shutting down")
		log.Println("To run in sandbox mode, set 'is_sandbox = true' in the .env file")
		os.Exit(0)
	}
	return
}

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
		err := alertUser()
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

	// rateParams := coinbasepro.GetHistoricRatesParams{
	// 	Start:       time.Date(2021, 1, 1, 0, 0, 0, 0, time.Local),
	// 	End:         time.Now(),
	// 	Granularity: 86400,
	// }
	// rates, err := client.GetHistoricRates("BTC-USD", rateParams)
	// Check(err)
	// log.Println(rates)
	products, err := client.GetProducts()
	Check(err)
	for _, product := range products {
		log.Println(product.ID)
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

	// See if we need to daemonize
	// Note we don't care to use the websocket if it's not as a daemon (for now)
	if conf.Daemon.Daemonize {
		// First daemonize the websocket
		// log.Println("Daemonizing websocket dispatcher")
		daemonize(ctx, conf.WsDaemon, conf.Bot.Coinbase, wsDaemonHelper)

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
			err = alertUser()
			// This shouldn't have an error
			if err != nil {
				log.Fatalln(err)
			}
			daemonize(ctx, conf.Daemon, conf.Bot.Sandbox, runner)
		}

		// We don't want to call run() and will return here
		return

		// Check if we should still daemonize the websocket
	} else if conf.WsDaemon.Daemonize {
		log.Println("Daemonizing websocket dispatcher")
		daemonize(ctx, conf.WsDaemon, conf.Bot.Coinbase, wsDaemonHelper)
	}
	// Run the main part of the program
	run(ctx, conf)
}
