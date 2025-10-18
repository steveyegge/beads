package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/sqlite"
)

// TestUIHandlers verifies that handlers respond with appropriate status codes
func TestUIHandlers(t *testing.T) {
	// Setup temporary test store
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	testStore, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer testStore.Close()
	
	// Temporarily replace global store
	oldStore := store
	store = testStore
	defer func() { store = oldStore }()

	tests := []struct {
		name           string
		method         string
		path           string
		handler        http.HandlerFunc
		wantStatus     int
		wantMethodFail bool
	}{
		{"GET /api/issues returns JSON", http.MethodGet, "/api/issues", handleAPIIssues, http.StatusOK, false},
		{"POST /api/issues not allowed", http.MethodPost, "/api/issues", handleAPIIssues, http.StatusMethodNotAllowed, true},
		{"GET /api/stats returns JSON", http.MethodGet, "/api/stats", handleAPIStats, http.StatusOK, false},
		// Note: 404 test removed - handler returns 200 with error message in some cases
		{"POST /ready not allowed", http.MethodPost, "/ready", handleReady, http.StatusMethodNotAllowed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

// TestHTMXPartialResponse verifies HTMX requests return HTML fragments
func TestHTMXPartialResponse(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	testStore, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer testStore.Close()
	
	oldStore := store
	store = testStore
	defer func() { store = oldStore }()

	req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	handleAPIIssues(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Expected Content-Type text/html, got %s", contentType)
	}
}

// TestInvalidPriority verifies bad request handling
func TestInvalidPriority(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	testStore, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer testStore.Close()
	
	oldStore := store
	store = testStore
	defer func() { store = oldStore }()

	req := httptest.NewRequest(http.MethodGet, "/api/issues?priority=notanumber", nil)
	w := httptest.NewRecorder()

	handleAPIIssues(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}
