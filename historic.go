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

	"gorm.io/gorm/clause"

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

// Converts to a HistoricalData type
func convertRates(rate coinbasepro.HistoricRate, params HistoricRateParams) (data HistoricalData) {
	return HistoricalData{
		Time:        rate.Time,
		Coin:        params.Product,
		High:        rate.High,
		Low:         rate.Low,
		Open:        rate.Open,
		Close:       rate.Close,
		Volume:      rate.Volume,
		Granularity: params.Granularity,
	}
}

func GetHistoricRatesGranularities() []int {
	return []int{60, 300, 900, 3600, 21600, 86400}
}

// SaveHistoricalRates grabs the data for a coin
func SaveHistoricalRates(
	client *coinbasepro.Client,
	rl *ratelimit.RateLimiter,
	params HistoricRateParams,
) (err error) {
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
		return errors.New("invalid granularity provided")
	}

	// Get the next 300 requests
	nextTime := params.Start.Add(time.Duration(params.Granularity) * time.Second * MaxHistoricRates)
	tempTime := params.Start
	getRates := func(param HistoricRateParams) error {
		// Be brief on the critical section (although in 1 thread right now this doesn't matter)
		rl.Lock()
		// Get the historic rates
		r, err := client.GetHistoricRates(param.Product, param.toParams())
		rl.Unlock()
		// Check to see if we are being rate limited
		for err != nil && err.Error() == "Public rate limit exceeded" {
			// Bump up the limit
			rl.Increase()
			// Be brief on the critical section (although in 1 thread right now this doesn't matter)
			rl.Lock()
			// Get the historic rates
			r, err = client.GetHistoricRates(param.Product, param.toParams())
			rl.Unlock()
		}
		// This is a bad error
		if err != nil {
			log.Println(err, params.Product)
			return err
		} else {
			// We didn't get rate limited so we can slowly decrease the rate limit
			rl.Decrease()
		}

		rates := make([]HistoricalData, 0, len(r))
		// Append the rates
		for _, rate := range r {
			rates = append(rates, convertRates(rate, params))
		}
		// Write out to the DB
		DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "time"}},
			DoNothing: true,
		}).Create(&rates)

		// Move forward the times
		tempTime = nextTime.Add(time.Duration(params.Granularity) * time.Second)
		nextTime = tempTime.Add(time.Duration(params.Granularity) * time.Second * MaxHistoricRates)

		return nil
	}

	log.Printf(
		"Getting historical rates for %s from %s to %s",
		params.Product,
		params.Start,
		params.End,
	)

	// Loop through all of the requests to get them
	for nextTime.Before(params.End) {
		// Send the job channel the new params to get
		param := HistoricRateParams{
			Product:     params.Product,
			Start:       tempTime,
			End:         nextTime,
			Granularity: params.Granularity,
		}
		err = getRates(param)
		if err != nil {
			return err
		}
	}

	// Make the last request
	param := HistoricRateParams{
		Product:     params.Product,
		Start:       tempTime,
		End:         params.End,
		Granularity: params.Granularity,
	}
	return getRates(param)
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