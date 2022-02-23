package websocket

import (
	"container/list"
	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/shopspring/decimal"
	"github.com/stew3254/ratelimit"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"lucrum/config"
	"lucrum/database"
	"lucrum/lib"
	"sort"
	"sync"
	"time"
)

// Books is the global order book used throughout the program for lookup
var Books *OrderBook

func GetOrderBooks(
	client *coinbasepro.Client,
	rl *ratelimit.RateLimiter,
	productIds []string,
) (snapshot *OrderBook) {
	wg := &sync.WaitGroup{}
	books := make([]coinbasepro.Book, 0)
	for _, product := range productIds {
		// Wait for each order book
		wg.Add(1)
		go func(product string) {
			rl.Lock()
			book, err := client.GetBook(product, 3)
			rl.Unlock()
			for err != nil && err.Error() == "Public rate limit exceeded" {
				// Bump up the limit
				rl.Increase()
				// Be brief on the critical section (although in 1 thread right now this doesn't matter)
				rl.Lock()
				// Get the historic rates
				book, err = client.GetBook(product, 3)
				rl.Unlock()
			}
			if err == nil {
				rl.Decrease()
			} else {
				log.Fatalln(err)
			}
			rl.Lock()
			books = append(books, book)
			rl.Unlock()
			wg.Done()
		}(product)
	}

	// Wait for all the order books to be populated
	wg.Wait()

	// Create a slice of snapshot entries to add to the database
	now := time.Now().UnixMicro()
	snapshot = NewOrderBook()
	for i := 0; i < len(productIds); i++ {
		// Create the function to create a snapshot
		toSnapshot := func(isAsk bool, entry coinbasepro.BookEntry) database.OrderBookSnapshot {
			return database.OrderBookSnapshot{
				ProductId:     productIds[i],
				FirstSequence: books[i].Sequence,
				LastSequence:  books[i].Sequence,
				IsAsk:         isAsk,
				Time:          now,
				Price:         entry.Price,
				Size:          entry.Size,
				OrderID:       entry.OrderID,
			}
		}
		// Create the book
		book := NewBook(books[i].Sequence)

		// Add all the asks and bids for this coin to the slice
		for _, ask := range books[i].Asks {
			book.Sells.PushFront(toSnapshot(true, ask))
		}

		for _, bid := range books[i].Bids {
			book.Buys.PushFront(toSnapshot(false, bid))
		}
		// Add the book to the order book
		snapshot.Set(productIds[i], book)
	}
	return snapshot
}

// Gradually converts an order book list to a slice and adds it to the database
func obListToDB(l *list.List, db *gorm.DB, size int) {
	// Convert the list to a slice
	e := l.Front()
	entries := make([]database.OrderBookSnapshot, size)
	for {
		for i := 0; i < size; i++ {
			// There are no entries left so add remainders and break out of the loop
			if e == nil {
				// db.Create(entries[:i])
				db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "order_id"}},
					DoUpdates: clause.AssignmentColumns([]string{"last_sequence", "price", "size"}),
				}).Create(entries[:i])
				return
			}

			entries[i] = e.Value.(database.OrderBookSnapshot)
			e = e.Next()
		}
		// Add entries to the database
		// db.Create(entries)
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "order_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_sequence"}),
		}).Create(entries)
	}
}

// UpdateOrderBook handles interpreting messages from the full channel
// and applying them to the internal order book
func UpdateOrderBook(book *Book, msg coinbasepro.Message) {
	// Ignore old messages
	if msg.Sequence <= book.Sequence {
		return
	} else if msg.Sequence > book.Sequence+1 {
		// The messages are too new, so we missed something. Get a snapshot from the API
	}
	// Handle based on the message type
	switch msg.Type {
	case "open":
		// Create the order book snapshot
		now := time.Now().UnixMicro()
		entry := database.OrderBookSnapshot{
			ProductId:     msg.ProductID,
			FirstSequence: msg.Sequence,
			LastSequence:  msg.Sequence,
			Time:          now,
			Price:         msg.Price,
			Size:          msg.RemainingSize,
			OrderID:       msg.OrderID,
		}

		// Add the order to the front of the book
		if msg.Side == "buy" {
			entry.IsAsk = false
			book.Buys.PushFront(entry)
		} else {
			entry.IsAsk = true
			book.Sells.PushFront(entry)
		}
	case "done":
		// Get the entries we care about
		var entries *list.List
		if msg.Side == "buy" {
			entries = book.Buys
		} else {
			entries = book.Sells
		}

		// Remove the entry from the book if it exists
		for e := entries.Front(); e != nil; e = e.Next() {
			if e.Value.(database.OrderBookSnapshot).OrderID == msg.OrderID {
				// Remove the entry
				entries.Remove(e)
				break
			}
		}
	case "change":
		// Get the entries we care about
		var entries *list.List
		if msg.Side == "buy" {
			entries = book.Buys
		} else {
			entries = book.Sells
		}

		// Look through the order book to see if it's changing a resting order
		for e := entries.Front(); e != nil; e = e.Next() {
			v := e.Value.(database.OrderBookSnapshot)
			if v.OrderID == msg.OrderID {
				// Update the new size
				v.Size = msg.NewSize
				break
			}
			// Update the value again
			e.Value = v
		}
	}

	// Update the book's sequence number, so we know what the last message seen is
	book.Sequence = msg.Sequence
}

