package lib

import (
	"bufio"
	"container/list"
	"github.com/sevlyar/go-daemon"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"lucrum/database"
	"os"
	"strings"
	"sync"
)

// Books is the global order book used throughout the program for lookup
var Books *OrderBook

// Check simply fails if the error is not nil
func Check(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

// CheckErr simple fails if the error is not nil but with the message provided
func CheckErr(err error, msg string) {
	if err != nil {
		log.Fatalln(msg)
	}
}

// AlertUser gets a confirmation from the user, so they know they aren't in a sandbox
func AlertUser() (err error) {
	// Check to see if we're already daemonized or not
	if daemon.WasReborn() {
		return nil
	}

	// Alert user they are not in a sandbox
	log.Println("YOU ARE NOT IN A SANDBOX! ARE YOU SURE YOU WANT TO CONTINUE? (y/N)")

	// Try to read stdin
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')

	// Return the error
	if err != nil {
		return
	}

	// Get user input
	text = strings.Trim(strings.ToLower(text), "\n")
	if text != "y" && text != "yes" {
		log.Println("Okay, shutting down")
		log.Println("To run in sandbox mode, " +
			"set 'is_sandbox = true' in the config file or supply -s as a command line argument")
		os.Exit(0)
	}
	return
}

func MedianDec(slice []decimal.Decimal) string {
	if len(slice) == 0 {
		return "0"
	} else {
		if len(slice)&1 == 1 {
			return slice[(len(slice))/2].String()
		} else {
			return decimal.Avg(slice[(len(slice)-1)/2], slice[(len(slice))/2]).String()
		}
	}
}

func MedianInt64(slice []int64) int64 {
	if len(slice) == 0 {
		return 0
	} else {
		if len(slice)&1 == 1 {
			return slice[(len(slice))/2]
		} else {
			return AverageInt64([]int64{slice[(len(slice)-1)/2], slice[(len(slice))/2]})
		}
	}
}

func AverageDec(d []decimal.Decimal) string {
	if len(d) == 0 {
		return "0"
	} else if len(d) == 1 {
		return d[0].String()
	} else {
		return decimal.Avg(d[0], d[1:]...).String()
	}
}

func AverageInt64(s []int64) int64 {
	var avg int64 = 0
	for i := 0; i < len(s)-1; i++ {
		avg += s[i+1] - s[i]
	}
	if avg != 0 {
		avg /= int64(len(s))
	}
	return avg
}

func StringToDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		d = decimal.NewFromInt(0)
	}
	return d
}

func IterHelper(
	l *list.List,
	locker sync.Locker,
	shouldLock bool,
	stop <-chan struct{},
) (iter chan *list.Element) {
	iter = make(chan *list.Element, 10)
	go func(
		l *list.List,
		locker sync.Locker,
		shouldLock bool,
		iter chan *list.Element,
		stop <-chan struct{},
	) {
		// Handle clean up
		once := sync.Once{}
		f := func() {
			close(iter)
			if shouldLock {
				locker.Unlock()
			}
		}
		defer once.Do(f)

		// Don't lock if locked externally
		if shouldLock {
			// Unlock and clean up the channel at the end
			locker.Lock()
		}

		for e := l.Front(); e != nil; e = e.Next() {
			// Write the transaction or stop
			select {
			case iter <- e:
			case <-stop:
				return
			}
		}
		// Close the iter channel since we've finished iterating and wait for the stop message
		once.Do(f)
		<-stop
	}(l, locker, shouldLock, iter, stop)
	return iter
}

// ObListToDB gradually converts an order book list to a slice and adds it to the database
func ObListToDB(db *gorm.DB, ch <-chan *list.Element, stop chan<- struct{}, size int) {
	// Just tell the channel we're done
	defer func() {
		stop <- struct{}{}
		close(stop)
	}()

	// Convert the list to a slice
	entries := make([]database.OrderBookSnapshot, size)
	for {
		for i := 0; i < size; i++ {
			elem, ok := <-ch
			// There are no entries left so add remainders and break out of the loop
			if !ok {
				// db.Create(entries[:i])
				db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "order_id"}},
					DoUpdates: clause.AssignmentColumns([]string{"last_sequence", "price", "size"}),
				}).Create(entries[:i])
				return
			}

			entries[i] = elem.Value.(database.OrderBookSnapshot)
		}
		// Add entries to the database
		// db.Create(entries)
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "order_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_sequence"}),
		}).Create(entries)
	}
}
