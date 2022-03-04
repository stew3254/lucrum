package lib

import (
	"container/list"
	"github.com/preichenberger/go-coinbasepro/v2"
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
			book := Books.Get(productId, true)
			buyStop := make(chan struct{})
			sellStop := make(chan struct{})
			addTransactions := func(ch <-chan *list.Element, stop chan<- struct{}) {
				defer func() {
					stop <- struct{}{}
					close(stop)
				}()
				for elem := range ch {
					v := elem.Value.(database.OrderBookSnapshot)
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
			addTransactions(book.BuysIter(buyStop, true), buyStop)
			addTransactions(book.SellsIter(sellStop, true), sellStop)
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
