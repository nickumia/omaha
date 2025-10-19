# Omaha

(Aspirational) A Stock Recommender aimed at making high profit through 30-day holding periods. This tool fetches and analyzes S&P 500 stock performance data, providing month-to-date returns and sector-based analytics.

## Features

- **Recent Data**: Fetches the latest S&P 500 stock data
- **Sector Analysis**: Groups stocks by sector and calculates average returns
- **Parallel Processing**: Efficiently processes multiple stocks concurrently
- **REST API**: Simple HTTP endpoints for integration with other tools
- **CSV Export**: Save results in a structured CSV format

## API Endpoints

### 1. Get MTD (Month-To-Date) Returns

```
GET /api/mtd?year=YYYY&month=M&day=D
```

Fetches and calculates month-to-date returns for all S&P 500 stocks.

**Query Parameters:**
- `year` (optional): The target year (defaults to current year)
- `month` (optional): The target month (1-12, defaults to current month)
- `day` (optional): The target day (1-31, defaults to current day)

**Example Response (JSON):**
```json
[
  {
    "ticker": "AAPL",
    "sector": "Technology",
    "return": 0.0456,
    "bar_count": 15,
    "first_close": "150.25",
    "last_close": "156.80"
  },
  ...
]
```

### 2. Get Cached Results

```
GET /api/results
```

Returns the most recently fetched results without recalculating.

**Response:** Same as `/api/mtd` endpoint.

## CSV Output

The application generates a CSV file (`sp500_mtd_returns.csv`) with two sections:

1. **Ticker Data**: Individual stock performance
   - Ticker, Sector, Return, MTD_%, Bars, First_Close, Last_Close

2. **Sector Summary**: Aggregated sector performance
   - Sector, Avg_Return, Ticker_Count

## Getting Started

1. **Prerequisites**
   - Go 1.16+
   - Internet connection (for fetching stock data)

2. **Installation**
   ```bash
   git clone https://github.com/nickumia/omaha.git
   cd omaha
   go mod download
   ```

3. **Running the Server**
   ```bash
   go run .
   ```
   The server will start on `http://localhost:8080`

4. **Using the API**
   - Fetch current month's data: `http://localhost:8080/api/mtd`
   - Fetch specific month: `http://localhost:8080/api/mtd?year=2025&month=9&day=17`
   - Get cached results: `http://localhost:8080/api/results`

## Rate Limiting

- The application implements a worker pool to limit concurrent requests (default: 2x CPU cores, max 10)
- Failed requests are automatically retried with exponential backoff

## Error Handling

- Failed stock lookups are logged and skipped
- The API returns appropriate HTTP status codes for errors
- Detailed error messages are included in the response body
