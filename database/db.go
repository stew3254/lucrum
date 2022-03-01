package database

import (
	"context"
	"fmt"
	"github.com/preichenberger/go-coinbasepro/v2"
	"log"
	"lucrum/config"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func check(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

// CreateTables uses the models from models.go to create database tables
func CreateTables(db *gorm.DB) {
	check(db.Migrator().AutoMigrate(&L3OrderMessage{}))
	check(db.Migrator().AutoMigrate(&OrderBookSnapshot{}))
	check(db.Migrator().AutoMigrate(&Transaction{}))
	check(db.Migrator().AutoMigrate(&AggregateTransaction{}))
	check(db.Migrator().AutoMigrate(&HistoricalData{}))
}

// ConnectDB is a simple wrapper to open a GORM DB
func ConnectDB(ctx context.Context, conf config.Database) (db *gorm.DB) {
	// Make sure type is lower
	dbType := strings.ToLower(conf.Type)

	// If using a SQLite DB
	if dbType == "sqlite" {
		// Just open the db
		db, err := gorm.Open(sqlite.Open(conf.Name), &gorm.Config{CreateBatchSize: 500})
		if err != nil {
			log.Fatalln("Failed to open the database with reason:", err)
		}
		log.Println("Connection to DB succeeded!")
		// Then create the tables
		CreateTables(db)
		return db
	} else if dbType == "postgres" {
		// Create connection string
		connectionString := fmt.Sprintf(
			"sslmode=disable host=%s port=%d dbname=%s user=%s password=%s",
			conf.Host,
			conf.Port,
			conf.Name,
			conf.User,
			conf.Passphrase,
		)

		// Loop through possible connection attempts
		for i := 1; i <= conf.Attempts; i++ {
			var err error
			// TODO fix this when database cannot connect
			db, err = gorm.Open(postgres.Open(connectionString), &gorm.Config{})
			// Failure to connect to the database
			if err != nil {
				if i != conf.Attempts {
					log.Printf(
						"WARNING: Could not connect to db on attempt %d. Trying again in %d seconds.\n",
						i,
						conf.Wait,
					)
				} else {
					log.Fatalf("could not connect to db after %d attempts", conf.Attempts)
				}
				// Create a new timer to wait before trying again
				timer := time.After(time.Second * conf.Wait)
				// Sit here and wait until an interrupt is received or the timer expires
				// There might be a better way to not busy wait
				for {
					select {
					// Try to handle signal interrupts while waiting
					case <-ctx.Done():
						log.Println("Received an interrupt. Shutting down gracefully")
						os.Exit(1)
					// Wait on the timer
					case <-timer:
						goto TimeExpired
					}
				}
			TimeExpired:
			} else {
				// No error to worry about
				break
			}
		}
		log.Println("Connection to db succeeded!")
		// Now create the tables
		CreateTables(db)
		return db
	}
	return nil
}

// DropTables drops all tables in the DB, but is not a generic function if models have been
// added to models.go, they must be placed in here by hand
func DropTables(db *gorm.DB) {
	// Drop tables in an order that won't invoke errors from foreign key constraints
	check(db.Migrator().DropTable(&L3OrderMessage{}))
	check(db.Migrator().DropTable(&AggregateTransaction{}))
	check(db.Migrator().DropTable(&Transaction{}))
	check(db.Migrator().DropTable(&OrderBookSnapshot{}))
	check(db.Migrator().DropTable(&HistoricalData{}))
}

// ToOrderMessage converts a coinbase message into an L3 compatible order message
func ToOrderMessage(msg coinbasepro.Message) L3OrderMessage {
	return L3OrderMessage{
		Type:          msg.Type,
		ProductID:     msg.ProductID,
		TradeID:       msg.TradeID,
		OrderID:       msg.OrderID,
		Sequence:      msg.Sequence,
		MakerOrderID:  msg.MakerOrderID,
		TakerOrderID:  msg.TakerOrderID,
		Time:          msg.Time.Time().UnixMicro(),
		RemainingSize: msg.RemainingSize,
		NewSize:       msg.NewSize,
		OldSize:       msg.OldSize,
		Size:          msg.Size,
		Price:         msg.Price,
		Side:          msg.Side,
		Reason:        msg.Reason,
		OrderType:     msg.OrderType,
		Funds:         msg.Funds,
		UserID:        msg.UserID,
		ProfileID:     msg.ProfileID,
	}
}

// FromOrderMessage converts an L3 message into a coinbase one
func FromOrderMessage(msg L3OrderMessage) coinbasepro.Message {
	return coinbasepro.Message{
		Type:          msg.Type,
		ProductID:     msg.ProductID,
		TradeID:       msg.TradeID,
		OrderID:       msg.OrderID,
		Sequence:      msg.Sequence,
		MakerOrderID:  msg.MakerOrderID,
		TakerOrderID:  msg.TakerOrderID,
		Time:          coinbasepro.Time(time.UnixMicro(msg.Time)),
		RemainingSize: msg.RemainingSize,
		NewSize:       msg.NewSize,
		OldSize:       msg.OldSize,
		Size:          msg.Size,
		Price:         msg.Price,
		Side:          msg.Side,
		Reason:        msg.Reason,
		OrderType:     msg.OrderType,
		Funds:         msg.Funds,
		UserID:        msg.UserID,
		ProfileID:     msg.ProfileID,
	}
}
