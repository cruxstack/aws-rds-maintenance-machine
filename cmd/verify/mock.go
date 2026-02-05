package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RequestRecord captures details of an HTTP request.
type RequestRecord struct {
	Timestamp time.Time           `json:"timestamp"`
	Method    string              `json:"method"`
	Host      string              `json:"host"`
	Path      string              `json:"path"`
	Query     string              `json:"query,omitempty"`
	Headers   map[string][]string `json:"headers"`
	Body      string              `json:"body,omitempty"`
	Action    string              `json:"action,omitempty"` // AWS API action
}

// MockResponse defines a canned HTTP response.
type MockResponse struct {
	Service    string            `yaml:"service" json:"service"`
	Method     string            `yaml:"method" json:"method"`
	Path       string            `yaml:"path" json:"path"`
	Action     string            `yaml:"action,omitempty" json:"action,omitempty"` // AWS API action to match
	StatusCode int               `yaml:"status_code" json:"status_code"`
	Headers    map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body       string            `yaml:"body" json:"body"`
}

// MockServer simulates an HTTP API service.
type MockServer struct {
	name      string
	mu        sync.Mutex
	requests  []RequestRecord
	responses []MockResponse // Keep as list for action matching
	verbose   bool
}

// NewMockServer creates a new mock HTTP server.
func NewMockServer(name string, responses []MockResponse, verbose bool) *MockServer {
	filtered := make([]MockResponse, 0)
	for _, r := range responses {
		if strings.EqualFold(r.Service, name) {
			filtered = append(filtered, r)
		}
	}
	return &MockServer{
		name:      name,
		requests:  make([]RequestRecord, 0),
		responses: filtered,
		verbose:   verbose,
	}
}

// ServeHTTP records the request and returns a matching mock response.
func (ms *MockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	// Parse the body to extract the Action parameter (AWS API uses form-encoded body)
	action := ""
	if values, err := url.ParseQuery(string(body)); err == nil {
		action = values.Get("Action")
	}

	rec := RequestRecord{
		Timestamp: time.Now(),
		Method:    r.Method,
		Host:      r.Host,
		Path:      r.URL.Path,
		Query:     r.URL.RawQuery,
		Headers:   r.Header,
		Body:      string(body),
		Action:    action,
	}

	ms.mu.Lock()
	ms.requests = append(ms.requests, rec)
	ms.mu.Unlock()

	if ms.verbose {
		actionStr := ""
		if action != "" {
			actionStr = fmt.Sprintf(" [%s]", action)
		}
		fmt.Printf("    -> %-6s %-4s %s%s\n", ms.name, r.Method, r.URL.Path, actionStr)
	}

	// Find matching response by action first, then path
	for _, resp := range ms.responses {
		// If response has action defined, it must match
		if resp.Action != "" {
			if action == resp.Action && r.Method == resp.Method {
				ms.sendResponse(w, resp)
				return
			}
			continue
		}

		// No action specified, match by method and path
		if r.Method == resp.Method && matchPath(r.URL.Path, resp.Path) {
			ms.sendResponse(w, resp)
			return
		}
	}

	// Default response
	if ms.verbose {
		fmt.Printf("    !  %-6s No mock for: %s %s (action: %s)\n", ms.name, r.Method, r.URL.Path, action)
	}
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ErrorResponse><Error><Code>NotFound</Code><Message>No mock response configured</Message></Error></ErrorResponse>`))
}

func (ms *MockServer) sendResponse(w http.ResponseWriter, resp MockResponse) {
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/xml")
	}
	w.WriteHeader(resp.StatusCode)
	w.Write([]byte(resp.Body))
}

// GetRequests returns all captured requests.
func (ms *MockServer) GetRequests() []RequestRecord {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	reqs := make([]RequestRecord, len(ms.requests))
	copy(reqs, ms.requests)
	return reqs
}

// matchPath checks if a path matches a pattern (supports wildcards).
func matchPath(actual, pattern string) bool {
	if pattern == actual {
		return true
	}
	// Simple wildcard matching
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(actual) >= len(prefix) && actual[:len(prefix)] == prefix
	}
	return false
}
