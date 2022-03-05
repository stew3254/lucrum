package websocket

import (
	"context"
	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"log"
	"lucrum/config"
	"lucrum/database"
	"lucrum/lib"
	"sort"
	"sync"
	"time"
)

func initAggregates(productIds []string, granularity int) (m map[string]*database.
	AggregateTransaction) {
	m = make(map[string]*database.AggregateTransaction)
	for _, productId := range productIds {
		m[productId] = &database.AggregateTransaction{
			ProductId:                     productId,
			Granularity:                   granularity,
			TimeStarted:                   0,
			TimeEnded:                     0,
			FirstSequence:                 0,
			LastSequence:                  0,
			NumTransactionsSeen:           0,
			NumTransactionsOnBook:         0,
			NumNewTransactionsOnBook:      0,
			NumNewTransactionsStillOnBook: 0,
			NumMatches:                    0,
			NumBuys:                       0,
			NumOpenBuys:                   0,
			NumFilledBuys:                 0,
			NumSells:                      0,
			NumOpenSells:                  0,
			NumFilledSells:                0,
			NumCancelledBuys:              0,
			NumCancelledSells:             0,
			NumLimitBuys:                  0,
			NumLimitSells:                 0,
			NumMarketBuys:                 0,
			NumMarketSells:                0,
			AvgOpenBuyPrice:               "0",
			AvgOpenSellPrice:              "0",
			MedianOpenBuyPrice:            "0",
			MedianOpenSellPrice:           "0",
			AvgOpenBuySize:                "0",
			AvgOpenSellSize:               "0",
			MedianOpenBuySize:             "0",
			MedianOpenSellSize:            "0",
			HighestPrice:                  "0",
			LowestPrice:                   "0",
			AvgPrice:                      "0",
			MedianPrice:                   "0",
			HighestSize:                   "0",
			LowestSize:                    "0",
			AvgSize:                       "0",
			MedianSize:                    "0",
			AmtCoinTraded:                 "0",
		}
	}
	return m
}

type AggregateCtx struct {
	Aggregate *database.AggregateTransaction
	Open      map[string]coinbasepro.Message
	Prices    map[string][]decimal.Decimal
	Sizes     map[string][]decimal.Decimal
}

func aggregateMsg(ctx AggregateCtx, msg coinbasepro.Message) {
	// Set first sequence only if not set
	if ctx.Aggregate.FirstSequence == 0 {
		ctx.Aggregate.FirstSequence = msg.Sequence
	}
	// Always set last sequence
	ctx.Aggregate.LastSequence = msg.Sequence

	// Set time started if not set
	if ctx.Aggregate.TimeStarted == 0 {
		ctx.Aggregate.TimeStarted = msg.Time.Time().UnixMicro()
	}

	// Always set last time
	ctx.Aggregate.TimeEnded = msg.Time.Time().UnixMicro()

	switch msg.Type {
	case "received":
		// Bump up number of transactions seen
		ctx.Aggregate.NumTransactionsSeen++

		if msg.Side == "buy" {
			ctx.Aggregate.NumBuys++
			if msg.OrderType == "limit" {
				ctx.Aggregate.NumLimitBuys++
			} else if msg.OrderType == "market" {
				ctx.Aggregate.NumMarketBuys++
			}
		} else {
			ctx.Aggregate.NumSells++
			if msg.OrderType == "limit" {
				ctx.Aggregate.NumLimitSells++
			} else if msg.OrderType == "market" {
				ctx.Aggregate.NumMarketSells++
			}
		}
	case "open":
		ctx.Open[msg.OrderID] = msg
		// New transaction on the book
		ctx.Aggregate.NumNewTransactionsOnBook++
		ctx.Aggregate.NumNewTransactionsStillOnBook++

		if msg.Side == "buy" {
			ctx.Aggregate.NumOpenBuys++
		} else {
			ctx.Aggregate.NumOpenSells++
		}
	case "done":
		// Remove still on book since it's not there anymore
		if _, exists := ctx.Open[msg.OrderID]; exists {
			ctx.Aggregate.NumNewTransactionsStillOnBook--
			delete(ctx.Open, msg.OrderID)
		}

		if msg.Side == "buy" {
			if msg.Reason == "filled" {
				ctx.Aggregate.NumFilledBuys++
			} else if msg.Reason == "canceled" {
				ctx.Aggregate.NumCancelledBuys++
			}
		} else {
			if msg.Reason == "filled" {
				ctx.Aggregate.NumFilledSells++
			} else if msg.Reason == "canceled" {
				ctx.Aggregate.NumCancelledSells++
			}
		}
	case "match":
		ctx.Aggregate.NumMatches++

		// Add the price and size to the book
		ctx.Prices[msg.ProductID] = append(ctx.Prices[msg.ProductID], lib.StringToDecimal(msg.Price))
		ctx.Sizes[msg.ProductID] = append(ctx.Sizes[msg.ProductID], lib.StringToDecimal(msg.Size))
	}
}

