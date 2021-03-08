package main

import (
	"fmt"
	"log"
	"lucrum/config"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func CreateTables(db *gorm.DB) {
	Check(db.Migrator().AutoMigrate(&OrderBook{}))
	Check(db.Migrator().AutoMigrate(&MarketData{}))
	Check(db.Migrator().AutoMigrate(&HistoricalData{}))
}

// ConnectDB is a simple wrapper to open a GORM DB
func ConnectDB(conf config.Database) (db *gorm.DB) {
	// Make sure type is lower
	dbType := strings.ToLower(conf.Type)

	// If using a SQLite DB
	if dbType == "sqlite" {
		db, err := gorm.Open(sqlite.Open(conf.Name), &gorm.Config{})
		if err != nil {
			log.Fatalln("Failed to open the database with reason:", err)
		}
		log.Println("Connection to DB succeeded!")
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

		for i := 1; i <= conf.Attempts; i++ {
			var err error
			db, err = gorm.Open(postgres.Open(connectionString), &gorm.Config{})
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
				time.Sleep(time.Duration(conf.Wait) * time.Second)
			} else {
				// No error to worry about
				break
			}
		}
		log.Println("Connection to db succeeded!")
		CreateTables(db)
		return db
	}
	return nil
}

// DropTables drops everything in the DB
func DropTables(db *gorm.DB) {
	// Drop tables in an order that won't invoke errors from foreign key constraints
	Check(db.Migrator().DropTable(&MarketData{}))
	Check(db.Migrator().DropTable(&OrderBook{}))
	Check(db.Migrator().DropTable(&HistoricalData{}))
}
