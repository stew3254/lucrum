package websocket

import (
	"context"
	"log"
	"lucrum/config"
	"os"

	ws "github.com/gorilla/websocket"
	"github.com/preichenberger/go-coinbasepro/v2"
)

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

func WSDispatcher(ctx context.Context, conf config.Config) {
	// First filter our duplicate msgChannels
	channels := make(map[string]chan coinbasepro.Message, len(conf.Bot.Ws.Channels))
	for _, channel := range conf.Bot.Ws.Channels {
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
				go WSMessageHandler(channels[channel.Name], HandleUser)
			case "matches":
				go WSMessageHandler(channels[channel.Name], HandleMatches)
			case "full":
				go WSMessageHandler(channels[channel.Name], HandleFull)
			}
		}
	}

	// Create a websocket to coinbase
	var wsDialer ws.Dialer
	var wsConn *ws.Conn
	var err error
	if conf.Bot.IsSandbox {
		wsConn, _, err = wsDialer.Dial(
			conf.Bot.Sandbox.WsURL,
			nil,
		)
	} else {
		wsConn, _, err = wsDialer.Dial(
			conf.Bot.Coinbase.WsURL,
			nil,
		)
	}

	// If the websocket fails the bot can't function
	if err != nil {
		log.Fatalln(err)
	}

	// Subscribe with our msgChannels
	subscribe := coinbasepro.Message{
		Type:     "subscribe",
		Channels: conf.Bot.Ws.Channels,
	}

	// Write our subscription message
	if err := wsConn.WriteJSON(subscribe); err != nil {
		log.Fatalln(err)
	}

	// Create a channel to send the messages over
	msgChan := make(chan coinbasepro.Message, 10)
	// Create a channel to receive an error over
	errChan := make(chan error, 1)
	// Create a channel to signal that we are done
	done := make(chan struct{}, 1)

	// Read messages out of the websocket
	readMessages := func(msgChan chan coinbasepro.Message, errChan chan error) {
		for {
			// Get the message
			msg := coinbasepro.Message{}
			// _, b, err := wsConn.ReadMessage()
			// log.Println(string(b))
			if err = wsConn.ReadJSON(&msg); err != nil {
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

	// Start the message reader
	go readMessages(msgChan, errChan)

	// Constantly wait for messages
	// Then send them to appropriate msgChannels to handle them
	for true {
		select {
		// We received a message
		case msg := <-msgChan:
			// Find the corresponding Go channel in the map to send the message to
			msgType := msg.Type
			switch msg.Type {
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
				msgType = "full"
			case "open":
				// Correct the type
				msgType = "full"
			case "done":
				// Correct the type
				msgType = "full"
			case "change":
				// Correct the type
				msgType = "full"
			case "activate":
				// Correct the type
				msgType = "full"
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
			_ = wsConn.Close()
			os.Exit(1)
		}
	}
}
