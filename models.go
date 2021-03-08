package main

import (
	"time"
)

type MarketData struct {
	Id          int64     `gorm:"primaryKey; type:bigserial"`
	Time        time.Time `gorm:"type:timestamp"`
	Coin        string    `gorm:"type:varchar(16)"`
	High        float64   `gorm:"type:money"`
	Low         float64   `gorm:"type:money"`
	Open        float64   `gorm:"type:money"`
	Close       float64   `gorm:"type:money"`
	Volume      float64   `gorm:"type:int"`
	Granularity int       `gorm:"type:int"`
}

// Create this typedef to distinguish data the bot collects live vs past data
type HistoricalData MarketData

type OrderBook struct {
	Id             string    `gorm:"primaryKey; type:varchar(128)"`
	Price          float64   `gorm:"type:money"`
	Size           float64   `gorm:"type:float8"`
	ProductId      string    `gorm:"type:varchar(16)"`
	Side           string    `gorm:"type:varchar(4)"`
	Funds          float64   `gorm:"type:float8"`
	SpecifiedFunds float64   `gorm:"type:float8"`
	Type           string    `gorm:"type:varchar(32)"`
	CreatedAt      time.Time `gorm:"type:timestamp"`
	DoneAt         time.Time `gorm:"type:timestamp"`
	Cancelled      bool      `gorm:"type:bool"`
	FillFees       float64   `gorm:"type:float8"`
	FilledSize     float64   `gorm:"type:float8"`
	ExecutedValue  float64   `gorm:"type:float8"`
	Status         string    `gorm:"type:varchar(32)"`
	Settled        bool      `gorm:"type:bool"`
}
