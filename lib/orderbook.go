package lib

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

type Book struct {
	sequence int64
	Lock     *sync.RWMutex
	BuyLock  *sync.RWMutex
	SellLock *sync.RWMutex
	buys     *list.List
	sells    *list.List
}

// BuysIter allows you to read over the list in a thread safe way
func (b *Book) BuysIter(stop <-chan struct{}, shouldLock bool) (iter <-chan *list.Element) {
	return IterHelper(b.buys, b.BuyLock.RLocker(), shouldLock, stop)
}

// SellsIter allows you to read over the list in a thread safe way
func (b *Book) SellsIter(stop <-chan struct{}, shouldLock bool) (iter <-chan *list.Element) {
	return IterHelper(b.sells, b.SellLock.RLocker(), shouldLock, stop)
}

// BuysWriteIter allows you to read and modify the list in a thread safe way
func (b *Book) BuysWriteIter(stop <-chan struct{}, shouldLock bool) (iter <-chan *list.Element) {
	return IterHelper(b.buys, b.BuyLock, shouldLock, stop)
}

// SellsWriteIter allows you to read and modify the list in a thread safe way
func (b *Book) SellsWriteIter(stop <-chan struct{}, shouldLock bool) (iter <-chan *list.Element) {
	return IterHelper(b.sells, b.SellLock, shouldLock, stop)
}

func (b *Book) AddBuy(snapshot database.OrderBookSnapshot, shouldLock bool) {
	if shouldLock {
		defer b.BuyLock.Unlock()
		b.BuyLock.Lock()
	}
	b.buys.PushFront(snapshot)
}

func (b *Book) AddSell(snapshot database.OrderBookSnapshot, shouldLock bool) {
	if shouldLock {
		defer b.SellLock.Unlock()
		b.SellLock.Lock()
	}
	b.sells.PushFront(snapshot)
}

func remove(lock *sync.RWMutex, shouldLock bool, l *list.List, e *list.Element) {
	if shouldLock {
		defer lock.Unlock()
		lock.Lock()
	}
	l.Remove(e)
}

func (b *Book) RemoveBuy(e *list.Element, shouldLock bool) {
	remove(b.BuyLock, shouldLock, b.buys, e)
}

func (b *Book) RemoveSell(e *list.Element, shouldLock bool) {
	remove(b.SellLock, shouldLock, b.sells, e)
}

func (b *Book) GetSequence(shouldLock bool) int64 {
	if shouldLock {
		defer b.Lock.RUnlock()
		b.Lock.RLock()
	}
	return b.sequence
}

func (b *Book) SetSequence(sequence int64, shouldLock bool) {
	if shouldLock {
		defer b.Lock.Unlock()
		b.Lock.Lock()
	}
	b.sequence = sequence
}

func (b *Book) Len(shouldLock bool) int {
	if shouldLock {
		defer func() {
			b.BuyLock.RUnlock()
			b.SellLock.RUnlock()
		}()
		b.BuyLock.RLock()
		b.SellLock.RLock()
	}
	return b.buys.Len() + b.sells.Len()
}

func (b *Book) BuyLen(shouldLock bool) int {
	if shouldLock {
		defer b.BuyLock.RUnlock()
		b.BuyLock.RLock()
	}
	return b.buys.Len()
}

func (b *Book) SellLen(shouldLock bool) int {
	if shouldLock {
		defer b.SellLock.RUnlock()
		b.SellLock.RLock()
	}
	return b.sells.Len()
}

func NewBook(sequence int64) *Book {
	return &Book{
		sequence: sequence,
		Lock:     &sync.RWMutex{},
		BuyLock:  &sync.RWMutex{},
		SellLock: &sync.RWMutex{},
		buys:     list.New(),
		sells:    list.New(),
	}
}

type OrderBook struct {
	books map[string]*Book
	Lock  *sync.RWMutex
}

// Clean wipes the order book clean
func (o *OrderBook) Clean(shouldLock bool) {
	if shouldLock {
		defer o.Lock.Unlock()
		o.Lock.Lock()
	}
	for k, _ := range o.books {
		// Clean the books
		o.books[k] = nil
	}
}

func (o *OrderBook) Get(productId string, shouldLock bool) *Book {
	if shouldLock {
		defer o.Lock.RUnlock()
		o.Lock.RLock()
	}
	return o.books[productId]
}

func (o *OrderBook) Set(productId string, book *Book, shouldLock bool) {
	if shouldLock {
		defer o.Lock.Unlock()
		o.Lock.Lock()
	}
	o.books[productId] = book
}