func UpdateTransactions(open, done *Transactions, msg coinbasepro.Message) {
	switch msg.Type {
	case "received":
		// Create the new transaction
		open.Set(msg.ProductID, msg.OrderID, &database.Transaction{
			OrderID:      msg.OrderID,
			ProductId:    msg.ProductID,
			Time:         msg.Time.Time().UnixMicro(),
			Price:        msg.Price,
			Side:         msg.Side,
			Size:         msg.Size,
			Funds:        msg.Funds,
			OrderType:    msg.OrderType,
			AddedToBook:  false,
			OrderChanged: false,
		})
	case "open":
		// Update the old transaction
		open.Update(msg.ProductID, msg.OrderID,
			func(transaction *database.Transaction) {
				transaction.AddedToBook = true
				transaction.RemainingSize = msg.RemainingSize
			})
	case "done":
		// Update the old transaction
		t, _ := open.Get(msg.ProductID, msg.OrderID)
		t.ClosedAt = msg.Time.Time().UnixMicro()
		t.Reason = msg.Reason
		// Add it to done transactions
		done.Set(msg.ProductID, msg.OrderID, t)
		// Remove it from the open transactions
		open.Remove(msg.ProductID, msg.OrderID)
	case "match":
		open.Update(msg.ProductID, msg.TakerOrderID,
			func(transaction *database.Transaction) {
				transaction.MatchId = msg.MakerOrderID
			})
		open.Update(msg.ProductID, msg.MakerOrderID,
			func(transaction *database.Transaction) {
				transaction.MatchId = msg.TakerOrderID
			})
	case "change":
		open.Update(msg.ProductID, msg.OrderID,
			func(transaction *database.Transaction) {
				transaction.OrderChanged = true
				transaction.NewSize = msg.NewSize
			})
	}
}

// TODO write save transactions

// AggregateContext is information the aggregate function needs
type AggregateContext struct {
	ProductIds []string
	Open       *Transactions
	OldOpen    *Transactions
	Done       *Transactions
}

func newAddedToBook(old, new *Transactions, productId string) (count int) {
	stop := make(chan struct{}, 1)
	defer close(stop)

	transactionChan := new.Iter(productId, stop)
	for {
		select {
		case transaction, ok := <-transactionChan:
			if !ok {
				return count
			}

			if _, exists := old.Get(productId, transaction.OrderID); !exists {
				// New item added to the book
				count++
			}
		}
	}
}

func handleOpen(ctx AggregateContext, aggregate *database.AggregateTransaction, productId string) {
	stop := make(chan struct{}, 1)
	defer close(stop)
	openChan := ctx.Open.Iter(productId, stop)

	buyPrices := make([]decimal.Decimal, 0)
	sellPrices := make([]decimal.Decimal, 0)
	buySizes := make([]decimal.Decimal, 0)
	sellSizes := make([]decimal.Decimal, 0)

	for {
		select {
		case transaction, ok := <-openChan:
			if !ok {
				goto done
			}

			price, err := decimal.NewFromString(transaction.Price)
			if err != nil {
				log.Println("Bad price for transaction", transaction.OrderID)
			}

			size, err := decimal.NewFromString(transaction.Size)
			if err != nil {
				log.Println("Bad size for transaction", transaction.OrderID)
			}

			if transaction.Side == "buy" {
				// Transaction is a buy
				buyPrices = append(buyPrices, price)
				buySizes = append(buySizes, size)

				aggregate.NumBuys++
				if transaction.AddedToBook {
					aggregate.NumOpenBuys++
				}

				if transaction.OrderType == "limit" {
					aggregate.NumLimitBuys++
				}
			} else {
				// Transaction is a sell
				sellPrices = append(sellPrices, price)
				sellSizes = append(sellSizes, size)

				aggregate.NumSells++
				if transaction.AddedToBook {
					aggregate.NumOpenSells++
				}

				if transaction.OrderType == "limit" {
					aggregate.NumLimitSells++
				}
			}

			if _, exists := ctx.OldOpen.Get(productId, transaction.OrderID); !exists {
				// New item added to the book
				aggregate.NumNewTransactionsOnBook++
				aggregate.NumTransactionsSeen++
			}
		}
	}

done:
	// Sort the slices
	sort.Slice(buyPrices, func(i, j int) bool {
		return buyPrices[i].LessThan(buyPrices[j])
	})
	sort.Slice(sellPrices, func(i, j int) bool {
		return buyPrices[i].LessThan(buyPrices[j])
	})
	sort.Slice(buySizes, func(i, j int) bool {
		return buySizes[i].LessThan(buySizes[j])
	})
	sort.Slice(sellSizes, func(i, j int) bool {
		return buySizes[i].LessThan(buySizes[j])
	})

	aggregate.AvgOpenBuyPrice = decimal.Avg(buyPrices[0], buyPrices[1:]...).String()
	aggregate.AvgOpenSellPrice = decimal.Avg(sellPrices[0], sellPrices[1:]...).String()
	aggregate.MedianOpenBuyPrice = lib.Median(buyPrices).String()
	aggregate.MedianOpenSellPrice = lib.Median(sellPrices).String()

	aggregate.AvgOpenBuySize = decimal.Avg(buySizes[0], buySizes[1:]...).String()
	aggregate.AvgOpenSellSize = decimal.Avg(sellSizes[0], sellSizes[1:]...).String()
	aggregate.MedianOpenBuySize = lib.Median(buySizes).String()
	aggregate.MedianOpenSellSize = lib.Median(sellSizes).String()
}

