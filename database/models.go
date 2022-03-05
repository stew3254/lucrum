package database

// HistoricalData is the historical data pulled from the Coinbase Pro API
type HistoricalData struct {
	Id          int64   `gorm:"type:integer; primaryKey; not null"`
	Time        int64   `gorm:"type:int"`
	ProductId   string  `gorm:"type:varchar(16)"`
	High        float64 `gorm:"type:float"`
	Low         float64 `gorm:"type:float"`
	Open        float64 `gorm:"type:float"`
	Close       float64 `gorm:"type:float"`
	Volume      float64 `gorm:"type:double"`
	Granularity int     `gorm:"type:int"`
}

// OrderBookSnapshot contains entries to build the state of the book at a specific time
type OrderBookSnapshot struct {
	OrderID       string `gorm:"type:text; primaryKey; not null"`
	ProductId     string `gorm:"type:text; not null"`
	FirstSequence int64  `gorm:"type:int; not null"`
	LastSequence  int64  `gorm:"type:int; not null"`
	IsAsk         bool   `gorm:"type:boolean; not null"`
	Time          int64  `gorm:"type:int; not null"`
	Price         string `gorm:"type:text; not null"`
	Size          string `gorm:"type:text; not null"`
}

// L3OrderMessage is a raw message gotten from the coinbase full channel
type L3OrderMessage struct {
	Sequence      int64  `gorm:"type:int; primaryKey; not null"`
	Type          string `gorm:"type:text; not null"`
	ProductID     string `gorm:"type:text; not null"`
	TradeID       int    `gorm:"type:int"`
	OrderID       string `gorm:"type:varchar(128)"`
	MakerOrderID  string `gorm:"type:varchar(128)"`
	TakerOrderID  string `gorm:"type:varchar(128)"`
	Time          int64  `gorm:"type:int"`
	RemainingSize string `gorm:"type:text"`
	NewSize       string `gorm:"type:text"`
	OldSize       string `gorm:"type:text"`
	Size          string `gorm:"type:text"`
	Price         string `gorm:"type:text"`
	Side          string `gorm:"type:varchar(4)"`
	Reason        string `gorm:"type:varchar(8)"`
	OrderType     string `gorm:"type:varchar(6)"`
	Funds         string `gorm:"type:text"`
	UserID        string `gorm:"type:varchar(128)"`
	ProfileID     string `gorm:"type:varchar(128)"`
}

// Transaction is a processed version of an L3 message which is nicer for analysis
type Transaction struct {
	OrderID       string `gorm:"type:text; primaryKey; not null"`
	ProductId     string `gorm:"type:varchar(8); not null"`
	Time          int64  `gorm:"type:int; not null"`
	Price         string `gorm:"type:text; not null"`
	Side          string `gorm:"type:varchar(4); not null"`
	Size          string `gorm:"type:text; not null"`
	Funds         string `gorm:"type:text"`
	OrderType     string `gorm:"type:varchar(6); not null"`
	AddedToBook   bool   `gorm:"type:boolean; not null"`
	RemainingSize string `gorm:"type:text"`
	OrderChanged  bool   `gorm:"type:boolean; not null"`
	NewSize       string `gorm:"type:text"`
	Reason        string `gorm:"type:text"`
	ClosedAt      int64  `gorm:"type:int"`
	isMaker       bool   `gorm:"type:boolean"`
	MatchId       string `gorm:"varchar(128)"`
	// StopType      string `gorm:"type:varchar(16)"`
	// StopPrice     string `gorm:"type:text"`
}

// AggregateTransaction is stats on all the transactions that occurred in a time period
type AggregateTransaction struct {
	ProductId                     string `gorm:"type:varchar(8); not null"`
	Granularity                   int    `gorm:"type:integer; not null"`
	TimeStarted                   int64  `gorm:"type:integer; not null"`
	TimeEnded                     int64  `gorm:"type:integer; not null"`
	FirstSequence                 int64  `gorm:"type:integer; not null"`
	LastSequence                  int64  `gorm:"type:integer; not null"`
	NumTransactionsSeen           int    `gorm:"type:integer; not null"`
	NumTransactionsOnBook         int    `gorm:"type:integer; not null"`
	NumNewTransactionsOnBook      int    `gorm:"type:integer; not null"`
	NumNewTransactionsStillOnBook int    `gorm:"type:integer; not null"`
	NumMatches                    int    `gorm:"type:integer; not null"`
	NumBuys                       int    `gorm:"type:integer; not null"`
	NumOpenBuys                   int    `gorm:"type:integer; not null"`
	NumFilledBuys                 int    `gorm:"type:integer; not null"`
	NumSells                      int    `gorm:"type:integer; not null"`
	NumOpenSells                  int    `gorm:"type:integer; not null"`
	NumFilledSells                int    `gorm:"type:integer; not null"`
	NumCancelledBuys              int    `gorm:"type:integer; not null"`
	NumCancelledSells             int    `gorm:"type:integer; not null"`
	NumLimitBuys                  int    `gorm:"type:integer; not null"`
	NumLimitSells                 int    `gorm:"type:integer; not null"`
	NumMarketBuys                 int    `gorm:"type:integer; not null"`
	NumMarketSells                int    `gorm:"type:integer; not null"`

	// Get open data
	AvgOpenBuyPrice     string `gorm:"type:text; not null"`
	AvgOpenSellPrice    string `gorm:"type:text; not null"`
	MedianOpenBuyPrice  string `gorm:"type:text; not null"`
	MedianOpenSellPrice string `gorm:"type:text; not null"`
	AvgOpenBuySize      string `gorm:"type:text; not null"`
	AvgOpenSellSize     string `gorm:"type:text; not null"`
	MedianOpenBuySize   string `gorm:"type:text; not null"`
	MedianOpenSellSize  string `gorm:"type:text; not null"`

	// These are all matched prices, not irrelevant ones
	HighestPrice  string `gorm:"type:text; not null"`
	LowestPrice   string `gorm:"type:text; not null"`
	AvgPrice      string `gorm:"type:text; not null"`
	MedianPrice   string `gorm:"type:text; not null"`
	HighestSize   string `gorm:"type:text; not null"`
	LowestSize    string `gorm:"type:text; not null"`
	AvgSize       string `gorm:"type:text; not null"`
	MedianSize    string `gorm:"type:text; not null"`
	AmtCoinTraded string `gorm:"type:text; not null"`
}
