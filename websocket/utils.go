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

var SubChans *SubscribedChannels

type SubscribedChannels struct {
	locks    map[string]*sync.RWMutex
	channels map[string][]chan coinbasepro.Message
}

func NewChannels(productIds []string) *SubscribedChannels {
	c := &SubscribedChannels{
		locks:    make(map[string]*sync.RWMutex),
		channels: make(map[string][]chan coinbasepro.Message),
	}
	for _, productId := range productIds {
		c.locks[productId] = &sync.RWMutex{}
		c.channels[productId] = make([]chan coinbasepro.Message, 0, 2)
	}
	return c
}

func (c *SubscribedChannels) Add(productId string) chan coinbasepro.Message {
	ch := make(chan coinbasepro.Message, 50)
	c.locks[productId].Lock()
	c.channels[productId] = append(c.channels[productId], ch)
	c.locks[productId].Unlock()
	return ch
}

func (c *SubscribedChannels) Remove(productId string, ch chan coinbasepro.Message) {
	c.locks[productId].Lock()
	slice := c.channels[productId]
	for i, v := range slice {
		if ch == v {
			slice = append(slice[:i], slice[i+1:]...)
			break
		}
	}
	c.channels[productId] = slice
	c.locks[productId].Unlock()
}

func (c *SubscribedChannels) Send(productId string, msg coinbasepro.Message) {
	c.locks[productId].RLock()
	for _, ch := range c.channels[productId] {
		ch <- msg
	}
	c.locks[productId].RUnlock()
}

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

func initAggregates(productIds []string, granularity int64) (m map[string]*database.AggregateTransaction) {
	m = make(map[string]*database.AggregateTransaction)
	now := time.Now().UnixMicro()
	for _, productId := range productIds {
		m[productId] = &database.AggregateTransaction{
			ProductId:                     productId,
			Granularity:                   granularity,
			TimeStarted:                   now,
			TimeEnded:                     0,
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
			AvgTimeBetweenTrades:          0,
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
	Times     map[string][]int64
}

func aggregateMsg(ctx AggregateCtx, msg coinbasepro.Message) {
	ctx.Times[msg.ProductID] = append(ctx.Times[msg.ProductID], msg.Time.Time().UnixMicro())

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

func MsgSubscribe(productIds []string) map[string]chan coinbasepro.Message {
	m := make(map[string]chan coinbasepro.Message)
	for _, productId := range productIds {
		m[productId] = SubChans.Add(productId)
	}
	return m
}

func MsgUnsubscribe(chanMap map[string]chan coinbasepro.Message) {
	for productId, ch := range chanMap {
		SubChans.Remove(productId, ch)
	}
}

func MsgReader(msgChan chan coinbasepro.Message, stop chan struct{}) {
	defer func() {
		close(stop)
		close(msgChan)
	}()

	for {
		select {
		case msg, ok := <-msgChan:
			if ok {
				SubChans.Send(msg.ProductID, msg)
			} else {
				// Channel doesn't work, so return
				return
			}
		case <-stop:
			return
		}
	}
}

func AggregateTransactions(
	conf config.Websocket,
	db *gorm.DB,
	stop chan struct{},
	alert chan struct{},
	productIds []string,
	lastSequence map[string]int64,
) {
	// Create the timer
	wait := time.Duration(conf.Granularity) * time.Second

	// Initialize all the aggregates
	aggregateMap := initAggregates(productIds, conf.Granularity)

	open := make(map[string]coinbasepro.Message, 0)
	prices := make(map[string][]decimal.Decimal, 0)
	sizes := make(map[string][]decimal.Decimal, 0)
	times := make(map[string][]int64, 0)

	// Make sure to close the stop channel
	defer close(stop)

	// Subscribe to the channels
	channels := MsgSubscribe(productIds)

	// This handles any new messages that were read
	handleMsg := func(msg coinbasepro.Message, ok bool, lastSequence int64, timer *time.Timer) int64 {
		if !ok {
			// TODO find a nice way to clean this up and not waste messages
			return lastSequence
		}

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
			Times:     times,
		}, msg)

		return lastSequence
	}

	// Tell the parent handler it can start the MsgReader
	alert <- struct{}{}

	// Wait for the messages
	timer := time.NewTimer(wait)
	for {
		for _, productId := range productIds {
			select {
			// See if a message has come in yet
			case msg, ok := <-channels[productId]:
				lastSequence[msg.ProductID] = handleMsg(msg, ok, lastSequence[msg.ProductID], timer)

			case <-timer.C:
				// Make sure everything is caught up to the book
				for _, productId := range productIds {
					// Wait for the book to catch up
					for lastSequence[productId] > Books.Get(productId).Sequence {
						// Try to do a short sleep while we wait
						time.Sleep(time.Millisecond)
					}
					// We might have gotten behind the book, so lock it and wait to catch up again
					Books.lock.RLock()
					for Books.Get(productId).Sequence > lastSequence[productId] {
						msg, ok := <-channels[productId]
						lastSequence[productId] = handleMsg(msg, ok, lastSequence[productId], timer)
					}
					// Update the number of transactions on the book
					aggregateMap[productId].NumTransactionsOnBook = Books.Get(productId).Len()
					Books.lock.RUnlock()
				}

				// Get ending time
				now := time.Now().UnixMicro()

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
					aggregate.AvgOpenBuyPrice = lib.Average(obp)
					aggregate.AvgOpenBuySize = lib.Average(obs)
					aggregate.AvgOpenSellPrice = lib.Average(osp)
					aggregate.AvgOpenSellSize = lib.Average(oss)

					// Get median for open data
					aggregate.MedianOpenBuyPrice = lib.Median(obp)
					aggregate.MedianOpenBuySize = lib.Median(obs)
					aggregate.MedianOpenSellPrice = lib.Median(osp)
					aggregate.MedianOpenSellSize = lib.Median(oss)

					// Get price data
					if len(p) > 0 {
						aggregate.HighestPrice = p[len(p)-1].String()
						aggregate.LowestPrice = p[0].String()
					}
					aggregate.AvgPrice = lib.Average(p)
					aggregate.MedianPrice = lib.Median(p)

					// Get size data
					if len(p) > 0 {
						aggregate.HighestSize = s[len(s)-1].String()
						aggregate.LowestSize = s[0].String()
					}
					aggregate.AvgSize = lib.Average(s)
					aggregate.MedianSize = lib.Median(s)
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

					// Get time between trades
					var avg int64 = 0
					for i := 0; i < len(t)-1; i++ {
						avg += t[i+1] - t[i]
					}
					if avg != 0 {
						avg /= int64(len(t) - 1)
					}
					aggregate.AvgTimeBetweenTrades = avg

					// Set the time we stopped getting new messages
					aggregate.TimeEnded = now

					aggregates = append(aggregates, *aggregate)
				}

				// Save the aggregates to the db
				go db.Create(&aggregates)
				log.Println("Saved aggregate transactions")

				// Re-initialize the aggregates
				aggregateMap = initAggregates(productIds, conf.Granularity)
				open = make(map[string]coinbasepro.Message, 0)
				prices = make(map[string][]decimal.Decimal, 0)
				sizes = make(map[string][]decimal.Decimal, 0)
				times = make(map[string][]int64, 0)

				// Reset the timer
				timer.Reset(wait)
			case <-stop:
				return
			default:
				// Nothing has happened, skip to look at the next channel
				continue
			}
		}
	}
}
