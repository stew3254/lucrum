package config

import (
	"os"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"

	"github.com/pelletier/go-toml"
)

type Coinbase struct {
	URL        string `json:"url"`
	WsURL      string `json:"websocket_url"`
	Key        string `json:"api_key"`
	Secret     string `json:"api_secret"`
	Passphrase string `json:"api_passphrase"`
}

type Websocket struct {
	Channels []coinbasepro.MessageChannel `json:"channels"`
}

type Bot struct {
	IsSandbox bool      `json:"is_sandbox"`
	Coinbase  Coinbase  `json:"coinbase"`
	Sandbox   Coinbase  `json:"Sandbox"`
	Ws        Websocket `json:"websocket"`
}

type Daemon struct {
	Daemonize    bool   `json:"daemonize"`
	PidFile      string `json:"pid_file"`
	PidFilePerms string `json:"pid_file_perms"`
	LogFile      string `json:"log_file"`
	LogFilePerms string `json:"log_file_perms"`
}

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

type Redis struct {
	Name int8   `json:"name"`
	Host string `json:"host"`
	Port int16  `json:"port"`
}

type Config struct {
	Bot      Bot      `json:"bot"`
	Daemon   Daemon   `json:"daemon"`
	WsDaemon Daemon   `json:"websocket_daemon"`
	DB       Database `json:"database"`
	Redis    Redis    `json:"redis"`
	CLI      CommandLine
}

// Parse a Config file and the command line and return the resulting configuration
func Parse() (conf Config, err error) {
	// Grab command line args
	cli := argParse()

	// Read in file
	var file *os.File
	if len(cli.Config) > 0 {
		file, err = os.Open(cli.Config)
	} else {
		file, err = os.Open("config.toml")
	}

	if err != nil {
		return
	}

	// Parse looking for tag json to fix a problem with a coinbase type not decoding correctly
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

	// Override Sandbox behavior
	if cli.Sandbox {
		conf.Bot.IsSandbox = true
	} else if cli.UnSandbox {
		conf.Bot.IsSandbox = false
	}

	return
}
