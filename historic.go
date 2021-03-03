package main

import (
	"encoding/csv"
	"errors"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/preichenberger/go-coinbasepro/v2"
)

const MaxHistoricRates int = 300

func GetHistoricRatesGranularities() []int {
	return []int{60, 300, 900, 3600, 21600, 86400}
}

// GetHistoricRates grabs the data for a coin
func GetHistoricRates(
	client *coinbasepro.Client,
	product string,
	params coinbasepro.GetHistoricRatesParams,
) (rates []coinbasepro.HistoricRate, err error) {
	// Check if granularity is valid or not
	validGranularity := false
	for _, granularity := range GetHistoricRatesGranularities() {
		if params.Granularity == granularity {
			validGranularity = true
			break
		}
	}

	// Complain if we don't have a good granularity
	if !validGranularity {
		return nil, errors.New("invalid granularity provided")
	}

	log.Println(params.Start.Add(time.Duration(params.Granularity) * time.Second))
	log.Println(time.Duration(params.Granularity) * time.Second)
	rates, err = client.GetHistoricRates(product, params)
	if err != nil {
		return
	}

	return
}

// ReadRateFile reads in a specific CSV file format to get historical data
// This format contains headers in the order "Product,Start,End,Granularity"
// The coin must be of format COIN/CURRENCY
// The times must be in the format of RFC3339
// End time is allowed to be "now" in order to get time.Now()
func ReadRateFile(fileName string) ([]string, []coinbasepro.GetHistoricRatesParams, error) {
	var products []string
	var params []coinbasepro.GetHistoricRatesParams

	// Open the file
	file, err := os.Open(fileName)
	if err != nil {
		return nil, nil, err
	}
	reader := csv.NewReader(file)

	// Read in headers
	// Header order is "Product,Start,End,Granularity"
	_, err = reader.Read()
	if err != nil {
		return nil, nil, err
	}

	// Read in the rest of the file
	for {
		// Read in a record and break on EOF
		record, err := reader.Read()
		if err != nil && err != io.EOF {
			log.Fatalln(err)
		} else if err == io.EOF {
			break
		}

		// Get Start time
		start, err := time.Parse(time.RFC3339, record[1])
		if err != nil {
			log.Println("Error for Start:", err)
			continue
		}

		var end time.Time

		// We can get the time as now
		if strings.ToLower(record[2]) == "now" {
			end = time.Now()
		} else {
			// Get End time
			end, err = time.Parse(time.RFC3339, record[2])
			if err != nil && strings.ToLower(record[2]) != "now" {
				log.Println("Error for End:", err)
				continue
			}
		}

		// Get Granularity. Don't check it here since it's checked later
		granularity, err := strconv.Atoi(record[3])
		if err != nil {
			log.Println("Error for Granularity:", err)
			continue
		}

		// Add the product
		products = append(products, record[0])

		// Build our array to return
		params = append(
			params,
			coinbasepro.GetHistoricRatesParams{
				Start:       start,
				End:         end,
				Granularity: granularity,
			},
		)
	}

	return products, params, nil
}
