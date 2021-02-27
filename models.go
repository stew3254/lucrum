package main

import "github.com/preichenberger/go-coinbasepro/v2"

type MarketData struct {
	Time        coinbasepro.Time `gorm:"primaryKey; type:text"`
	Coin        string           `gorm:"type:text"`
	High        float32          `gorm:"type:real"`
	Low         float32          `gorm:"type:real"`
	Open        float32          `gorm:"type:real"`
	Close       float32          `gorm:"type:real"`
	Volume      int32            `gorm:"type:int"`
	Granularity int32            `gorm:"type:int2"`
}

// Create this typedef to distinguish data the bot collects live vs past data
type HistoricalData MarketData

type OrderBook struct {
	Id             string           `gorm:"primaryKey; type:text"`
	Price          float32          `gorm:"type:real"`
	Size           float32          `gorm:"type:real"`
	ProductId      string           `gorm:"type:text"`
	Side           string           `gorm:"type:text"`
	Funds          float32          `gorm:"type:real"`
	SpecifiedFunds float32          `gorm:"type:real"`
	Type           string           `gorm:"type:text"`
	CreatedAt      coinbasepro.Time `gorm:"type:text"`
	DoneAt         coinbasepro.Time `gorm:"type:text"`
	Cancelled      bool             `gorm:"type:int1"`
	FillFees       float32          `gorm:"type:real"`
	FilledSize     float32          `gorm:"type:real"`
	ExecutedValue  float32          `gorm:"type:real"`
	Status         string           `gorm:"type:text"`
	Settled        bool             `gorm:"type:int1"`
}
