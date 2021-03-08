package config

import (
	"os"

	"github.com/pelletier/go-toml"
)

type Coinbase struct {
	URL        string `toml:"url"`
	WsURL      string `toml:"websocket_url"`
	Key        string `toml:"api_key"`
	Secret     string `toml:"api_secret"`
	Passphrase string `toml:"api_passphrase"`
}

type Bot struct {
	Coinbase  Coinbase `toml:"coinbase"`
	Sandbox   Coinbase `toml:"sandbox"`
	IsSandbox bool     `toml:"is_sandbox"`
}

type Daemon struct {
	Daemonize    bool   `toml:"daemonize"`
	PidFile      string `toml:"pid_file"`
	PidFilePerms string `toml:"pid_file_perms"`
	LogFile      string `toml:"log_file"`
	LogFilePerms string `toml:"log_file_perms"`
}

type Database struct {
	Type       string `toml:"type"`
	Name       string `toml:"name"`
	Host       string `toml:"host"`
	Port       int16  `toml:"port"`
	User       string `toml:"user"`
	Passphrase string `toml:"passphrase"`
	Attempts   int    `toml:"attempts"`
	Wait       int    `toml:"connection_wait"`
}

type Redis struct {
	Name int8   `toml:"name"`
	Host string `toml:"host"`
	Port int16  `toml:"port"`
}

type Config struct {
	Bot      Bot      `toml:"bot"`
	Daemon   Daemon   `toml:"daemon"`
	WsDaemon Daemon   `toml:"websocket_daemon"`
	DB       Database `toml:"database"`
	Redis    Redis    `toml:"redis"`
}

func Parse(fileName string) (conf Config, err error) {
	conf = Config{}
	contents, err := os.ReadFile(fileName)
	if err != nil {
		return
	}
	err = toml.Unmarshal(contents, &conf)
	return
}
