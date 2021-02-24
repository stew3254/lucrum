package main

import (
  ws "github.com/gorilla/websocket"
  "github.com/preichenberger/go-coinbasepro/v2"
  "github.com/profclems/go-dotenv"
  "log"
)

// Used so we can hash it
type MessageChannel struct {
  Name      string
  ProductId string
}

func tickerUpdates(msgChannel chan coinbasepro.Message) {
  // Forever look for ticker updates
  var lastSequence int64
  var price string
  for {
    select {
    case msg := <-msgChannel:
      if msg.Sequence > lastSequence {
        lastSequence = msg.Sequence
        price = msg.Price
        log.Println(lastSequence, msg.BestAsk, price)
      }
    }
  }
}

func wsFeed(msgChannels []coinbasepro.MessageChannel) {
  // First filter our duplicate msgChannels
  channels := make(map[MessageChannel]chan coinbasepro.Message, len(msgChannels))
  for _, channel := range msgChannels {
    // Create a new MessageChannel for a single product
    for _, id := range channel.ProductIds {
      msgChannel := MessageChannel{
        Name:      channel.Name,
        ProductId: id,
      }
      // Just see if the channel is in the map
      if _, ok := channels[msgChannel]; !ok {
        // Create a new buffered channel. 
        // We can hold up to 50 messages which is like a few seconds worth
        // Probably will never reach this limit. This is good so we never miss messages
        channels[msgChannel] = make(chan coinbasepro.Message, 50)
        // Figure out which function to spawn with corresponding channel
        switch channel.Name {
        case "ticker":
          // Spawn a goroutine for ticker updates on a given coin
          go tickerUpdates(channels[msgChannel])
        }
      }
    }
  }
  
  // Create a websocket to coinbase
  var wsDialer ws.Dialer
  wsConn, _, err := wsDialer.Dial("wss://ws-feed.pro.coinbase.com", nil)
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
    // Construct a message channel out of the message
    msgChannel := MessageChannel{
      Name:      msg.Type,
      ProductId: msg.ProductID,
    }
    // Find the corresponding Go channel in the map to send the message to
    if channel, ok := channels[msgChannel]; ok {
      channel <- msg
    }
  }
}

func main() {
  // Load in the dotenv config
  err := dotenv.LoadConfig()
  if err != nil {
    log.Fatalln("Error loading in .env file")
  }

  client := coinbasepro.NewClient()

  // optional, configuration can be updated with ClientConfig
  client.UpdateConfig(&coinbasepro.ClientConfig{
    BaseURL: "https://api.pro.coinbase.com",
    Key: dotenv.GetString("API_KEY"),
    Passphrase: dotenv.GetString("API_PASSPHRASE"),
    Secret: dotenv.GetString("API_SECRET"),
  })

  // accounts, err := client.GetAccounts()
  // if err != nil {
  // 	log.Fatalln(err)
  // }
  wsFeed([]coinbasepro.MessageChannel{
    {
      Name: "ticker",
      ProductIds: []string{
        "BTC-USD",
      },
    },
  })
}
