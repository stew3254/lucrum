package main

import (
	"bufio"
	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/profclems/go-dotenv"
	"github.com/sevlyar/go-daemon"
	"gorm.io/gorm"
	"io"
	"log"
	"lucrum/websocket"
	"os"
	"strings"
)

// DB is a global DB connection to be shared
var DB *gorm.DB

func daemonize(logFile string, pidFile string, helper func(*os.Process)) {
	// Get workdir
	cwd, err := os.Getwd()
	Check(err)

	// Create a new daemon context
	ctx := daemon.Context{
		PidFileName: pidFile,
		PidFilePerm: 0644,
		LogFileName: logFile,
		LogFilePerm: 0644,
		WorkDir:     cwd,
		Args:        os.Args,
		Umask:       027,
	}

	// See if this process already exists
	proc, err := ctx.Search()
	// Something bad happened we don't know
	// This catches the file not existing and there not being a PID in the file
	if !os.IsNotExist(err) && err != io.EOF && err != nil {
		log.Fatalln(err)
	}
	
	// A process already exists so we don't need to daemonize
	if proc != nil {
		log.Println("Daemon process already exists, skipping daemonizing")
		return
	}

	// Daemonize
	child, err := ctx.Reborn()
	Check(err)

	// Run on daemonize
	helper(child)

	// Release the daemon
	err = ctx.Release()
	Check(err)
}

func wsDaemonHelper(child *os.Process) {
	// This is the child
	if child == nil {
		// Call the dispatcher
		// TODO make a config file to read this stuff from
		websocket.WSDispatcher([]coinbasepro.MessageChannel{
			{
				Name: "ticker",
				ProductIds: []string{
					"BTC-USD",
				},
			},
		})
	}
	// On parent we just return because more work might need to be done
}

// Used to actually run the bot
func run() {
	// Create the DB
	DB = CreateDB(dotenv.GetString("DB_NAME"))

	client := coinbasepro.NewClient()

	// Use this later
	// Set the redis config
	// rdConfig, err := redis.ParseURL(dotenv.GetString("REDIS_CONNECTION_STRING"))
	// if err != nil {
	//   log.Fatalln(err)
	// }
	//
	// rdb := redis.NewClient(rdConfig)

	if dotenv.GetBool("COINBASE_PRO_SANDBOX") {
		log.Println("RUNNING IN SANDBOX MODE")
		// Authenticate client configuration can be updated with ClientConfig
		client.UpdateConfig(&coinbasepro.ClientConfig{
			BaseURL:    dotenv.GetString("COINBASE_PRO_SANDBOX_URL"),
			Key:        dotenv.GetString("COINBASE_PRO_SANDBOX_KEY"),
			Passphrase: dotenv.GetString("COINBASE_PRO_SANDBOX_PASSPHRASE"),
			Secret:     dotenv.GetString("COINBASE_PRO_SANDBOX_SECRET"),
		})
	} else {
		log.Println("YOU ARE NOT IN A SANDBOX! ARE YOU SURE YOU WANT TO CONTINUE? (Y/n)")
		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		text = strings.Trim(strings.ToLower(text), "\n")
		if text != "y" && text != "yes" {
			log.Println("Okay, shutting down")
			log.Println("To run in sandbox mode, set COINBASE_PRO_SANDBOX=true in the .env file")
			os.Exit(0)
		}
		// Authenticate client configuration can be updated with ClientConfig
		client.UpdateConfig(&coinbasepro.ClientConfig{
			BaseURL:    dotenv.GetString("COINBASE_PRO_URL"),
			Key:        dotenv.GetString("COINBASE_PRO_KEY"),
			Passphrase: dotenv.GetString("COINBASE_PRO_PASSPHRASE"),
			Secret:     dotenv.GetString("COINBASE_PRO_SECRET"),
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
	// Load in the dotenv config
	err := dotenv.LoadConfig()
	if err != nil {
		log.Fatalln("Error loading in .env file")
	}

	// Get websocket logfile name
	wsLogFile := dotenv.GetString("WS_LOG_FILE")
	if len(wsLogFile) == 0 {
		wsLogFile = "websocket.log"
	}

	// See if we need to daemonize
	// Note we don't care to use the websocket if it's not as a daemon (for now)
	if dotenv.GetBool("DAEMONIZE_BOT") {
		// First daemonize the websocket
		log.Println("Daemonizing websocket dispatcher")
		daemonize(wsLogFile, dotenv.GetString("WEBSOCKET_PID_FILE"), wsDaemonHelper)

		// Get logfile name
		logFile := dotenv.GetString("LOG_FILE")
		if len(logFile) == 0 {
			logFile = "lucrum.log"
		}

		runner := func(child *os.Process) {
			// Run the real program if it's the child
			// If it's the parent we are done and can exit the program
			if child == nil {
				run()
			}
		}
		log.Println("Daemonizing lucrum")
		daemonize(logFile, dotenv.GetString("PID_FILE"), runner)

		// We don't want to call run() and will return here
		return
		
		// Check if we should still daemonize the websocket
	} else if dotenv.GetBool("DAEMONIZE_WEBSOCKET") {
		log.Println("Daemonizing websocket dispatcher")
		daemonize(wsLogFile, dotenv.GetString("WEBSOCKET_PID_FILE"), wsDaemonHelper)
	}
	// Run the main part of the program
	run()
}
