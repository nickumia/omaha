package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gocolly/colly"
	"github.com/piquette/finance-go/chart"
	"github.com/piquette/finance-go/datetime"
	"github.com/shopspring/decimal"
)

// ------------------------------------
// Configuration
// ------------------------------------
const (
	maxErrors   = 20   // Maximum number of errors before giving up
	debug       = false // Set to true for debug output
	maxWorkers  = 10    // Maximum number of concurrent workers
)

// Global error counter
var errorCount int

// ------------------------------------
// Step 1: Get S&P 500 tickers
// ------------------------------------
func getSP500Tickers() ([]string, []string, error) {
	url := "https://en.wikipedia.org/wiki/List_of_S%26P_500_companies"
	c := colly.NewCollector()
	var tickers []string
	var sectors []string
	errorCount = 0 // Reset error counter at start

	c.OnHTML("table.wikitable tbody tr", func(e *colly.HTMLElement) {
		// Get the first column (ticker symbol) from each row
		ticker := e.ChildText("td:nth-child(1) a")
		sector := e.ChildText("td:nth-child(3)")
		// If no link, try getting the text directly
		if ticker == "" {
			ticker = e.ChildText("td:nth-child(1)")
		}
		// Clean up and validate the ticker
		ticker = strings.TrimSpace(ticker)
		if ticker != "" && ticker != "Symbol" && len(ticker) < 10 { // Basic validation
			tickers = append(tickers, ticker)
			sectors = append(sectors, sector)
		}
	})

	// Set error handler
	c.OnError(func(r *colly.Response, err error) {
		errorCount++
		log.Printf("Error %d/%d - URL: %s failed with response: %v\nError: %v", 
			errorCount, maxErrors, r.Request.URL, r.StatusCode, err)
		
		if errorCount >= maxErrors {
			log.Fatalf("Reached maximum number of errors (%d). Exiting...", maxErrors)
		}
	})

	fmt.Println("Fetching S&P 500 tickers from Wikipedia...")
	if err := c.Visit(url); err != nil {
		return nil, nil, fmt.Errorf("error visiting %s: %v", url, err)
	}

	if len(tickers) == 0 {
		return nil, nil, fmt.Errorf("no tickers found on the page")
	}

	fmt.Printf("Found %d tickers\n", len(tickers))
	return tickers, sectors, nil
}

