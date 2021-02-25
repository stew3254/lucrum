package main

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
)

// CreateDB is a simple wrapper to open a GORM DB
func CreateDB(dbName string) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		log.Fatalln(err)
	}

	// Add the models and build tables out of them. Fail if they can't be added
	Check(db.Migrator().AutoMigrate(&OrderBook{}))
	Check(db.Migrator().AutoMigrate(&MarketData{}))

	log.Println("Connection to DB succeeded!")
	return db
}

// DropTables drops everything in the DB
func DropTables(db *gorm.DB) {
	// Drop tables in an order that won't invoke errors from foreign key constraints
	Check(db.Migrator().DropTable(&MarketData{}))
	Check(db.Migrator().DropTable(&OrderBook{}))
}
