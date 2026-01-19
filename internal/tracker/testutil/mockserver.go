//go:build integration

// Package testutil provides testing utilities for tracker integration tests.
package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// RecordedRequest stores information about a request made to the mock server.
type RecordedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

// MockResponse represents a configured response for the mock server.
type MockResponse struct {
	StatusCode int
	Body       interface{}
	Headers    map[string]string
}

// MockTrackerServer is the base mock server for tracker integration tests.
// It provides request recording, response configuration, and common helpers.
type MockTrackerServer struct {
	Server *httptest.Server
	mu     sync.RWMutex

	// Recorded requests for assertions
	requests []RecordedRequest

	// Response configuration
	responses      map[string]MockResponse // path -> response
	defaultHandler func(w http.ResponseWriter, r *http.Request)

	// Error simulation
	authError      bool
	rateLimitError bool
	serverError    bool

	// Rate limit retry tracking
	rateLimitRetries int
	rateLimitCount   int
}

// NewMockTrackerServer creates a new base mock server.
func NewMockTrackerServer() *MockTrackerServer {
	m := &MockTrackerServer{
		requests:  []RecordedRequest{},
		responses: make(map[string]MockResponse),
	}

	m.Server = httptest.NewServer(http.HandlerFunc(m.handleRequest))
	return m
}

// handleRequest is the main request handler that records requests and returns configured responses.
func (m *MockTrackerServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Read and record the request body
	var body []byte
	if r.Body != nil {
		body, _ = readBody(r)
	}

	m.mu.Lock()
	m.requests = append(m.requests, RecordedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
	})
	m.mu.Unlock()

	// Check for error simulation
	if m.authError {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "Unauthorized"})
		return
	}

	if m.rateLimitError {
		m.rateLimitCount++
		if m.rateLimitCount <= m.rateLimitRetries {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Retry-After", "1")
			writeJSON(w, map[string]string{"error": "Rate limited"})
			return
		}
		// After retries, allow the request through
	}

	if m.serverError {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "Internal server error"})
		return
	}

	// Check for configured response
	m.mu.RLock()
	resp, found := m.responses[r.URL.Path]
	m.mu.RUnlock()

	if found {
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		if resp.StatusCode != 0 {
			w.WriteHeader(resp.StatusCode)
		}
		if resp.Body != nil {
			writeJSON(w, resp.Body)
		}
		return
	}

	// Call custom handler if set
	if m.defaultHandler != nil {
		m.defaultHandler(w, r)
		return
	}

	// Default: 404
	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "Not found"})
}

// URL returns the mock server URL.
func (m *MockTrackerServer) URL() string {
	return m.Server.URL
}

// Close shuts down the mock server.
func (m *MockTrackerServer) Close() {
	m.Server.Close()
}

// SetResponse configures a response for a specific path.
func (m *MockTrackerServer) SetResponse(path string, statusCode int, body interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[path] = MockResponse{
		StatusCode: statusCode,
		Body:       body,
	}
}

// SetResponseWithHeaders configures a response with custom headers.
func (m *MockTrackerServer) SetResponseWithHeaders(path string, statusCode int, body interface{}, headers map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[path] = MockResponse{
		StatusCode: statusCode,
		Body:       body,
		Headers:    headers,
	}
}

// SetDefaultHandler sets a custom handler for unmatched requests.
func (m *MockTrackerServer) SetDefaultHandler(handler func(w http.ResponseWriter, r *http.Request)) {
	m.defaultHandler = handler
}

// SetAuthError enables/disables 401 Unauthorized responses.
func (m *MockTrackerServer) SetAuthError(enabled bool) {
	m.authError = enabled
}

// SetRateLimitError enables/disables 429 Too Many Requests responses.
// retries specifies how many requests should get rate limited before succeeding.
func (m *MockTrackerServer) SetRateLimitError(enabled bool, retries int) {
	m.rateLimitError = enabled
	m.rateLimitRetries = retries
	m.rateLimitCount = 0
}

// SetServerError enables/disables 500 Internal Server Error responses.
func (m *MockTrackerServer) SetServerError(enabled bool) {
	m.serverError = enabled
}

// GetRequests returns all recorded requests.
func (m *MockTrackerServer) GetRequests() []RecordedRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]RecordedRequest, len(m.requests))
	copy(result, m.requests)
	return result
}

// GetRequestCount returns the number of recorded requests.
func (m *MockTrackerServer) GetRequestCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.requests)
}

// ClearRequests clears all recorded requests.
func (m *MockTrackerServer) ClearRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = []RecordedRequest{}
}

// Reset clears all recorded requests and responses.
func (m *MockTrackerServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = []RecordedRequest{}
	m.responses = make(map[string]MockResponse)
	m.authError = false
	m.rateLimitError = false
	m.serverError = false
	m.rateLimitCount = 0
	m.rateLimitRetries = 0
}

// Helper functions

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()

	buf := make([]byte, 0, 1024)
	for {
		tmp := make([]byte, 512)
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}
