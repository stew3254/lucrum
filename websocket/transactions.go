package websocket

import (
	"container/list"
	"lucrum/database"
	"sync"
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

func (t *Transactions) Get(productId, orderId string) (*database.Transaction, bool) {
	t.lock.RLock()
	tmp, exists := t.transactions[productId][orderId]
	t.lock.RUnlock()
	return tmp, exists
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
	transaction, exists := t.Get(productId, orderId)
	// Do nothing since this doesn't exist
	if !exists {
		return
	}
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

func NewTransactions(productIds []string, books *OrderBook) *Transactions {
	t := &Transactions{
		transactions: make(map[string]map[string]*database.Transaction),
		lock:         &sync.RWMutex{},
	}
	// Initialize the map
	for _, productId := range productIds {
		t.transactions[productId] = make(map[string]*database.Transaction)
	}

	// Add transactions from the order book
	if books != nil {
		for _, productId := range productIds {
			book := Books.Get(productId)
			addTransactions := func(l *list.List) {
				for e := l.Front(); e != nil; e = e.Next() {
					v := e.Value.(database.OrderBookSnapshot)
					transaction := &database.Transaction{
						OrderID:       v.OrderID,
						ProductId:     v.ProductId,
						Time:          v.Time,
						Price:         v.Price,
						Side:          "buy",
						Size:          v.Size,
						AddedToBook:   true,
						RemainingSize: v.Size,
					}
					t.Set(v.ProductId, v.OrderID, transaction)
				}
			}
			addTransactions(book.Buys)
			addTransactions(book.Sells)
		}
	}
	return t
}

func (t *Transactions) Len() int {
	t.lock.RLock()
	l := t.Len()
	t.lock.RUnlock()
	return l
}

// Iter takes a channel to alert to stop reading messages
func (t *Transactions) Iter(productId string, stop <-chan struct{}) (iter chan database.Transaction) {
	iter = make(chan database.Transaction, 10)
	go func(stop <-chan struct{}) {
		t.lock.RLock()
		// Unlock and clean up the channel at the end
		defer func() {
			t.lock.RUnlock()
			close(iter)
		}()

		for _, transaction := range t.transactions[productId] {
			// Write the transaction or stop
			select {
			case iter <- *transaction:
			case <-stop:
				return
			}
		}
	}(stop)
	return iter
}