// ------------------------------------
// Step 2: Get month start and end
// ------------------------------------
func getMonthRange(year int, month time.Month, day int) (time.Time, time.Time) {
	start := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

// ------------------------------------
// Step 3: Compute MTD return from Yahoo
// ------------------------------------
type MTDResult struct {
	Return     float64
	BarCount   int
	FirstClose decimal.Decimal
	LastClose  decimal.Decimal
}

func getMTDReturn(ticker string, start, end time.Time) (MTDResult, error) {
	if debug {
		fmt.Printf("🔍 Fetching data for %s from %s to %s\n", ticker, start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	
	params := &chart.Params{
		Symbol:   ticker,
		Start:    datetime.FromUnix(int(start.Unix())),
		End:      datetime.FromUnix(int(end.Unix())),
		Interval: datetime.OneDay,
	}

	iter := chart.Get(params)
	var firstClose, lastClose decimal.Decimal
	firstSet := false
	barCount := 0

	for iter.Next() {
		bar := iter.Bar()
		barCount++
		if !firstSet {
			firstClose = bar.Close
			firstSet = true
		}
		lastClose = bar.Close
	}

	if err := iter.Err(); err != nil {
		errMsg := fmt.Sprintf("❌ Error fetching data for %s: %v", ticker, err)
		// Try to extract more details if it's a finance-go error
		if ferr, ok := err.(interface{ Code() string }); ok {
			errMsg += fmt.Sprintf(" (Code: %s)", ferr.Code())
		}
		if ferr, ok := err.(interface{ Detail() string }); ok {
			errMsg += fmt.Sprintf(" (Detail: %s)", ferr.Detail())
		}
		fmt.Println(errMsg)
		return MTDResult{Return: math.NaN()}, fmt.Errorf(errMsg)
	}
	if !firstSet || firstClose.IsZero() {
		fmt.Printf("⚠️  No data found for %s\n", ticker)
		return MTDResult{Return: math.NaN()}, fmt.Errorf("no data")
	}

	mtd := lastClose.Div(firstClose).Sub(decimal.NewFromInt(1))
	mtdFloat, _ := mtd.Float64()
	return MTDResult{
		Return:     mtdFloat,
		BarCount:   barCount,
		FirstClose: firstClose,
		LastClose:  lastClose,
	}, nil
}

// ------------------------------------
// Step 4: Main
// ------------------------------------
type Result struct {
	Ticker     string
	Sector     string
	Return     float64
	BarCount   int
	FirstClose string
	LastClose  string
}

type SectorReturn struct {
	Sector      string
	AvgReturn   float64
	TickerCount int
}

// calculateSectorReturns calculates average returns by sector
func calculateSectorReturns(results []Result) []SectorReturn {
	sectorMap := make(map[string]struct {
		totalReturn float64
		count       int
	})

	// Calculate total returns per sector
	for _, r := range results {
		if r.Sector == "" {
			continue
		}
		sector := sectorMap[r.Sector]
		sector.totalReturn += r.Return
		sector.count++
		sectorMap[r.Sector] = sector
	}

	// Calculate average returns
	var sectorReturns []SectorReturn
	for sector, data := range sectorMap {
		if data.count > 0 {
			sectorReturns = append(sectorReturns, SectorReturn{
				Sector:      sector,
				AvgReturn:   data.totalReturn / float64(data.count),
				TickerCount: data.count,
			})
		}
	}

	// Sort by average return (descending)
	sort.Slice(sectorReturns, func(i, j int) bool {
		return sectorReturns[i].AvgReturn > sectorReturns[j].AvgReturn
	})

	return sectorReturns
}

// writeResultsToCSV writes both individual ticker data and sector summary to a CSV file
func writeResultsToCSV(results []Result, sectorReturns []SectorReturn, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create CSV: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header for ticker data
	if err := writer.Write([]string{"Ticker", "Sector", "Return", "MTD_%", "Bars", "First_Close", "Last_Close"}); err != nil {
		return err
	}

	// Write individual ticker data
	for _, r := range results {
		if err := writer.Write([]string{
			r.Ticker,
			r.Sector,
			fmt.Sprintf("%.6f", r.Return),
			fmt.Sprintf("%.2f%%", r.Return*100),
			fmt.Sprintf("%d", r.BarCount),
			r.FirstClose,
			r.LastClose,
		}); err != nil {
			return err
		}
	}

	// Add a separator
	if err := writer.Write([]string{""}); err != nil {
		return err
	}

	// Write sector summary header
	if err := writer.Write([]string{"Sector", "Avg_Return", "Ticker_Count"}); err != nil {
		return err
	}

	// Write sector data
	for _, sr := range sectorReturns {
		if err := writer.Write([]string{
			sr.Sector,
			fmt.Sprintf("%.2f%%", sr.AvgReturn*100),
			fmt.Sprintf("%d", sr.TickerCount),
		}); err != nil {
			return err
		}
	}

	return nil
}

// getMTDResults fetches month-to-date returns for a specific month and year
// If year and month are 0, it will use the previous month
func getMTDResults(year int, month time.Month, day int) ([]Result, error) {
	// If year and month are not provided, use previous month
	if year == 0 || month == 0 {
		lastMonth := time.Now().AddDate(0, -1, 0)
		year, month, day = lastMonth.Year(), lastMonth.Month(), lastMonth.Day()
	}

	start, end := getMonthRange(year, month, day)

	fmt.Printf("📅 Fetching S&P 500 MTD returns for %s %d (from %s to %s)...\n", 
		month, year, 
		start.Format("2006-01-02"), 
		end.Format("2006-01-02"))

	tickers, sectors, err := getSP500Tickers()
	if err != nil {
		log.Fatalf("Failed to get tickers: %v", err)
	}

	// Create a map to store sector data
	sectorData := make(map[string]struct {
		totalReturn float64
		count       int
	})

	// Process tickers in parallel
	type jobResult struct {
		ticker string
		sector string
		result MTDResult
		err    error
	}

	// Calculate number of workers (use number of CPU cores * 2, but not more than maxWorkers to avoid rate limiting)
	workers := runtime.NumCPU() * 2
	if workers > maxWorkers {
		workers = maxWorkers
	}

	// Process tickers in parallel using a worker pool
	numTickers := len(tickers)
	jobs := make(chan jobResult, numTickers)
	results := make(chan jobResult, numTickers)

	// Start workers
	for w := 0; w < workers; w++ {
		go func() {
			for j := range jobs {
				result, err := getMTDReturn(j.ticker, start, end)
				if err != nil {
					results <- jobResult{ticker: j.ticker, sector: j.sector, err: err}
					continue
				}
				results <- jobResult{ticker: j.ticker, sector: j.sector, result: result}
			}
		}()
	}

	// Send jobs
	go func() {
		for i, ticker := range tickers {
			sector := "Unknown"
			if i < len(sectors) {
				sector = sectors[i]
			}
			jobs <- jobResult{ticker: ticker, sector: sector}
		}
		close(jobs)
	}()

	// Collect results
	var validResults []Result
	var errs []error

	for i := 0; i < numTickers; i++ {
		res := <-results
		if res.err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", res.ticker, res.err))
			continue
		}

		result := Result{
			Ticker:     res.ticker,
			Sector:     res.sector,
			Return:     res.result.Return,
			BarCount:   res.result.BarCount,
			FirstClose: res.result.FirstClose.String(),
			LastClose:  res.result.LastClose.String(),
		}
		validResults = append(validResults, result)

		// Update sector data
		sd := sectorData[res.sector]
		sd.totalReturn += result.Return
		sd.count++
		sectorData[res.sector] = sd
	}

	// Log any errors
	if len(errs) > 0 {
		log.Printf("Completed with %d errors during processing\n", len(errs))
	}

	// Log any errors from parallel processing
	if len(errs) > 0 {
		log.Printf("Completed with %d errors during processing\n", len(errs))
	}

	// Sort valid results by return descending
	sort.Slice(validResults, func(i, j int) bool {
		return validResults[i].Return > validResults[j].Return
	})

	// Convert sector data to slice for sorting
	var sectorReturns []SectorReturn
	for sector, data := range sectorData {
		sectorReturns = append(sectorReturns, SectorReturn{
			Sector:      sector,
			AvgReturn:   data.totalReturn / float64(data.count),
			TickerCount: data.count,
		})
	}

	// Sort by average return (descending)
	sort.Slice(sectorReturns, func(i, j int) bool {
		return sectorReturns[i].AvgReturn > sectorReturns[j].AvgReturn
	})

	// Write results to CSV
	outputFile := "sp500_mtd_returns.csv"
	if err := writeResultsToCSV(validResults, sectorReturns, outputFile); err != nil {
		log.Printf("Warning: Failed to write CSV: %v", err)
	} else {
		log.Printf("✅ Saved results to %s\n", outputFile)

		// Log top 5 sectors
		log.Println("\n🏆 Top 5 Performing Sectors:")
		for i := 0; i < 5 && i < len(sectorReturns); i++ {
			sr := sectorReturns[i]
			log.Printf("%-30s %6.2f%% (%d tickers)", 
				sr.Sector + ":", sr.AvgReturn*100, sr.TickerCount)
		}
	}

	return validResults, nil
}

func main() {
	// Initialize the server
	server := NewServer()

	// Start the server in a goroutine
	go func() {
		if err := server.Start(":8080"); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	log.Println("🚀 Server started. Use the refresh button in the UI to load data.")

	// Keep the program running
	select {}
}
