package main

import (
	"encoding/csv"
	"errors"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"lucrum/ratelimit"

	"github.com/preichenberger/go-coinbasepro/v2"
)

// Max amount of rates you are allowed to request at a time
const MaxHistoricRates time.Duration = 300

// Helper to keep data neat
type HistoricRateParams struct {
	Product     string
	Start       time.Time
	End         time.Time
	Granularity int
}

func (p HistoricRateParams) toParams() coinbasepro.GetHistoricRatesParams {
	return coinbasepro.GetHistoricRatesParams{
		Start:       p.Start,
		End:         p.End,
		Granularity: p.Granularity,
	}
}

func getRates(
	client *coinbasepro.Client,
	limiter ratelimit.Limiter,
	jobChan <-chan HistoricRateParams,
	rateChan chan<- []coinbasepro.HistoricRate,
	wg *sync.WaitGroup,
) {
	for {
		select {
		// Listen for new jobs
		case params := <-jobChan:
			// Lock on the critical section
			// This way we don't make too many web requests at once
			limiter.Lock()
			// Get the rates
			rates, err := client.GetHistoricRates(params.Product, params.toParams())
			limiter.Unlock()
			// Is it fails we die and return
			if err != nil {
				wg.Done()
				return
			}
			// Send back the rates
			rateChan <- rates
		// Channel is closed and we are done
		default:
			wg.Done()
			return
		}
	}
}
func GetHistoricRatesGranularities() []int {
	return []int{60, 300, 900, 3600, 21600, 86400}
}

// GetHistoricRates grabs the data for a coin
func GetHistoricRates(
	client *coinbasepro.Client,
	limiter ratelimit.Limiter,
	params HistoricRateParams,
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

	wg := sync.WaitGroup{}
	jobChan := make(chan HistoricRateParams, 5)
	ratesChan := make(chan []coinbasepro.HistoricRate, 5)

	// Spin off some workers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go getRates(client, limiter, jobChan, ratesChan, &wg)
	}

	// Get the next 300 requests
	nextTime := params.Start.Add(time.Duration(params.Granularity) * time.Second * MaxHistoricRates)
	tempTime := params.Start
	for nextTime.Before(params.End) {
		// Send the job channel the new params to get
		jobChan <- HistoricRateParams{
			Product:     params.Product,
			Start:       tempTime,
			End:         nextTime,
			Granularity: params.Granularity,
		}
		// Move forward the times
		tempTime = nextTime
		nextTime = nextTime.Add(time.Duration(params.Granularity) * time.Second * MaxHistoricRates)
	}

	// Make the last request
	// Send the job channel the new params to get
	jobChan <- HistoricRateParams{
		Product:     params.Product,
		Start:       tempTime,
		End:         params.End,
		Granularity: params.Granularity,
	}
	// Close the channel to signal to the goroutines we are done

	for {
		select {
		case r := <-ratesChan:
			rates = append(rates, r...)
		}
	}
}

// ReadRateFile reads in a specific CSV file format to get historical data
// This format contains headers in the order "Product,Start,End,Granularity"
// The coin must be of format COIN/CURRENCY
// The times must be in the format of RFC3339
// End time is allowed to be "now" in order to get time.Now()
func ReadRateFile(fileName string) ([]HistoricRateParams, error) {
	var params []HistoricRateParams

	// Open the file
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(file)

	// Read in headers
	// Header order is "Product,Start,End,Granularity"
	_, err = reader.Read()
	if err != nil {
		return nil, err
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

		// Build our array to return
		params = append(
			params,
			HistoricRateParams{
				Product:     record[0],
				Start:       start,
				End:         end,
				Granularity: granularity,
			},
		)
	}

	return params, nil
}
