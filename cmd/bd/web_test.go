//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestBuildGraphData_Empty verifies graph response when no issues exist.
func TestBuildGraphData_Empty(t *testing.T) {
	RunDualModeTest(t, "graph_empty", func(t *testing.T, env *DualModeTestEnv) {
		if env.Mode() != DirectMode {
			t.Skip("graph requires direct storage access")
		}
		result := buildGraphData(env.Context(), env.Store(), "", false)
		if len(result.Nodes) != 0 {
			t.Errorf("expected 0 nodes, got %d", len(result.Nodes))
		}
		if len(result.Edges) != 0 {
			t.Errorf("expected 0 edges, got %d", len(result.Edges))
		}
	})
}

// TestBuildGraphData_WithIssues verifies graph includes open issues as nodes.
func TestBuildGraphData_WithIssues(t *testing.T) {
	RunDualModeTest(t, "graph_with_issues", func(t *testing.T, env *DualModeTestEnv) {
		if env.Mode() != DirectMode {
			t.Skip("graph requires direct storage access")
		}

		// create two issues with a dependency
		issueA := &types.Issue{
			Title:     "issue a",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  1,
		}
		issueB := &types.Issue{
			Title:     "issue b",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
		}
		if err := env.CreateIssue(issueA); err != nil {
			t.Fatalf("create issue a: %v", err)
		}
		if err := env.CreateIssue(issueB); err != nil {
			t.Fatalf("create issue b: %v", err)
		}

		// add dependency: B depends on A
		if err := env.AddDependency(issueB.ID, issueA.ID, types.DepBlocks); err != nil {
			t.Fatalf("add dependency: %v", err)
		}

		result := buildGraphData(env.Context(), env.Store(), "", false)
		if len(result.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(result.Nodes))
		}
		if len(result.Edges) < 1 {
			t.Errorf("expected at least 1 edge, got %d", len(result.Edges))
		}

		// verify node fields are populated
		for _, node := range result.Nodes {
			if node.ID == "" {
				t.Error("node has empty ID")
			}
			if node.Title == "" {
				t.Error("node has empty title")
			}
			if node.Status == "" {
				t.Error("node has empty status")
			}
		}
	})
}

// TestBuildGraphData_FocusID verifies subgraph filtering by issue ID.
func TestBuildGraphData_FocusID(t *testing.T) {
	RunDualModeTest(t, "graph_focus", func(t *testing.T, env *DualModeTestEnv) {
		if env.Mode() != DirectMode {
			t.Skip("graph requires direct storage access")
		}

		// create two unconnected issues
		issueA := &types.Issue{
			Title:     "focused issue",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  1,
		}
		issueB := &types.Issue{
			Title:     "unrelated issue",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
		}
		if err := env.CreateIssue(issueA); err != nil {
			t.Fatalf("create issue a: %v", err)
		}
		if err := env.CreateIssue(issueB); err != nil {
			t.Fatalf("create issue b: %v", err)
		}

		// focus on issue A — should only return A
		result := buildGraphData(env.Context(), env.Store(), issueA.ID, false)
		if len(result.Nodes) != 1 {
			t.Errorf("expected 1 focused node, got %d", len(result.Nodes))
		}
		if len(result.Nodes) > 0 && result.Nodes[0].ID != issueA.ID {
			t.Errorf("expected node %s, got %s", issueA.ID, result.Nodes[0].ID)
		}
	})
}

