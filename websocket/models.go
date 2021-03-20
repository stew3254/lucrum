package websocket

import (
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"
)

type AuthenticationMessage struct {
	Type       string                       `json:"type"`
	Channels   []coinbasepro.MessageChannel `json:"channels"`
	B64Key     []byte                       `json:"type"`
	Passphrase string                       `json:"type"`
	Signature  string                       `json:"type"`
	Timestamp  time.Time                    `json:"type"`
}
