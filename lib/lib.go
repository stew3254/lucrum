package lib

import (
	"bufio"
	"github.com/sevlyar/go-daemon"
	"github.com/shopspring/decimal"
	"log"
	"os"
	"strings"
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
	log.Println("YOU ARE NOT IN A SANDBOX! ARE YOU SURE YOU WANT TO CONTINUE? (y/N)")

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

func Median(slice []decimal.Decimal) string {
	if len(slice) == 0 {
		return "0"
	} else {
		if len(slice)&1 == 1 {
			return slice[(len(slice))/2].String()
		} else {
			return decimal.Avg(slice[(len(slice)-1)/2], slice[(len(slice))/2]).String()
		}
	}
}

func Average(d []decimal.Decimal) string {
	if len(d) == 0 {
		return "0"
	} else if len(d) == 1 {
		return d[0].String()
	} else {
		return decimal.Avg(d[0], d[1:]...).String()
	}
}

func StringToDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		d = decimal.NewFromInt(0)
	}
	return d
}