// TestBuildWebMux_APIIssues verifies the /api/issues endpoint via httptest.
func TestBuildWebMux_APIIssues(t *testing.T) {
	RunDualModeTest(t, "api_issues", func(t *testing.T, env *DualModeTestEnv) {
		if env.Mode() != DaemonMode {
			t.Skip("API endpoints require daemon client")
		}

		// create a test issue
		issue := &types.Issue{
			Title:     "api test issue",
			IssueType: types.TypeBug,
			Status:    types.StatusOpen,
			Priority:  0,
		}
		if err := env.CreateIssue(issue); err != nil {
			t.Fatalf("create issue: %v", err)
		}

		// build mux with real daemon client, no graph store
		mux := buildWebMux(env.Client(), nil, false)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		// test /api/issues
		resp, err := http.Get(ts.URL + "/api/issues")
		if err != nil {
			t.Fatalf("GET /api/issues: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("expected application/json content-type, got %s", ct)
		}

		body, _ := io.ReadAll(resp.Body)
		var issues []json.RawMessage
		if err := json.Unmarshal(body, &issues); err != nil {
			t.Fatalf("unmarshal issues: %v (body: %s)", err, string(body))
		}
		if len(issues) < 1 {
			t.Error("expected at least 1 issue in response")
		}
	})
}

// TestBuildWebMux_APIStats verifies the /api/stats endpoint.
func TestBuildWebMux_APIStats(t *testing.T) {
	RunDualModeTest(t, "api_stats", func(t *testing.T, env *DualModeTestEnv) {
		if env.Mode() != DaemonMode {
			t.Skip("API endpoints require daemon client")
		}

		mux := buildWebMux(env.Client(), nil, false)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/api/stats")
		if err != nil {
			t.Fatalf("GET /api/stats: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		var stats types.Statistics
		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &stats); err != nil {
			t.Fatalf("unmarshal stats: %v", err)
		}
	})
}

// TestBuildWebMux_IndexHTML verifies the root serves HTML.
func TestBuildWebMux_IndexHTML(t *testing.T) {
	// no daemon needed for static file serving
	mux := buildWebMux(nil, nil, false)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content-type, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "bd") {
		t.Error("expected index.html to contain 'bd'")
	}
}

// TestBuildWebMux_SSEEvents verifies the /api/events SSE endpoint.
func TestBuildWebMux_SSEEvents(t *testing.T) {
	mux := buildWebMux(nil, nil, false)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	// read the initial "connected" event
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	data := string(buf[:n])
	if !strings.Contains(data, "event: connected") {
		t.Errorf("expected 'event: connected' in SSE stream, got: %s", data)
	}
}

// TestBuildWebMux_APINoClient verifies endpoints return 503 when daemon is nil.
func TestBuildWebMux_APINoClient(t *testing.T) {
	mux := buildWebMux(nil, nil, false)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tests := []struct {
		name string
		path string
	}{
		{"issues", "/api/issues"},
		{"stats", "/api/stats"},
		{"ready", "/api/ready"},
		{"blocked", "/api/blocked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("expected 503 when daemon is nil, got %d", resp.StatusCode)
			}
		})
	}
}

// TestBuildWebMux_GraphNoStore verifies /api/graph returns 503 without store.
func TestBuildWebMux_GraphNoStore(t *testing.T) {
	mux := buildWebMux(nil, nil, false)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/graph")
	if err != nil {
		t.Fatalf("GET /api/graph: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when graph store is nil, got %d", resp.StatusCode)
	}
}

// TestBuildWebMux_StaticAssets verifies CSS and JS files are served.
func TestBuildWebMux_StaticAssets(t *testing.T) {
	mux := buildWebMux(nil, nil, false)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tests := []struct {
		name        string
		path        string
		contentType string
	}{
		{"css", "/static/css/styles.css", "text/css"},
		{"app_js", "/static/js/app.js", "javascript"},
		{"kanban_js", "/static/js/kanban.js", "javascript"},
		{"dagre_vendor", "/static/vendor/dagre.min.js", "javascript"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200 for %s, got %d", tt.path, resp.StatusCode)
			}
		})
	}
}

// TestHttpJSON verifies the httpJSON helper function.
func TestHttpJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       interface{}
		wantStatus int
	}{
		{
			name:       "200 with map",
			status:     200,
			body:       map[string]string{"key": "value"},
			wantStatus: 200,
		},
		{
			name:       "503 with error",
			status:     503,
			body:       map[string]string{"error": "not available"},
			wantStatus: 503,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httpJSON(rec, tt.status, tt.body)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("expected application/json, got %s", ct)
			}
		})
	}
}
