package config

import (
	"github.com/alecthomas/kong"
)

// CommandLine is used to define flags when calling the program
type CommandLine struct {
	Config        string   `short:"c" help:"Configuration file"`
	Background    bool     `short:"b" help:"Run the bot in the background"`
	BackgroundWs  bool     `help:"Run the websocket handler in the background"`
	DropTables    bool     `short:"d" help:"Wipes the tables in the db to get a fresh start"`
	HistoricRates []string `short:"r" help:"CSV files for rates to read in"`
	Sandbox       bool     `short:"s" help:"Run the box in Sandbox mode" xor:"sandbox"`
	UnSandbox     bool     `short:"u" help:"Run the box without Sandbox mode" xor:"sandbox"`
	WS            bool     `short:"w" help:"Explicitly run only the websocket in the foreground"`
	Verbose       bool     `short:"v" help:"Increase verbosity level"`
}

// Parse the command line arguments
// This should only be called from the main Parse method in this package
func argParse() (cli CommandLine) {
	ctx := kong.Parse(&cli)
	switch ctx.Command() {
	// case "config":
	// 	log.Println("Foo")
	default:
		return
	}
}
