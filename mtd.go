package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
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
	maxErrors = 20 // Maximum number of errors before giving up
	debug     = false // Set to true for debug output
)

// Global error counter
var errorCount int

// ------------------------------------
// Step 1: Get S&P 500 tickers
// ------------------------------------
func getSP500Tickers() ([]string, error) {
	url := "https://en.wikipedia.org/wiki/List_of_S%26P_500_companies"
	c := colly.NewCollector()
	var tickers []string
	errorCount = 0 // Reset error counter at start

	c.OnHTML("table.wikitable tbody tr", func(e *colly.HTMLElement) {
		// Get the first column (ticker symbol) from each row
		ticker := e.ChildText("td:nth-child(1) a")
		// If no link, try getting the text directly
		if ticker == "" {
			ticker = e.ChildText("td:nth-child(1)")
		}
		// Clean up and validate the ticker
		ticker = strings.TrimSpace(ticker)
		if ticker != "" && ticker != "Symbol" && len(ticker) < 10 { // Basic validation
			tickers = append(tickers, ticker)
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
		return nil, fmt.Errorf("error visiting %s: %v", url, err)
	}

	if len(tickers) == 0 {
		return nil, fmt.Errorf("no tickers found on the page")
	}

	fmt.Printf("Found %d tickers\n", len(tickers))
	return tickers, nil
}

// ------------------------------------
// Step 2: Get month start and end
// ------------------------------------
func getMonthRange(year int, month time.Month) (time.Time, time.Time) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
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
	Return     float64
	BarCount   int
	FirstClose string
	LastClose  string
}

func main() {
	// Use the previous month to ensure data is available
	now := time.Now()
	firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	firstOfLastMonth := firstOfThisMonth.AddDate(0, -1, 0)
	year := firstOfLastMonth.Year()
	month := firstOfLastMonth.Month()
	start, end := getMonthRange(year, month)

	fmt.Printf("📅 Fetching S&P 500 MTD returns for %s %d (from %s to %s)...\n", 
		month, year, 
		start.Format("2006-01-02"), 
		end.Format("2006-01-02"))

	tickers, err := getSP500Tickers()
	if err != nil {
		log.Fatalf("Failed to get tickers: %v", err)
	}

	results := []Result{}
	total := len(tickers)
	for i, t := range tickers {
		result, err := getMTDReturn(t, start, end)
		if err != nil {
			log.Printf("Skipping %s: %v", t, err)
		} else if !math.IsNaN(result.Return) {
			results = append(results, Result{
				Ticker:     t,
				Return:     result.Return,
				BarCount:   result.BarCount,
				FirstClose: result.FirstClose.String(),
				LastClose:  result.LastClose.String(),
			})
		}
		if (i+1)%50 == 0 {  // More frequent updates
			fmt.Printf("Processed %d/%d... (Found %d valid results so far)\n", i+1, total, len(results))
		}
	}

	// Sort by return descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Return > results[j].Return
	})

	// Write CSV
	file, err := os.Create("sp500_mtd_returns.csv")
	if err != nil {
		log.Fatalf("Failed to create CSV: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()
	writer.Write([]string{"Ticker", "MTD_Return", "MTD_%", "Bars", "First_Close", "Last_Close"})

	for _, r := range results {
		writer.Write([]string{
			r.Ticker,
			fmt.Sprintf("%.6f", r.Return),
			fmt.Sprintf("%.2f", r.Return*100),
			fmt.Sprintf("%d", r.BarCount),
			r.FirstClose,
			r.LastClose,
		})
	}

	fmt.Println("✅ Saved results to sp500_mtd_returns.csv")
	fmt.Println("🏁 Top 10 performers:")
	for i := 0; i < 10 && i < len(results); i++ {
		fmt.Printf("%-6s  %6.2f%%\n", results[i].Ticker, results[i].Return*100)
	}
}
