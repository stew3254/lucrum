package websocket

import (
	"encoding/json"
	"github.com/preichenberger/go-coinbasepro/v2"
	"log"
)

// Show data that comes from the ticker
func HandleTicker(msg coinbasepro.Message) {
	log.Println(msg.Price)
}

// Handle status messages
func HandleStatus(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// Handle heartbeat messages
func HandleHeartbeat(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}
