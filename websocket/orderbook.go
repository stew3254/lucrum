package websocket

import (
	"container/list"
	"gorm.io/gorm"
	"lucrum/database"
	"sync"
)

type Book struct {
	Sequence int64
	buyLock  *sync.RWMutex
	sellLock *sync.RWMutex
	buys     *list.List
	sells    *list.List
}

func (b *Book) BuysIter(stop <-chan struct{}) (iter chan database.OrderBookSnapshot) {
	return iterHelper(b.buys, b.buyLock, stop)
}

func (b *Book) SellsIter(stop <-chan struct{}) (iter chan database.OrderBookSnapshot) {
	return iterHelper(b.sells, b.sellLock, stop)
}

func (b *Book) AddBuy(snapshot database.OrderBookSnapshot) {
	b.buyLock
}

func (b *Book) Len() int {
	b.buyLock.RLock()
	b.sellLock.RLock()
	defer b.buyLock.RUnlock()
	defer b.sellLock.RUnlock()
	return b.buys.Len() + b.sells.Len()
}

func (b *Book) BuyLen() int {
	b.buyLock.RLock()
	defer b.buyLock.RUnlock()
	return b.buys.Len()
}

func (b *Book) SellLen() int {
	b.sellLock.RLock()
	defer b.sellLock.RUnlock()
	return b.sells.Len()
}

func NewBook(sequence int64) *Book {
	return &Book{
		Sequence: sequence,
		buyLock:  &sync.RWMutex{},
		sellLock: &sync.RWMutex{},
		buys:     list.New(),
		sells:    list.New(),
	}
}

type OrderBook struct {
	books map[string]*Book
	lock  *sync.RWMutex
}

func (o *OrderBook) Get(productId string) *Book {
	o.lock.RLock()
	defer o.lock.RUnlock()
	return o.books[productId]
}

func (o *OrderBook) Set(productId string, book *Book) {
	o.lock.Lock()
	defer o.lock.Unlock()
	o.books[productId] = book
}

func (o *OrderBook) Remove(productId string) {
	o.lock.Lock()
	defer o.lock.Unlock()
	delete(o.books, productId)
}

func (o *OrderBook) Len() int {
	o.lock.RLock()
	defer o.lock.Unlock()
	return len(o.books)
}

// Save all books to the db
func (o *OrderBook) Save(db *gorm.DB) {
	o.lock.RLock()
	defer o.lock.RUnlock()
	for _, book := range o.books {
		obListToDB(book.buys, db, 10000)
		obListToDB(book.sells, db, 10000)
	}
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		books: make(map[string]*Book),
		lock:  &sync.RWMutex{},
	}
}

func iterHelper(
	l *list.List,
	lock *sync.RWMutex,
	stop <-chan struct{},
) (iter chan database.OrderBookSnapshot) {
	iter = make(chan database.OrderBookSnapshot, 10)
	go func(stop <-chan struct{}) {
		lock.RLock()
		// Unlock and clean up the channel at the end
		defer func() {
			lock.RUnlock()
			close(iter)
		}()

		for e := l.Front(); e != nil; e = e.Next() {
			v := e.Value.(database.OrderBookSnapshot)
			// Write the transaction or stop
			select {
			case iter <- v:
			case <-stop:
				return
			}
		}
	}(stop)
	return iter
}