func (o *OrderBook) Remove(productId string, shouldLock bool) {
	if shouldLock {
		defer o.Lock.Unlock()
		o.Lock.Lock()
	}
	delete(o.books, productId)
}

func (o *OrderBook) Len(shouldLock bool) int {
	if shouldLock {
		defer o.Lock.Unlock()
		o.Lock.RLock()
	}
	return len(o.books)
}

// Save all books to the db
func (o *OrderBook) Save(db *gorm.DB, shouldLock bool) {
	if shouldLock {
		defer o.Lock.RUnlock()
		o.Lock.RLock()
	}
	for _, book := range o.books {
		buyStop := make(chan struct{})
		sellStop := make(chan struct{})
		ObListToDB(db, book.BuysIter(buyStop, true), buyStop, 10000)
		ObListToDB(db, book.SellsIter(sellStop, true), sellStop, 10000)
	}
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		books: make(map[string]*Book),
		Lock:  &sync.RWMutex{},
	}
}

func GetOrderBooks(
	snapshot **OrderBook,
	client *coinbasepro.Client,
	rl *ratelimit.RateLimiter,
	productIds []string,
) {
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

	once := sync.Once{}
	if *snapshot == nil {
		// Create the order book
		*snapshot = NewOrderBook()
		(*snapshot).Lock.Lock()
	} else {
		// Clean up the order book
		(*snapshot).Lock.Lock()
		(*snapshot).Clean(false)
	}
	defer once.Do((*snapshot).Lock.Unlock)

	now := time.Now().UnixMicro()
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
			book.AddSell(toSnapshot(true, ask), true)
		}

		for _, bid := range books[i].Bids {
			book.AddBuy(toSnapshot(false, bid), true)
		}
		// Add the book to the order book
		(*snapshot).Set(productIds[i], book, false)
	}
}

// UpdateOrderBook handles interpreting messages from the full channel
// and applying them to the internal order book
func UpdateOrderBook(book *Book, msg coinbasepro.Message) {
	// Have to lock to book since way too much needs to change
	var once sync.Once
	// Unlock the book at the end
	defer once.Do(book.Lock.Unlock)

	book.Lock.Lock()
	sequence := book.GetSequence(false)

	// Ignore old messages
	if msg.Sequence <= sequence {
		return
	} else if msg.Sequence > sequence+1 {
		// TODO fill this out when you think of a better strategy than ignoring it
		return
	}

	// Update the book's sequence number, so we know what the last message seen is
	book.SetSequence(msg.Sequence, false)
	once.Do(book.Lock.Unlock)

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
			book.AddBuy(entry, true)
		} else {
			entry.IsAsk = true
			book.AddSell(entry, true)
		}
	case "done":
		// Make sure the stop channel is unbuffered, so we know the other side received it
		// and quits before unlocking the book
		stop := make(chan struct{})
		var ch <-chan *list.Element
		// Get the entries we care about
		if msg.Side == "buy" {
			// Need extra flexibility while locking
			defer book.BuyLock.Unlock()
			book.BuyLock.Lock()
			ch = book.BuysWriteIter(stop, false)
		} else {
			// Need extra flexibility while locking
			defer book.SellLock.Unlock()
			book.SellLock.Lock()
			ch = book.SellsWriteIter(stop, false)
		}

		// Remove the entry from the book if it exists
		channelCleanup := sync.Once{}
		cleanup := func() {
			stop <- struct{}{}
			close(stop)
		}

		for elem := range ch {
			if elem.Value.(database.OrderBookSnapshot).OrderID == msg.OrderID {
				// Remove the entry
				channelCleanup.Do(cleanup)
				if msg.Side == "buy" {
					book.RemoveBuy(elem, false)
				} else {
					book.RemoveSell(elem, false)
				}
				break
			}
		}
		channelCleanup.Do(cleanup)
	case "change":
		// Get the entries we care about
		stop := make(chan struct{})
		var ch <-chan *list.Element
		if msg.Side == "buy" {
			ch = book.BuysWriteIter(stop, true)
		} else {
			ch = book.SellsWriteIter(stop, true)
		}

		channelCleanup := sync.Once{}
		cleanup := func() {
			stop <- struct{}{}
			close(stop)
		}
		// Look through the order book to see if it's changing a resting order
		for elem := range ch {
			v := elem.Value.(database.OrderBookSnapshot)
			if v.OrderID == msg.OrderID {
				// Update the new size
				v.Size = msg.NewSize
				// Update the value again
				elem.Value = v
				channelCleanup.Do(cleanup)
				break
			}
		}
		channelCleanup.Do(cleanup)
	}
}
