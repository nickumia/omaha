package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/gocolly/colly"
	"github.com/piquette/finance-go/chart"
	"github.com/piquette/finance-go/datetime"
	"github.com/shopspring/decimal"
)

// ------------------------------------
// Step 1: Get S&P 500 tickers
// ------------------------------------
func getSP500Tickers() ([]string, error) {
	url := "https://en.wikipedia.org/wiki/List_of_S%26P_500_companies"
	c := colly.NewCollector()
	var tickers []string

	c.OnHTML("table.wikitable tbody tr", func(e *colly.HTMLElement) {
		ticker := e.ChildText("td:nth-child(1)")
		if ticker != "" && ticker != "Symbol" {
			tickers = append(tickers, ticker)
		}
	})

	if err := c.Visit(url); err != nil {
		return nil, err
	}
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
func getMTDReturn(ticker string, start, end time.Time) (float64, error) {
	params := &chart.Params{
		Symbol:   ticker,
		Start:    datetime.FromUnix(int(start.Unix())),
		End:      datetime.FromUnix(int(end.Unix())),
		Interval: datetime.OneDay,
	}

	iter := chart.Get(params)
	var firstClose, lastClose decimal.Decimal
	firstSet := false

	for iter.Next() {
		bar := iter.Bar()
		if !firstSet {
			firstClose = bar.Close
			firstSet = true
		}
		lastClose = bar.Close
	}

	if err := iter.Err(); err != nil {
		return math.NaN(), err
	}
	if !firstSet || firstClose.IsZero() {
		return math.NaN(), fmt.Errorf("no data")
	}

	mtd := lastClose.Div(firstClose).Sub(decimal.NewFromInt(1))
	mtdFloat, _ := mtd.Float64()
	return mtdFloat, nil
}

// ------------------------------------
// Step 4: Main
// ------------------------------------
type Result struct {
	Ticker string
	Return float64
}

func main() {
	year := 2025
	month := time.September // change this as needed
	start, end := getMonthRange(year, month)

	fmt.Printf("📅 Fetching S&P 500 MTD returns for %s %d...\n", month, year)

	tickers, err := getSP500Tickers()
	if err != nil {
		log.Fatalf("Failed to get tickers: %v", err)
	}

	results := []Result{}
	total := len(tickers)
	for i, t := range tickers {
		mtd, err := getMTDReturn(t, start, end)
		if err == nil && !math.IsNaN(mtd) {
			results = append(results, Result{Ticker: t, Return: mtd})
		}
		if (i+1)%25 == 0 {
			fmt.Printf("Processed %d/%d...\n", i+1, total)
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
	writer.Write([]string{"Ticker", "MTD_Return", "MTD_%"} )

	for _, r := range results {
		writer.Write([]string{r.Ticker, fmt.Sprintf("%.6f", r.Return), fmt.Sprintf("%.2f", r.Return*100)})
	}

	fmt.Println("✅ Saved results to sp500_mtd_returns.csv")
	fmt.Println("🏁 Top 10 performers:")
	for i := 0; i < 10 && i < len(results); i++ {
		fmt.Printf("%-6s  %6.2f%%\n", results[i].Ticker, results[i].Return*100)
	}
}
