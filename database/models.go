package database

type L3OrderMessage struct {
	Id            int64  `gorm:"type:int; primaryKey; not null"`
	Type          string `gorm:"type:text; not null"`
	ProductID     string `gorm:"type:text; not null"`
	TradeID       int    `gorm:"type:int"`
	OrderID       string `gorm:"type:varchar(128)"`
	Sequence      int64  `gorm:"type:int; default:0; not null"`
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

type AggregateTransaction struct {
	ProductId                string
	Granularity              int64
	TimeStarted              int64
	NumTransactionsSeen      int
	NumTransactionsOnBook    int
	NumNewTransactionsOnBook int
	NumMatches               int
	NumBuys                  int
	NumOpenBuys              int
	NumFilledBuys            int
	NumSells                 int
	NumOpenSells             int
	NumFilledSells           int
	NumCancelledBuys         int
	NumCancelledSells        int
	NumLimitBuys             int
	NumLimitSells            int
	NumMarketBuys            int
	NumMarketSells           int
	NumTakerBuys             int
	NumTakerSells            int
	AmtCoinTraded            string
	AvgTimeBetweenTrades     int
	AvgOpenBuyPrice          string
	AvgOpenSellPrice         string
	MedianOpenBuyPrice       string
	MedianOpenSellPrice      string
	AvgOpenBuySize           string
	AvgOpenSellSize          string
	MedianOpenBuySize        string
	MedianOpenSellSize       string

	// These are all matched prices, not irrelevant ones
	HighestPrice string
	LowestPrice  string
	AvgPrice     string
	MedianPrice  string
	HighestSize  string
	LowestSize   string
	AvgSize      string
	MedianSize   string
}
