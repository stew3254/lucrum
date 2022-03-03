package websocket

import (
	"context"
	"log"
	"lucrum/config"
	"sync"

	"gorm.io/gorm"

	ws "github.com/gorilla/websocket"
	"github.com/preichenberger/go-coinbasepro/v2"
)

// Authenticate sends the subscription message to the websocket and authenticates
func Authenticate(conn *ws.Conn, conf config.Coinbase, msgs []coinbasepro.MessageChannel) error {
	// Create unsigned subscription message
	msg := coinbasepro.Message{
		Type:     "subscribe",
		Channels: msgs,
	}

	// Sign the message
	subscribe, err := msg.Sign(conf.Secret, conf.Key, conf.Passphrase)
	if err != nil {
		log.Fatalln("Authentication error:", err)
	}

	// Write our subscription message
	if err := conn.WriteJSON(subscribe); err != nil {
		log.Fatalln(err)
	}
	return nil
}

// WSMessageHandler is a wrapper for simple handlers that only handle a message at a time
func WSMessageHandler(
	ctx context.Context,
	wg *sync.WaitGroup,
	db *gorm.DB,
	msgChannel <-chan coinbasepro.Message,
	handler func(db *gorm.DB, msg coinbasepro.Message),
) {
	defer func() {
		log.Println("Closed message handler")
		wg.Done()
	}()

	// Forever look for updates
	var lastSequence int64

	// Ignore the initial message sent saying we've started listening or close since interrupted
	select {
	case _, ok := <-msgChannel:
		if !ok {
			return
		}
	case <-ctx.Done():
		return
	}

	for {
		select {
		case msg := <-msgChannel:
			// Is this a mistake to assume it can't be 0?
			// This fixes the bug with level2 channels not using sequences
			if msg.Sequence == 0 || msg.Sequence > lastSequence {
				handler(db, msg)
			}
		case <-ctx.Done():
			return
		}
	}
}

// Read messages out of the websocket
func readMsgs(
	ctx context.Context,
	wg *sync.WaitGroup,
	wsConn *ws.Conn,
	msgChan chan<- coinbasepro.Message,
	errChan chan<- error,
) {
	defer func() {
		// Clean up and alert the parent we're done
		_ = wsConn.Close()
		close(msgChan)
		close(errChan)
		log.Println("Closed websocket reader")
		wg.Done()
	}()
	once := sync.Once{}
	for {
		// Get the message
		msg := coinbasepro.Message{}
		// _, b, err := wsConn.ReadMessage()
		// log.Println(string(b))
		if err := wsConn.ReadJSON(&msg); err != nil {
			select {
			// This was closed normally, it's not a real error
			case <-ctx.Done():
				return
			// Send the error back
			case errChan <- err:
				return
			}
		}

		// Only when the first message is read send the start message,
		// so handlers know that they can read
		once.Do(func() { msgChan <- coinbasepro.Message{Type: "lucrum_start"} })

		// Send the message
		select {
		case msgChan <- msg:
		case <-ctx.Done():
			return
		}
	}
}

