package lib

import (
	"container/list"
	"context"
	"github.com/preichenberger/go-coinbasepro/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"lucrum/config"
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
	t.lock.RLock()
	transactions = make([]*database.Transaction, 0, len(t.transactions[productId]))
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

func (t *Transactions) Len() (l int) {
	defer t.lock.RUnlock()
	t.lock.RLock()
	for _, m := range t.transactions {
		l += len(m)
	}
	return l
}

func (t *Transactions) Clean(productId string) {
	t.lock.Lock()
	t.transactions[productId] = make(map[string]*database.Transaction)
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

	addTransactions := func(ch <-chan *list.Element, stop chan<- struct{}) {
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
		stop <- struct{}{}
		close(stop)
	}

	// Add transactions from the order book
	if books != nil {
		books.Lock.RLock()
		for _, productId := range productIds {
			book := books.Get(productId, false)
			book.Lock.RLock()
			buyStop := make(chan struct{})
			sellStop := make(chan struct{})
			addTransactions(book.BuysIter(buyStop, false), buyStop)
			addTransactions(book.SellsIter(sellStop, false), sellStop)
			book.Lock.RUnlock()
		}
		books.Lock.RUnlock()
	}
	return t
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
		t, exists := open.Get(msg.ProductID, msg.OrderID)
		if !exists {
			log.Println("Not on book", msg.OrderID)
			return
		}
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

// SaveTransactions saves the open and done transactions. Also cleans the done transactions
// This bypasses 'private' constraints and normally should not be done
func SaveTransactions(db *gorm.DB, open, done *Transactions) {
	assignmentCols := []string{"added_to_book", "remaining_size", "closed_at", "reason", "match_id", "order_changed", "new_size"}

	toSlice := func(t *Transactions) []database.Transaction {
		length := 0
		for _, m := range t.transactions {
			length += len(m)
		}
		slice := make([]database.Transaction, 0, length)
		for _, m := range t.transactions {
			for _, v := range m {
				slice = append(slice, *v)
			}
		}
		return slice
	}

	// Convert the transactions to a slice
	open.lock.RLock()
	done.lock.Lock()
	openSlice := toSlice(open)
	doneSlice := toSlice(done)

	// Throw away done transactions since we don't need them anymore
	for productId := range done.transactions {
		done.transactions[productId] = make(map[string]*database.Transaction)
	}
	open.lock.RUnlock()
	done.lock.Unlock()

	// Save the transactions
	db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "order_id"}},
		DoUpdates: clause.AssignmentColumns(assignmentCols),
	}).Create(&openSlice)
	db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "order_id"}},
		DoUpdates: clause.AssignmentColumns(assignmentCols),
	}).Create(&doneSlice)
}

func TransactionsSaver(ctx context.Context, wg, trWg *sync.WaitGroup, open, done *Transactions) {
	cli := ctx.Value(LucrumKey("conf")).(config.Configuration).CLI
	db := ctx.Value(LucrumKey("db")).(*gorm.DB)

	defer func() {
		// Wait for all the l3 message handlers to finish in order to save
		if cli.Verbose {
			log.Println("Transaction saver waiting on L3 consumers to finish")
		}
		trWg.Wait()
		if cli.Verbose {
			log.Println("Saving transactions")
		}
		// Save the transactions one last time and exit
		SaveTransactions(db, open, done)
		if cli.Verbose {
			log.Println("Saved transactions, now closing")
		}
		// Tell the parent we're done
		wg.Done()
	}()

	if cli.Verbose {
		log.Println("Started transaction saver")
	}

	// Every 10 seconds save the transactions
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ticker.C:
			if cli.Verbose {
				log.Println("Saving transactions")
			}
			SaveTransactions(db, open, done)
			if cli.Verbose {
				log.Println("Saved transactions")
			}
		case <-ctx.Done():
			return
		}
	}
}
