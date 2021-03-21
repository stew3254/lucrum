package main

import (
	"context"
	"fmt"
	"log"
	"lucrum/config"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// CreateTables uses the models from models.go to create database tables
func CreateTables(db *gorm.DB) {
	Check(db.Migrator().AutoMigrate(&OrderBook{}))
	Check(db.Migrator().AutoMigrate(&MarketData{}))
	Check(db.Migrator().AutoMigrate(&HistoricalData{}))
}

// ConnectDB is a simple wrapper to open a GORM DB
func ConnectDB(ctx context.Context, conf config.Database) (db *gorm.DB) {
	// Make sure type is lower
	dbType := strings.ToLower(conf.Type)

	// If using a SQLite DB
	if dbType == "sqlite" {
		// Just open the db
		db, err := gorm.Open(sqlite.Open(conf.Name), &gorm.Config{})
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
						os.Exit(0)
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
	Check(db.Migrator().DropTable(&MarketData{}))
	Check(db.Migrator().DropTable(&OrderBook{}))
	Check(db.Migrator().DropTable(&HistoricalData{}))
}
