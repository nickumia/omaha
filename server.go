package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Server holds the web server state
type Server struct {
	templates map[string]*template.Template
	results   []Result
	mu        sync.RWMutex
}

// NewServer creates a new server instance
func NewServer() *Server {
	s := &Server{
		templates: make(map[string]*template.Template),
	}
	s.loadTemplates()
	return s
}

// loadTemplates loads all HTML templates
func (s *Server) loadTemplates() {
	templateFiles, err := filepath.Glob("templates/*.html")
	if err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}

	funcMap := template.FuncMap{
		"mult": func(a float64, b float64) float64 { return a * b },
	}

	for _, tmpl := range templateFiles {
		t, err := template.New(filepath.Base(tmpl)).Funcs(funcMap).ParseFiles(tmpl)
		if err != nil {
			log.Fatalf("Error parsing template %s: %v", tmpl, err)
		}
		s.templates[filepath.Base(tmpl)] = t
	}
}

// UpdateResults updates the stored results in a thread-safe way
func (s *Server) UpdateResults(results []Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = results
}

// handleIndex renders the main page
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tmpl, ok := s.templates["index.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, s.results); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleAPI returns the results as JSON
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.results); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleRefresh triggers a refresh of the MTD data
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// if r.Method != http.MethodPost || r.Method != http.MethodGet {
	// 	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	// 	return
	// }

	// Parse query parameters for year and month
	query := r.URL.Query()
	year := 0
	month := time.Month(0)
	day := 0

	if y := query.Get("year"); y != "" {
		if y, err := strconv.Atoi(y); err == nil && y > 0 {
			year = y
		}
	}

	if m := query.Get("month"); m != "" {
		if m, err := strconv.Atoi(m); err == nil && m >= 1 && m <= 12 {
			month = time.Month(m)
		}
	}

	if d := query.Get("day"); d != "" {
		if d, err := strconv.Atoi(d); err == nil && d >= 1 && d <= 31 {
			day = d
		}
	}

	results, err := getMTDResults(year, month, day)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to refresh data: %v", err), http.StatusInternalServerError)
		return
	}

	s.UpdateResults(results)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// Start starts the web server
func (s *Server) Start(addr string) error {

	// Register routes
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/api/results", s.handleAPI)
	http.HandleFunc("/api/mtd", s.handleRefresh)

	// Start server
	server := &http.Server{
		Addr:         addr,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("🚀 Server starting on http://%s\n", addr)
	return server.ListenAndServe()
}