func handleDone(ctx AggregateContext, aggregate *database.AggregateTransaction, productId string) {
	stop := make(chan struct{}, 1)
	doneChan := ctx.Done.Iter(productId, stop)

	prices := make([]decimal.Decimal, 0)
	sizes := make([]decimal.Decimal, 0)

	for {
		select {
		case transaction, ok := <-doneChan:
			if !ok {
				goto done
			}

			// This is a new transaction that has occurred
			aggregate.NumTransactionsSeen++

			price, err := decimal.NewFromString(transaction.Price)
			if err != nil {
				log.Println("Bad price for transaction", transaction.OrderID)
			}

			size, err := decimal.NewFromString(transaction.Size)
			if err != nil {
				log.Println("Bad size for transaction", transaction.OrderID)
			}

			// Found a match
			if len(transaction.MatchId) > 0 {
				aggregate.NumMatches++
				// Add the matched prices and sizes
				prices = append(prices, price)
				sizes = append(sizes, size)
			}

			if transaction.Side == "buy" {
				// Transaction is a buy
				aggregate.NumBuys++

				if transaction.OrderType == "limit" {
					aggregate.NumLimitBuys++
				} else if transaction.OrderType == "market" {
					aggregate.NumMarketBuys++
				}

				if transaction.Reason == "filled" {
					aggregate.NumFilledBuys++
				} else if transaction.Reason == "canceled" {
					aggregate.NumCancelledBuys++
				}
			} else {
				// Transaction is a sell
				aggregate.NumSells++

				if transaction.AddedToBook {
					aggregate.NumOpenSells++
				}

				if transaction.OrderType == "limit" {
					aggregate.NumLimitSells++
				} else if transaction.OrderType == "market" {
					aggregate.NumMarketSells++
				}

				if transaction.Reason == "filled" {
					aggregate.NumFilledBuys++
				} else if transaction.Reason == "canceled" {
					aggregate.NumCancelledBuys++
				}
			}
		}
	}

done:
	// Sort the slices
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].LessThan(prices[j])
	})
	sort.Slice(sizes, func(i, j int) bool {
		return sizes[i].LessThan(sizes[j])
	})

	aggregate.HighestPrice = prices[len(prices)-1].String()
	aggregate.LowestPrice = prices[0].String()
	aggregate.AvgPrice = decimal.Avg(prices[0], prices[1:]...).String()
	aggregate.MedianPrice = lib.Median(prices).String()

	aggregate.HighestSize = sizes[len(sizes)-1].String()
	aggregate.LowestSize = sizes[0].String()
	aggregate.AvgSize = decimal.Avg(sizes[0], sizes[1:]...).String()
	aggregate.MedianSize = lib.Median(sizes).String()
}

func AggregateTransactions(
	conf config.Websocket,
	db *gorm.DB,
	ctx AggregateContext,
) {
	// Create our timer to wait for messages
	wait := time.Duration(conf.Granularity) * time.Second
	timer := time.NewTimer(wait)

	for {
		// Wait until the timer expires
		<-timer.C

		aggregates := make([]database.AggregateTransaction, 0, len(ctx.ProductIds))
		now := time.Now().UnixMicro()
		for _, productId := range ctx.ProductIds {
			aggregate := &database.AggregateTransaction{
				ProductId:             productId,
				Granularity:           conf.Granularity,
				TimeStarted:           now,
				NumTransactionsSeen:   ctx.Done.Len(),
				NumTransactionsOnBook: Books.Get(productId).Len(),
			}
			handleOpen(ctx, aggregate, productId)
			handleDone(ctx, aggregate, productId)
		}

		db.Create(&aggregates)

		// Reset the timer
		timer.Reset(wait)
	}

}
