package websocket

import (
	ws "github.com/gorilla/websocket"
	"github.com/preichenberger/go-coinbasepro/v2"
	"github.com/profclems/go-dotenv"
	"log"
)

func WSMessageHandler(msgChannel chan coinbasepro.Message, handler func(msg coinbasepro.Message)) {
	// Forever look for ticker updates
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

func WSDispatcher(msgChannels []coinbasepro.MessageChannel) {
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
		dotenv.GetString("COINBASE_PRO_WS_SANDBOX"),
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
		log.Println(err)
	}

	// Read messages out of the websocket
	// Then send them to appropriate msgChannels to handle them
	for true {
		msg := coinbasepro.Message{}
		if err := wsConn.ReadJSON(&msg); err != nil {
			log.Println(err)
			break
		}
		// Find the corresponding Go channel in the map to send the message to
		if channel, ok := channels[msg.Type]; ok {
			channel <- msg
		}
	}
}
