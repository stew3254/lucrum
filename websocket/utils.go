package websocket

import (
	"container/list"
	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/stew3254/ratelimit"
	"gorm.io/gorm"
	"log"
	"lucrum/database"
	"sync"
	"time"
)

type Transactions struct {
	transactions map[string]map[string]*database.Transaction
	lock         *sync.RWMutex
}

// Pop removes all the transactions from the map gives them as a struct
func (t *Transactions) Pop(productId string) (out []database.Transaction) {
	t.lock.Lock()
	out = make([]database.Transaction, 0, len(t.transactions[productId]))
	for _, v := range t.transactions[productId] {
		out = append(out, *v)
	}
	t.transactions[productId] = make(map[string]*database.Transaction)
	t.lock.Unlock()
	return out
}

func (t *Transactions) ToSlice(productId string) (transactions []*database.Transaction) {
	transactions = make([]*database.Transaction, 0, len(t.transactions))
	t.lock.RLock()
	for _, v := range t.transactions[productId] {
		transactions = append(transactions, v)
	}
	t.lock.RUnlock()
	return transactions
}

func (t *Transactions) FromSlice(productId string, transactions []*database.Transaction) {
	t.lock.Lock()
	t.transactions[productId] = make(map[string]*database.Transaction)
	for _, v := range transactions {
		t.transactions[productId][v.OrderID] = v
	}
	t.lock.Unlock()
}

func (t *Transactions) Get(productId, orderId string) *database.Transaction {
	t.lock.RLock()
	tmp := t.transactions[productId][orderId]
	t.lock.RUnlock()
	return tmp
}

func (t *Transactions) Set(productId, orderId string, v *database.Transaction) {
	t.lock.Lock()
	t.transactions[productId][orderId] = v
	t.lock.Unlock()
}

// Update a value with the given function
func (t *Transactions) Update(
	productId string,
	orderId string,
	f func(transaction *database.Transaction)) {
	transaction := t.Get(productId, orderId)
	t.lock.Lock()
	f(transaction)
	t.lock.Unlock()
	t.Set(productId, orderId, transaction)
}

func (t *Transactions) Remove(productId, orderId string) {
	t.lock.Lock()
	delete(t.transactions[productId], orderId)
	t.lock.Unlock()
}

func NewTransactions(productIds []string) *Transactions {
	t := &Transactions{
		transactions: make(map[string]map[string]*database.Transaction),
		lock:         &sync.RWMutex{},
	}
	// Initialize the map
	for _, productId := range productIds {
		t.transactions[productId] = make(map[string]*database.Transaction)
	}
	return t
}

type Book struct {
	Sequence int64
	Buys     *list.List
	Sells    *list.List
}

type OrderBook struct {
	books map[string]*Book
	lock  *sync.RWMutex
}

func (o *OrderBook) Get(productId string) *Book {
	o.lock.RLock()
	b := o.books[productId]
	o.lock.RUnlock()
	return b
}

func (o *OrderBook) Set(productId string, book *Book) {
	o.lock.Lock()
	o.books[productId] = book
	o.lock.Unlock()
}

func (o *OrderBook) Remove(productId string) {
	o.lock.Lock()
	delete(o.books, productId)
	o.lock.Unlock()
}

// Save all books to the db
func (o *OrderBook) Save(db *gorm.DB) {
	o.lock.RLock()
	for _, book := range o.books {
		obListToDB(book.Buys, db, 10000)
		obListToDB(book.Sells, db, 10000)
	}
	o.lock.RUnlock()
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		books: make(map[string]*Book),
		lock:  &sync.RWMutex{},
	}
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
	snapshot = NewOrderBook()
	for i := 0; i < len(productIds); i++ {
		// Create the function to create a snapshot
		toSnapshot := func(isAsk bool, entry coinbasepro.BookEntry) database.OrderBookSnapshot {
			return database.OrderBookSnapshot{
				ProductId: productIds[i],
				Sequence:  books[i].Sequence,
				IsAsk:     isAsk,
				Time:      0,
				Price:     entry.Price,
				Size:      entry.Size,
				OrderID:   entry.OrderID,
			}
		}
		// Create the book
		book := &Book{
			Sequence: books[i].Sequence,
			Buys:     list.New(),
			Sells:    list.New(),
		}

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
				db.Create(entries[:i])
				// db.Clauses(clause.OnConflict{
				// 	Columns:   []clause.Column{{Name: "order_id"}},
				// 	DoNothing: true,
				// }).Create(entries)
				return
			}

			entries[i] = e.Value.(database.OrderBookSnapshot)
			e = e.Next()
		}
		// Add entries to the database
		db.Create(entries)
		// db.Clauses(clause.OnConflict{
		// 	Columns:   []clause.Column{{Name: "order_id"}},
		// 	DoNothing: true,
		// }).Create(entries)
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
		now := time.Now()
		entry := database.OrderBookSnapshot{
			ProductId: msg.ProductID,
			Sequence:  msg.Sequence,
			Time:      now.UnixMicro(),
			Price:     msg.Price,
			Size:      msg.RemainingSize,
			OrderID:   msg.OrderID,
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

// SaveOrderBook writes out the book to the database
func SaveOrderBook(book *Book, db *gorm.DB) {
	update := func(l *list.List, sequence int64) {
		for e := l.Front(); e != nil; e = e.Next() {
			v := e.Value.(database.OrderBookSnapshot)
			v.Sequence = sequence
			e.Value = v
		}
		obListToDB(l, db, 10000)
	}
	update(book.Buys, book.Sequence)
	update(book.Sells, book.Sequence)
}
