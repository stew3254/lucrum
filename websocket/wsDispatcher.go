package websocket

import (
	"context"
	"log"
	"lucrum/config"
	"os"

	ws "github.com/gorilla/websocket"
	"github.com/preichenberger/go-coinbasepro/v2"
)

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

// WsMessageHandler takes care of calling the function that handles a message
func WSMessageHandler(msgChannel chan coinbasepro.Message, handler func(msg coinbasepro.Message)) {
	// Forever look for updates
	var lastSequence int64
	for {
		select {
		case msg := <-msgChannel:
			// Is this a mistake to assume it can't be 0?
			// This fixes the bug with level2 channels not using sequences
			if msg.Sequence == 0 || msg.Sequence > lastSequence {
				handler(msg)
			}
		}
	}
}

// Read messages out of the websocket
func readMessages(
	wsConn *ws.Conn,
	msgChan chan coinbasepro.Message,
	errChan chan error,
	done chan struct{},
) {
	for {
		// Get the message
		msg := coinbasepro.Message{}
		// _, b, err := wsConn.ReadMessage()
		// log.Println(string(b))
		if err := wsConn.ReadJSON(&msg); err != nil {
			select {
			// This was closed normally, it's not a real error
			case <-done:
				return
			// This was not closed normally, it's a real error
			default:
			}
			log.Println(err)
			// Send the error back
			errChan <- err
			return
		}

		// Send the message
		msgChan <- msg
	}
}

func WSDispatcher(
	ctx context.Context,
	conf config.Config,
	msgChannels []coinbasepro.MessageChannel,
) {
	// This is used so we can receive user specific messages
	// Due to how coinbase set up their websocket, it's not possible to tell
	// with just one connection
	seenUser := false
	seenFull := false

	// First filter our duplicate msgChannels
	channels := make(map[string]chan coinbasepro.Message, len(msgChannels))
	for _, channel := range msgChannels {
		// Just see if the channel is in the map
		if _, ok := channels[channel.Name]; !ok {
			// Create a new buffered channel.
			// We can hold up to 50 messages which is like a few seconds worth
			// Probably will never reach this limit. This is good so we never miss messages
			channels[channel.Name] = make(chan coinbasepro.Message, 50)
			// Figure out which function to spawn with corresponding channel
			switch channel.Name {
			case "heartbeat":
				go WSMessageHandler(channels[channel.Name], HandleHeartbeat)
			case "status":
				go WSMessageHandler(channels[channel.Name], HandleStatus)
			case "ticker":
				go WSMessageHandler(channels[channel.Name], HandleTicker)
			case "level2":
				go WSMessageHandler(channels[channel.Name], HandleLevel2)
			case "user":
				// In case multiple have been passed in, ignore them
				if !seenUser {
					seenUser = true
					go WSMessageHandler(channels[channel.Name], HandleUser)
				}
			case "matches":
				go WSMessageHandler(channels[channel.Name], HandleMatches)
			case "full":
				// Since full channel and user channel look the same,
				// this is the only decent method of confirming they are different
				// TODO FINISH THIS
				if seenUser && !seenFull {
					seenFull = true
					// Delete the old channel since it is no longer needed
					delete(channels, channel.Name)
					// Create a new dispatcher to explicitly handle this
					newMsgChannels := []coinbasepro.MessageChannel{channel}
					go WSDispatcher(ctx, conf, newMsgChannels)
				} else if !seenUser {
					// Normal usage here
					go WSMessageHandler(channels[channel.Name], HandleFull)
				}
			}
		}
	}

	// Get bot configuration
	var botConf config.Bot
	if conf.Conf.IsSandbox {
		botConf = conf.Conf.Sandbox
	} else {
		botConf = conf.Conf.Production
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
	msgChan := make(chan coinbasepro.Message, 10)
	// Create a channel to receive an error over
	errChan := make(chan error, 1)
	// Create a channel to signal that we are done
	done := make(chan struct{}, 1)

	// Start the message reader
	go readMessages(conn, msgChan, errChan, done)

	// Constantly wait for messages
	// Then send them to appropriate msgChannels to handle them
	for true {
		select {
		// We received a message
		case msg := <-msgChan:
			// Find the corresponding Go channel in the map to send the message to
			msgType := msg.Type
			switch msg.Type {
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
				// Correct the type (This means the full handler will no longer get match messages)
				msgType = "matches"
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

			if channel, ok := channels[msgType]; ok {
				channel <- msg
			}
		// Something bad happened and it's time to die
		case err := <-errChan:
			log.Println(err)
			return
		// We received an interrupt
		case <-ctx.Done():
			log.Println("Received an interrupt. Shutting down gracefully")
			// Send signal to the message reader that we are done
			done <- struct{}{}
			// Close the connection so the reader sending in the messages errors and dies
			_ = conn.Close()
			os.Exit(1)
		}
	}
}
