package websocket

import (
	"encoding/json"
	"log"

	"github.com/preichenberger/go-coinbasepro/v2"
)

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

// Handle ticker messages
func HandleTicker(msg coinbasepro.Message) {
	log.Println(msg.Price)
}

// Handle level2 messages
func HandleLevel2(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// Handle user only messages from full channel
func HandleUser(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// Handle matches from level2 channel
func HandleMatches(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}

// Handle full channel messages
func HandleFull(msg coinbasepro.Message) {
	out, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}
	log.Println(string(out))
}