// AggregateTransactions reads in messages from the MsgReader and computes statistics
// based on the live messages that come in. At a defined granularity it will stop and wait
// until the timer is up to then finish computing and save to the database
func AggregateTransactions(
	ctx context.Context,
	wg *sync.WaitGroup,
	alert chan<- struct{},
	productIds []string,
	lastSequence map[string]int64,
) {
	// Get configurations and db
	cli := ctx.Value(lib.LucrumKey("conf")).(config.Configuration).CLI
	wsConf := ctx.Value(lib.LucrumKey("conf")).(config.Configuration).Type.Ws
	db := ctx.Value(lib.LucrumKey("db")).(*gorm.DB)

	// Tell the parent we're done
	defer func() {
		if cli.Verbose {
			log.Println("Stopped aggregator")
		}
		wg.Done()
	}()

	if cli.Verbose {
		log.Println("Started aggregator")
	}

	// Define the wait duration
	wait := time.Duration(wsConf.Granularity) * time.Second

	// Initialize all the aggregates
	aggregateMap := initAggregates(productIds, wsConf.Granularity)

	// Create the maps so we can track stuff for later
	open := make(map[string]coinbasepro.Message, 0)
	prices := make(map[string][]decimal.Decimal, 0)
	sizes := make(map[string][]decimal.Decimal, 0)
	times := make(map[string][]int64, 0)

	// Subscribe to the channels
	channels := MsgSubscribe(productIds)

	// This handles any new messages that were read
	handleMsg := func(msg coinbasepro.Message, ok bool, lastSequence int64, timer *time.Ticker) int64 {
		// Something is wrong with the channel so just quit and lose all progress
		if !ok {
			// TODO find a nice way to clean this up and not waste messages
			return lastSequence
		}

		// We lost a message and need to handle an intelligent cleanup
		if msg.Sequence > lastSequence+1 {
			// TODO handle cleaning up nicely
			// start over
			lastSequence = msg.Sequence
			timer.Reset(wait)
			return lastSequence
		}

		// Bump up the last sequence
		lastSequence++

		// Update the aggregate with the message
		aggregateMsg(AggregateCtx{
			Aggregate: aggregateMap[msg.ProductID],
			Open:      open,
			Prices:    prices,
			Sizes:     sizes,
		}, msg)

		return lastSequence
	}

	// Tell the parent handler it can start the MsgReader
	select {
	case alert <- struct{}{}:
	case <-ctx.Done():
		return
	}

	// Wait for the messages
	ticker := time.NewTicker(wait)
	for {
		for _, productId := range productIds {
			select {
			// See if a message has come in yet
			case msg, ok := <-channels[productId]:
				lastSequence[msg.ProductID] = handleMsg(msg, ok, lastSequence[msg.ProductID], ticker)

			case <-ticker.C:
				// Make sure everything is caught up to the book
				for _, productId := range productIds {
					// Wait for the book to catch up
					for lastSequence[productId] > lib.Books.Get(productId, true).GetSequence(true) {
						// Try to do a short sleep while we wait
						time.Sleep(10 * time.Microsecond)
					}
					// We might have gotten behind the book, so lock it and wait to catch up again
					lib.Books.Lock.RLock()
					for lib.Books.Get(productId, false).GetSequence(true) > lastSequence[productId] {
						// Wait for more messages to catch up but die if interrupted
						select {
						case msg, ok := <-channels[productId]:
							lastSequence[productId] = handleMsg(msg, ok, lastSequence[productId], ticker)
						case <-ctx.Done():
							return
						}
					}
					// Update the number of transactions on the book
					aggregateMap[productId].NumTransactionsOnBook = lib.Books.Get(productId, false).Len(true)
					lib.Books.Lock.RUnlock()
				}

				openBuyPrices := make(map[string][]decimal.Decimal)
				openSellPrices := make(map[string][]decimal.Decimal)
				openBuySizes := make(map[string][]decimal.Decimal)
				openSellSizes := make(map[string][]decimal.Decimal)
				for _, msg := range open {
					// Add the price and size to the book
					if msg.Side == "buy" {
						openBuyPrices[msg.ProductID] = append(openBuyPrices[msg.ProductID], lib.StringToDecimal(msg.Price))
						openBuySizes[msg.ProductID] = append(openBuySizes[msg.ProductID],
							lib.StringToDecimal(msg.RemainingSize))
					} else {
						openSellPrices[msg.ProductID] = append(openSellPrices[msg.ProductID], lib.StringToDecimal(msg.Price))
						openSellSizes[msg.ProductID] = append(openSellSizes[msg.ProductID],
							lib.StringToDecimal(msg.RemainingSize))
					}
				}

				// Timer expired, now time to save the aggregate to the db
				aggregates := make([]database.AggregateTransaction, 0, len(aggregateMap))
				for _, productId := range productIds {
					aggregate := aggregateMap[productId]
					p := prices[productId]
					s := sizes[productId]
					t := times[productId]
					obp := openBuyPrices[productId]
					obs := openBuySizes[productId]
					osp := openSellPrices[productId]
					oss := openSellSizes[productId]
					sort.Slice(p, func(i, j int) bool {
						return p[i].LessThan(p[j])
					})
					sort.Slice(s, func(i, j int) bool {
						return s[i].LessThan(s[j])
					})
					sort.Slice(t, func(i, j int) bool {
						return t[i] < t[j]
					})
					sort.Slice(obp, func(i, j int) bool {
						return obp[i].LessThan(obp[j])
					})
					sort.Slice(obs, func(i, j int) bool {
						return obs[i].LessThan(obs[j])
					})
					sort.Slice(osp, func(i, j int) bool {
						return osp[i].LessThan(osp[j])
					})
					sort.Slice(oss, func(i, j int) bool {
						return oss[i].LessThan(oss[j])
					})

					// Get averages for open data
					aggregate.AvgOpenBuyPrice = lib.AverageDec(obp)
					aggregate.AvgOpenBuySize = lib.AverageDec(obs)
					aggregate.AvgOpenSellPrice = lib.AverageDec(osp)
					aggregate.AvgOpenSellSize = lib.AverageDec(oss)

					// Get median for open data
					aggregate.MedianOpenBuyPrice = lib.MedianDec(obp)
					aggregate.MedianOpenBuySize = lib.MedianDec(obs)
					aggregate.MedianOpenSellPrice = lib.MedianDec(osp)
					aggregate.MedianOpenSellSize = lib.MedianDec(oss)

					// Get price data
					if len(p) > 0 {
						aggregate.HighestPrice = p[len(p)-1].String()
						aggregate.LowestPrice = p[0].String()
					}
					aggregate.AvgPrice = lib.AverageDec(p)
					aggregate.MedianPrice = lib.MedianDec(p)

					// Get size data
					if len(p) > 0 {
						aggregate.HighestSize = s[len(s)-1].String()
						aggregate.LowestSize = s[0].String()
					}
					aggregate.AvgSize = lib.AverageDec(s)
					aggregate.MedianSize = lib.MedianDec(s)
					traded := func(d []decimal.Decimal) string {
						if len(d) == 0 {
							return "0"
						} else if len(d) == 1 {
							return d[0].String()
						} else {
							return decimal.Sum(d[0], s[1:]...).String()
						}
					}
					aggregate.AmtCoinTraded = traded(s)

					aggregates = append(aggregates, *aggregate)
				}

				// Save the aggregates to the db
				db.Create(&aggregates)
				if cli.Verbose {
					log.Println("Saved aggregate transaction")
				}

				// Re-initialize the aggregates
				aggregateMap = initAggregates(productIds, wsConf.Granularity)
				open = make(map[string]coinbasepro.Message, 0)
				prices = make(map[string][]decimal.Decimal, 0)
				sizes = make(map[string][]decimal.Decimal, 0)
			case <-ctx.Done():
				return
			default:
				// Nothing has happened, skip to look at the next channel
				continue
			}
		}
	}
}
