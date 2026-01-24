// internal/vikunja/integration_test.go
//go:build integration

package vikunja

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegration_FetchAndConvertTasks(t *testing.T) {
	// Mock Vikunja API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/1/views/1/tasks":
			tasks := []Task{
				{
					ID:          1,
					Title:       "Test Task",
					Description: "Description",
					Done:        false,
					Priority:    3,
					ProjectID:   1,
					Created:     time.Now().Add(-24 * time.Hour),
					Updated:     time.Now(),
					Labels:      []Label{{Title: "bug"}},
					RelatedTasks: map[string][]Task{
						"blocking": {{ID: 2, Title: "Blocked Task"}},
					},
				},
				{
					ID:          2,
					Title:       "Blocked Task",
					Description: "Waiting on task 1",
					Done:        false,
					Priority:    2,
					ProjectID:   1,
					Created:     time.Now().Add(-24 * time.Hour),
					Updated:     time.Now(),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tasks)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token").
		WithProjectID(1).
		WithViewID(1)

	ctx := context.Background()
	tasks, err := client.FetchTasks(ctx, "all")
	if err != nil {
		t.Fatalf("FetchTasks failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("Got %d tasks, want 2", len(tasks))
	}

	// Test conversion
	config := DefaultMappingConfig()
	conversion := TaskToBeads(&tasks[0], server.URL, config)

	if conversion.Issue == nil {
		t.Fatal("Conversion returned nil issue")
	}

	if len(conversion.Dependencies) != 1 {
		t.Errorf("Got %d dependencies, want 1", len(conversion.Dependencies))
	}
}
