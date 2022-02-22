package config

import (
	"os"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"

	"github.com/pelletier/go-toml"
)

// Coinbase contains all the useful coinbase information
type Coinbase struct {
	URL        string `json:"url"`
	WsURL      string `json:"websocket_url"`
	Key        string `json:"api_key"`
	Secret     string `json:"api_secret"`
	Passphrase string `json:"api_passphrase"`
}

// Websocket contains the list of channels to subscribe with and what info to store
type Websocket struct {
	Channels          []coinbasepro.MessageChannel `json:"channels"`
	Granularity       int64                        `json:"granularity"`
	RawMessages       bool                         `json:"raw_messages"`
	StoreTransactions bool                         `json:"store_transactions"`
}

// Bot has the bot specific (production/sandbox) configurations
type Bot struct {
	Coinbase Coinbase `json:"coinbase"`
	DB       Database `json:"database"`
}

// ConfType holds the information whether the bot is in sandbox mode or not
// Note: this could probably be named better
type ConfType struct {
	IsSandbox  bool      `json:"is_sandbox"`
	Production Bot       `json:"production"`
	Sandbox    Bot       `json:"sandbox"`
	Ws         Websocket `json:"websocket"`
}

// Daemon holds information necessary for daemonization
type Daemon struct {
	Daemonize    bool   `json:"daemonize"`
	PidFile      string `json:"pid_file"`
	PidFilePerms string `json:"pid_file_perms"`
	LogFile      string `json:"log_file"`
	LogFilePerms string `json:"log_file_perms"`
}

// Database holds all the potentially useful DB information we could need
type Database struct {
	Type       string        `json:"type"`
	Name       string        `json:"name"`
	Host       string        `json:"host"`
	Port       int16         `json:"port"`
	User       string        `json:"user"`
	Passphrase string        `json:"passphrase"`
	Attempts   int           `json:"attempts"`
	Wait       time.Duration `json:"connection_wait"`
}

// Config is the top level structure for the configuration file
// CLI contains the parsed command line flags. This should only be used
// For things that do not override normal configurations (such as verbosity level)
type Config struct {
	Conf     ConfType `json:"conf"`
	Daemon   Daemon   `json:"daemon"`
	WsDaemon Daemon   `json:"websocket_daemon"`
	CLI      CommandLine
}

// Parse a Config file and the command line and return the resulting configuration
func Parse() (conf Config, err error) {
	// Grab command line args
	cli := argParse()

	// Read in config file
	var file *os.File
	if len(cli.Config) > 0 {
		file, err = os.Open(cli.Config)
	} else {
		file, err = os.Open("config.toml")
	}

	// If something is wrong, return it
	if err != nil {
		return
	}

	// Parse is looking for tag json instead of toml
	// to fix a problem with a coinbase type not decoding correctly
	decoder := toml.NewDecoder(file)
	decoder.SetTagName("json")

	// Convert toml to the Config struct
	err = decoder.Decode(&conf)
	// Add these to the main Config for things that aren't overridden
	// like historical data or verbosity
	conf.CLI = cli

	// Override backgrounding
	if cli.Background {
		conf.Daemon.Daemonize = true
	}
	if cli.BackgroundWs {
		conf.WsDaemon.Daemonize = true
	}

	// Override sandbox behavior
	if cli.Sandbox {
		conf.Conf.IsSandbox = true
	} else if cli.UnSandbox {
		conf.Conf.IsSandbox = false
	}

	return
}
