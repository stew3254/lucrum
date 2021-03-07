package main

import (
	"time"
)

type MarketData struct {
	Time        time.Time `gorm:"primaryKey; type:text"`
	Coin        string    `gorm:"type:text"`
	High        float64   `gorm:"type:real"`
	Low         float64   `gorm:"type:real"`
	Open        float64   `gorm:"type:real"`
	Close       float64   `gorm:"type:real"`
	Volume      float64   `gorm:"type:int"`
	Granularity int       `gorm:"type:int2"`
}

// Create this typedef to distinguish data the bot collects live vs past data
type HistoricalData MarketData

type OrderBook struct {
	Id             string    `gorm:"primaryKey; type:text"`
	Price          float64   `gorm:"type:real"`
	Size           float64   `gorm:"type:real"`
	ProductId      string    `gorm:"type:text"`
	Side           string    `gorm:"type:text"`
	Funds          float64   `gorm:"type:real"`
	SpecifiedFunds float64   `gorm:"type:real"`
	Type           string    `gorm:"type:text"`
	CreatedAt      time.Time `gorm:"type:text"`
	DoneAt         time.Time `gorm:"type:text"`
	Cancelled      bool      `gorm:"type:int1"`
	FillFees       float64   `gorm:"type:real"`
	FilledSize     float64   `gorm:"type:real"`
	ExecutedValue  float64   `gorm:"type:real"`
	Status         string    `gorm:"type:text"`
	Settled        bool      `gorm:"type:int1"`
}
