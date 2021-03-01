package websocket

import (
	"context"
	"log"
	"lucrum/config"

	ws "github.com/gorilla/websocket"
	"github.com/preichenberger/go-coinbasepro/v2"
)

func WSMessageHandler(msgChannel chan coinbasepro.Message, handler func(msg coinbasepro.Message)) {
	// Forever look for updates
	var lastSequence int64
	for {
		select {
		case msg := <-msgChannel:
			if msg.Sequence > lastSequence {
				handler(msg)
			}
		}
	}
}

func WSDispatcher(ctx context.Context, conf config.Coinbase, msgChannels []coinbasepro.MessageChannel) {
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
			}
		}
	}

	// Create a websocket to coinbase
	var wsDialer ws.Dialer
	wsConn, _, err := wsDialer.Dial(
		conf.WsURL,
		nil,
	)

	// If the websocket fails the bot can't function
	if err != nil {
		log.Fatalln(err)
	}

	// Subscribe with our msgChannels
	subscribe := coinbasepro.Message{
		Type:     "subscribe",
		Channels: msgChannels,
	}

	// Write our subscription message
	if err := wsConn.WriteJSON(subscribe); err != nil {
		log.Fatalln(err)
	}

	// Create a channel to send the messages over
	msgChan := make(chan coinbasepro.Message, 10)
	// Create a channel to receive an error over
	errChan := make(chan error, 1)

	// Read messages out of the websocket
	readMessages := func(msgChan chan coinbasepro.Message, errChan chan error) {
		for {
			// Get the message
			msg := coinbasepro.Message{}
			if err := wsConn.ReadJSON(&msg); err != nil {
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
			if channel, ok := channels[msg.Type]; ok {
				channel <- msg
			}
		// Something bad happened and it's time to die
		case err := <-errChan:
			log.Println(err)
			return
		// We received an interrupt
		case <-ctx.Done():
			log.Println("Received an interrupt. Shutting down gracefully")
			// Close the connection so the reader sending in the messages errors and dies
			_ = wsConn.Close()
			return
		}
	}
}