// WSDispatcher takes in the channels, sets up the websocket connection and creates
// the appropriate message handlers to handle future messages and will manage which ones
// it sends its messages to
func WSDispatcher(
	ctx context.Context,
	wg *sync.WaitGroup,
	conf config.Config,
	db *gorm.DB,
	msgChannels []coinbasepro.MessageChannel,
) {
	// Tell the parent we're done
	defer func() {
		log.Println("Closed Websocket Dispatcher")
		wg.Done()
	}()

	// Get bot configuration
	var botConf config.Bot
	if conf.Conf.IsSandbox {
		botConf = conf.Conf.Sandbox
	} else {
		botConf = conf.Conf.Production
	}

	// This is used, so we can receive user specific messages
	// Due to how coinbase set up their websocket, it's not possible to tell
	// with just one connection, but with at most 2
	seenUser := false
	// This way I don't keep accidentally spawning more and more websocket connections
	seenFull := false

	// First filter our duplicate msgChannels
	channels := make(map[string]chan coinbasepro.Message)
	for _, channel := range msgChannels {
		// Just see if the channel is in the map
		if _, ok := channels[channel.Name]; !ok {
			// Add to the wait group
			wg.Add(1)
			// Create a new buffered channel.
			// We can hold up to 50 messages which is like a few seconds worth for most channels
			channels[channel.Name] = make(chan coinbasepro.Message, 50)
			// Figure out which function to spawn with corresponding channel
			switch channel.Name {
			case "heartbeat":
				go WSMessageHandler(ctx, wg, db, channels[channel.Name], HandleHeartbeat)
			case "status":
				go WSMessageHandler(ctx, wg, db, channels[channel.Name], HandleStatus)
			case "ticker":
				go WSMessageHandler(ctx, wg, db, channels[channel.Name], HandleTicker)
			case "level2":
				go L2Handler(ctx, wg, db, channels[channel.Name])
			case "user":
				// In case multiple have been passed in, ignore them
				if !seenUser {
					seenUser = true
					go UserHandler(ctx, wg, db, channels[channel.Name])
				}
			case "matches":
				go WSMessageHandler(ctx, wg, db, channels[channel.Name], HandleMatches)
			case "full":
				// Since full channel and user channel look the same,
				// this is the only decent method of confirming they are different
				if seenUser && !seenFull {
					seenFull = true
					// Delete the old channel since it is no longer needed
					delete(channels, channel.Name)
					// Create a new dispatcher to explicitly handle this
					newMsgChannels := []coinbasepro.MessageChannel{channel}
					// TODO Figure out clean way to make these communicate with each other
					go WSDispatcher(ctx, wg, conf, db, newMsgChannels)
				} else if !seenUser {
					// Normal usage here
					go L3Handler(
						ctx,
						wg,
						botConf,
						conf.Conf.Ws,
						db,
						channels[channel.Name],
						channel.ProductIds,
					)
				}
			}
		}
	}

	// Create a websocket to coinbase
	var wsDialer ws.Dialer
	conn, _, err := wsDialer.Dial(
		botConf.Coinbase.WsURL,
		nil,
	)

	// If the websocket fails the bot can't function
	if err != nil {
		log.Fatalln(err)
	}

	// Try to authenticate to the websocket
	if err = Authenticate(conn, botConf.Coinbase, msgChannels); err != nil {
		log.Fatalln("Authentication failed:", err)
	}

	// Create a channel to send the messages over
	msgChan := make(chan coinbasepro.Message, 50)
	// Create a channel to receive an error over
	errChan := make(chan error, 1)

	// Start the message reader
	wg.Add(1)
	go readMsgs(ctx, wg, conn, msgChan, errChan)

	// Send the message but check to make sure we haven't received an interrupt yet
	sendMsg := func(ch chan<- coinbasepro.Message, ok bool, msg coinbasepro.Message) {
		if !ok {
			return
		}
		select {
		case ch <- msg:
		case <-ctx.Done():
			return
		}
	}

	// Constantly wait for messages
	// Then send them to appropriate msgChannels to handle them
	for true {
		select {
		// We received a message
		case msg, ok := <-msgChan:
			if !ok {
				return
			}

			// Find the corresponding Go channel in the map to send the message to
			msgType := msg.Type
			switch msg.Type {
			case "lucrum_start":
				// Tell the channels that they can start listening
				msgType = "lucrum_start"
			case "error":
				log.Println(msg.Message, msg.Reason)
			case "snapshot":
				// Correct the type
				msgType = "level2"
			case "l2update":
				// Correct the type
				msgType = "level2"
			case "last_match":
				// Correct the type
				msgType = "matches"
			case "match":
				// Since there is an overload here, do a manual check later
				msgType = "match"
			case "received":
				// Correct the type
				if seenUser {
					msgType = "user"
				} else {
					msgType = "full"
				}
			case "open":
				// Correct the type
				if seenUser {
					msgType = "user"
				} else {
					msgType = "full"
				}
			case "done":
				// Correct the type
				if seenUser {
					msgType = "user"
				} else {
					msgType = "full"
				}
			case "change":
				// Correct the type
				if seenUser {
					msgType = "user"
				} else {
					msgType = "full"
				}
			case "activate":
				// Correct the type
				if seenUser {
					msgType = "user"
				} else {
					msgType = "full"
				}
			}

			if msgType == "match" {
				// In the overloaded case where matches need to go to 2 places, handle this explicitly
				ch, ok := channels["full"]
				sendMsg(ch, ok, msg)
				ch, ok = channels["matches"]
				sendMsg(ch, ok, msg)
			} else if msgType == "lucrum_start" {
				// Alert all channels we should start listening
				for _, ch := range channels {
					sendMsg(ch, true, msg)
				}
			} else {
				// If the channel exists, send the message over to the handler
				ch, ok := channels[msgType]
				sendMsg(ch, ok, msg)
			}

		// Something bad happened and it's time to die
		case err = <-errChan:
			log.Println("Websocket error:", err)
			return
		// We received an interrupt
		case <-ctx.Done():
			return
		}
	}
}
