package main

import (
	"time"
)

// MarketData is the the data that will be collected for the simulation and performance analysis
type MarketData struct {
	Id                int64     `gorm:"primaryKey; type:bigserial"`
	Time              time.Time `gorm:"type:timestamp"`
	Coin              string    `gorm:"type:varchar(16)"`
	High              float64   `gorm:"type:money"`
	Low               float64   `gorm:"type:money"`
	Average           float64   `gorm:"type:money"`
	Open              float64   `gorm:"type:money"`
	Close             float64   `gorm:"type:money"`
	Volume            float64   `gorm:"type:float8"`
	Granularity       int       `gorm:"type:int"`
	BuyOrders         int       `gorm:"type:float8"`
	SellOrders        int       `gorm:"type:float8"`
	FilledBuyOrders   int       `gorm:"type:float8"`
	FilledSellOrders  int       `gorm:"type:float8"`
	HighBuyAmount     float64   `gorm:"type:float8"`
	HighSellAmount    float64   `gorm:"type:float8"`
	LowBuyAmount      float64   `gorm:"type:float8"`
	LowSellAmount     float64   `gorm:"type:float8"`
	AverageBuyPrice   float64   `gorm:"type:float8"`
	AverageSellPrice  float64   `gorm:"type:float8"`
	AverageAmount     float64   `gorm:"type:float8"`
	VarianceBuyPrice  float64   `gorm:"type:float8"`
	VarianceSellPrice float64   `gorm:"type:float8"`
	VarianceAmount    float64   `gorm:"type:float8"`
}

// HistoricalData is the historical data pulled from the Coinbase Pro API
type HistoricalData struct {
	Id          int64     `gorm:"primaryKey; type:bigserial"`
	Time        time.Time `gorm:"type:timestamp"`
	Coin        string    `gorm:"type:varchar(16)"`
	High        float64   `gorm:"type:money"`
	Low         float64   `gorm:"type:money"`
	Open        float64   `gorm:"type:money"`
	Close       float64   `gorm:"type:money"`
	Volume      float64   `gorm:"type:float8"`
	Granularity int       `gorm:"type:int"`
}

// OrderBook is currently undecided
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
