package database

import (
	"time"
)

type L3OrderMessage struct {
	Id            int64     `gorm:"type:int; primaryKey; not null"`
	Type          string    `gorm:"type:text; not null"`
	ProductID     string    `gorm:"type:text; not null"`
	TradeID       int       `gorm:"type:int"`
	OrderID       string    `gorm:"type:text"`
	Sequence      int64     `gorm:"type:int; default:0; not null"`
	MakerOrderID  string    `gorm:"type:text"`
	TakerOrderID  string    `gorm:"type:text"`
	Time          time.Time `gorm:"type:text"`
	RemainingSize string    `gorm:"type:text"`
	NewSize       string    `gorm:"type:text"`
	OldSize       string    `gorm:"type:text"`
	Size          string    `gorm:"type:text"`
	Price         string    `gorm:"type:text"`
	Side          string    `gorm:"type:text"`
	Reason        string    `gorm:"type:text"`
	OrderType     string    `gorm:"type:text"`
	Funds         string    `gorm:"type:text"`
	UserID        string    `gorm:"type:text"`
	ProfileID     string    `gorm:"type:text"`
}

// MarketData is the data that will be collected for the simulation and performance analysis
type MarketData struct {
	Id                int64     `gorm:"type:integer; primaryKey; not null"`
	Time              time.Time `gorm:"type:text"`
	Coin              string    `gorm:"type:varchar(16)"`
	High              float64   `gorm:"type:float"`
	Low               float64   `gorm:"type:float"`
	Average           float64   `gorm:"type:float"`
	Open              float64   `gorm:"type:float"`
	Close             float64   `gorm:"type:float"`
	Volume            float64   `gorm:"type:double"`
	Granularity       int       `gorm:"type:int"`
	BuyOrders         int       `gorm:"type:int"`
	SellOrders        int       `gorm:"type:int"`
	FilledBuyOrders   int       `gorm:"type:int"`
	FilledSellOrders  int       `gorm:"type:int"`
	HighBuyAmount     float64   `gorm:"type:double"`
	HighSellAmount    float64   `gorm:"type:double"`
	LowBuyAmount      float64   `gorm:"type:double"`
	LowSellAmount     float64   `gorm:"type:double"`
	AverageBuyPrice   float64   `gorm:"type:double"`
	AverageSellPrice  float64   `gorm:"type:double"`
	AverageAmount     float64   `gorm:"type:double"`
	VarianceBuyPrice  float64   `gorm:"type:double"`
	VarianceSellPrice float64   `gorm:"type:double"`
	VarianceAmount    float64   `gorm:"type:double"`
}

// HistoricalData is the historical data pulled from the Coinbase Pro API
type HistoricalData struct {
	Id          int64     `gorm:"type:integer; primaryKey; not null"`
	Time        time.Time `gorm:"type:text"`
	Coin        string    `gorm:"type:varchar(16)"`
	High        float64   `gorm:"type:float"`
	Low         float64   `gorm:"type:float"`
	Open        float64   `gorm:"type:float"`
	Close       float64   `gorm:"type:float"`
	Volume      float64   `gorm:"type:double"`
	Granularity int       `gorm:"type:int"`
}

// OrderBookSnapshot contains entries to build the state of the book at a specific time
type OrderBookSnapshot struct {
	Id        int64     `gorm:"type:int; primaryKey; not null"`
	ProductId string    `gorm:"type:text; not null"`
	Sequence  int64     `gorm:type:int; not null`
	IsAsk     bool      `gorm:"type:boolean; not null"`
	Time      time.Time `gorm:"type:text; not null"`
	Price     string    `gorm:"type:text; not null" json:"price"`
	Size      string    `gorm:"type:text; not null" json:"size"`
	OrderID   string    `gorm:"type:text; not null" json:"order_id"`
}
