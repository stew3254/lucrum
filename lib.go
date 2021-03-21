package main

import (
	"bufio"
	"log"
	"os"
	"strings"

	"github.com/sevlyar/go-daemon"
)

// Check simply fails if the error is not nil
func Check(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

// CheckErr simple fails if the error is not nil but with the message provided
func CheckErr(err error, msg string) {
	if err != nil {
		log.Fatalln(msg)
	}
}

// AlertUser gets a confirmation from the user so they know they aren't in a sandbox
func AlertUser() (err error) {
	// Check to see if we're already daemonized or not
	if daemon.WasReborn() {
		return nil
	}

	// Alert user they are not in a sandbox
	log.Println("YOU ARE NOT IN A SANDBOX! ARE YOU SURE YOU WANT TO CONTINUE? (Y/n)")

	// Try to read stdin
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')

	// Return the error
	if err != nil {
		return
	}

	// Get user input
	text = strings.Trim(strings.ToLower(text), "\n")
	if text != "y" && text != "yes" {
		log.Println("Okay, shutting down")
		log.Println("To run in sandbox mode, " +
			"set 'is_sandbox = true' in the config file or supply -s as a command line argument")
		os.Exit(0)
	}
	return
}
