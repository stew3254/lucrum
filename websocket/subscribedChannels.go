package websocket

import (
	"context"
	"github.com/preichenberger/go-coinbasepro/v2"
	"log"
	"lucrum/config"
	"lucrum/lib"
	"sync"
)

// SubChans is the global list of subscribed channels which the MsgReader reads from
// and subscribers can get messages from
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

func (c *SubscribedChannels) Send(ctx context.Context, productId string, msg coinbasepro.Message) {
	defer c.locks[productId].RUnlock()
	c.locks[productId].RLock()
	for _, ch := range c.channels[productId] {
		select {
		case ch <- msg:
		case <-ctx.Done():
			return
		}
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

func MsgReader(
	ctx context.Context,
	wg *sync.WaitGroup,
	msgChan chan coinbasepro.Message,
	lastSequence map[string]int64,
) {
	cli := ctx.Value(lib.LucrumKey("conf")).(config.Configuration).CLI
	defer func() {
		if cli.Verbose {
			log.Println("Closed subscribed message broadcaster")
		}
		wg.Done()
	}()

	if cli.Verbose {
		log.Println("Started subscribed message broadcaster")
	}

	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				return
			}
			// Ignore old messages and only send new ones
			if msg.Sequence > lastSequence[msg.ProductID] {
				SubChans.Send(ctx, msg.ProductID, msg)
			}
		case <-ctx.Done():
			return
		}
	}
}
