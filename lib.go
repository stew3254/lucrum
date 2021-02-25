package main

import "log"

// Check simple fails if the error is not nil
func Check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// CheckErr simple fails if the error is not nil but with the message provided
func CheckErr(err error, msg string) {
	if err != nil {
		log.Fatal(msg)
	}
}
