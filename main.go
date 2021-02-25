package main

import (
	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/profclems/go-dotenv"
	"gorm.io/gorm"
	"log"
	"time"
)

// DB is a global DB connection to be shared
var DB *gorm.DB

func main() {
	// Load in the dotenv config
	err := dotenv.LoadConfig()
	if err != nil {
		log.Fatalln("Error loading in .env file")
	}

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

	if dotenv.GetInt("COINBASE_PRO_SANDBOX") == 1 {
		log.Println("RUNNING IN SANDBOX MODE")
		// Authenticate client configuration can be updated with ClientConfig
		client.UpdateConfig(&coinbasepro.ClientConfig{
			BaseURL:    dotenv.GetString("COINBASE_PRO_SANDBOX_URL"),
			Key:        dotenv.GetString("COINBASE_PRO_SANDBOX_KEY"),
			Passphrase: dotenv.GetString("COINBASE_PRO_SANDBOX_PASSPHRASE"),
			Secret:     dotenv.GetString("COINBASE_PRO_SANDBOX_SECRET"),
		})
	} else {
		// Authenticate client configuration can be updated with ClientConfig
		client.UpdateConfig(&coinbasepro.ClientConfig{
			BaseURL:    dotenv.GetString("COINBASE_PRO_URL"),
			Key:        dotenv.GetString("COINBASE_PRO_KEY"),
			Passphrase: dotenv.GetString("COINBASE_PRO_PASSPHRASE"),
			Secret:     dotenv.GetString("COINBASE_PRO_SECRET"),
		})
	}
	
	rateParams := coinbasepro.GetHistoricRatesParams{
		Start:       time.Date(2021, 1, 1, 0, 0, 0, 0, time.Local),
		End:         time.Now(),
		Granularity: 86400,
	}
	rates, err := client.GetHistoricRates("BTC-USD", rateParams)
	Check(err)
	log.Println(rates)
}
