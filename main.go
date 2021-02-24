package main

import (
  "encoding/json"
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

func handleWSMsg(msgChannel chan coinbasepro.Message, handler func(msg coinbasepro.Message)) {
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

// Show data that comes from the ticker
func handleTicker(msg coinbasepro.Message) {
  log.Println(msg.Price)
}

// Handle status messages
func handleStatus(msg coinbasepro.Message) {
  out, err := json.Marshal(msg)
  if err != nil {
    log.Println(err)
  }
  log.Println(string(out))
}

func handleHeartbeat(msg coinbasepro.Message) {
  out, err := json.Marshal(msg)
  if err != nil {
    log.Println(err)
  }
  log.Println(string(out))
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
        case "heartbeat":
          // Spawn a goroutine for status updates on a given coin
          go handleWSMsg(channels[msgChannel], handleHeartbeat)
        case "status":
          // Spawn a goroutine for status updates on a given coin
          go handleWSMsg(channels[msgChannel], handleStatus)
        case "ticker":
          // Spawn a goroutine for ticker updates on a given coin
          go handleWSMsg(channels[msgChannel], handleTicker)
        }
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

  // Authenticate client configuration can be updated with ClientConfig
  client.UpdateConfig(&coinbasepro.ClientConfig{
    BaseURL: dotenv.GetString("COINBASE_PRO_URL"),
    Key: dotenv.GetString("COINBASE_PRO_KEY"),
    Passphrase: dotenv.GetString("COINBASE_PRO_PASSPHRASE"),
    Secret: dotenv.GetString("COINBASE_PRO_SECRET"),
  })

  // accounts, err := client.GetAccounts()
  // if err != nil {
  // 	log.Fatalln(err)
  // }
  wsFeed([]coinbasepro.MessageChannel{
    {
      Name: "status",
      ProductIds: []string{
        "BTC-USD",
      },
    },
    // {
    //   Name: "heartbeat",
    //   ProductIds: []string{
    //     "BTC-USD",
    //   },
    // },
  })
}
