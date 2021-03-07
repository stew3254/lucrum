package config

import (
	"github.com/alecthomas/kong"
)

var CLI struct {
	Config        string   `short:"c" help:"Configuration file"`
	Background    bool     `short:"b" help:"Run the bot in the background"`
	BackgroundWS  bool     `help:"Run the websocket handler in the background"`
	HistoricRates []string `short:"r" help:"CSV files for rates to read in"`
	Sandbox       bool     `short:"s" help:"Run the box in sandbox mode" xor:"sandbox"`
	UnSandbox     bool     `short:"u" help:"Run the box without sandbox mode" xor:"sandbox"`
	Verbose       bool     `short:"v" help:"Increase verbosity level"`
}

func ArgParse() {
	ctx := kong.Parse(&CLI)
	switch ctx.Command() {
	// case "config":
	// 	log.Println("Foo")
	default:
		return
	}
}
