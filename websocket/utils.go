package websocket

import (
	"container/list"
	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/stew3254/ratelimit"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"lucrum/database"
	"sync"
	"time"
)

type Book struct {
	Sequence int64
	Buys     *list.List
	Sells    *list.List
}

func getOrderBooks(
	client *coinbasepro.Client,
	rl *ratelimit.RateLimiter,
	productIds []string,
) (snapshot map[string]*Book) {
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
	now := time.Now()
	snapshot = make(map[string]*Book)
	for i := 0; i < len(productIds); i++ {
		// Create the function to create a snapshot
		toSnapshot := func(isAsk bool, entry coinbasepro.BookEntry) database.OrderBookSnapshot {
			return database.OrderBookSnapshot{
				ProductId: productIds[i],
				Sequence:  books[i].Sequence,
				IsAsk:     isAsk,
				Time:      now,
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
		// Add the book to the map
		snapshot[productIds[i]] = book
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
				db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "order_id"}},
					DoNothing: true,
				}).Create(entries)
				return
			}

			entries[i] = e.Value.(database.OrderBookSnapshot)
			e = e.Next()
		}
		// Add entries to the database
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "order_id"}},
			DoNothing: true,
		}).Create(entries)
	}
}
