// Package github provides client and data types for the GitHub REST API.
package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewClient verifies the constructor creates a properly configured client.
func TestNewClient(t *testing.T) {
	client := NewClient("test-token", "owner", "repo")

	if client.Token != "test-token" {
		t.Errorf("Token = %q, want %q", client.Token, "test-token")
	}
	if client.Owner != "owner" {
		t.Errorf("Owner = %q, want %q", client.Owner, "owner")
	}
	if client.Repo != "repo" {
		t.Errorf("Repo = %q, want %q", client.Repo, "repo")
	}
	if client.BaseURL != DefaultAPIEndpoint {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, DefaultAPIEndpoint)
	}
	if client.HTTPClient == nil {
		t.Error("HTTPClient is nil, want non-nil default client")
	}
}

// TestClientWithHTTPClient verifies the builder pattern for custom HTTP client.
func TestClientWithHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	client := NewClient("token", "owner", "repo").WithHTTPClient(customClient)

	if client.HTTPClient != customClient {
		t.Error("HTTPClient not set to custom client")
	}
	if client.Token != "token" {
		t.Errorf("Token = %q, want %q", client.Token, "token")
	}
}

// TestClientWithBaseURL verifies custom base URL setting.
func TestClientWithBaseURL(t *testing.T) {
	client := NewClient("token", "owner", "repo").WithBaseURL("https://github.example.com/api/v3")

	if client.BaseURL != "https://github.example.com/api/v3" {
		t.Errorf("BaseURL = %q, want custom URL", client.BaseURL)
	}
	if client.Owner != "owner" {
		t.Errorf("Owner = %q, want %q", client.Owner, "owner")
	}
}

// TestBuildURL verifies URL construction for API endpoints.
func TestBuildURL(t *testing.T) {
	client := NewClient("token", "owner", "repo")

	tests := []struct {
		name    string
		path    string
		params  map[string]string
		wantURL string
	}{
		{
			name:    "issues endpoint",
			path:    "/repos/owner/repo/issues",
			params:  nil,
			wantURL: "https://api.github.com/repos/owner/repo/issues",
		},
		{
			name:    "with query params",
			path:    "/repos/owner/repo/issues",
			params:  map[string]string{"state": "open", "per_page": "100"},
			wantURL: "https://api.github.com/repos/owner/repo/issues",
		},
		{
			name:    "single issue",
			path:    "/repos/owner/repo/issues/42",
			params:  nil,
			wantURL: "https://api.github.com/repos/owner/repo/issues/42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.buildURL(tt.path, tt.params)
			if !strings.HasPrefix(got, tt.wantURL) {
				t.Errorf("buildURL(%q) = %q, want prefix %q", tt.path, got, tt.wantURL)
			}
			for k, v := range tt.params {
				if !strings.Contains(got, k+"="+v) {
					t.Errorf("buildURL missing param %s=%s in %q", k, v, got)
				}
			}
		})
	}
}

// TestFetchIssues_Success verifies fetching issues from GitHub API.
func TestFetchIssues_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %s, want GET", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("Authorization header = %q, want Bearer prefix", r.Header.Get("Authorization"))
		}
		if !strings.Contains(r.URL.Path, "/repos/owner/repo/issues") {
			t.Errorf("URL path = %s, want to contain /repos/owner/repo/issues", r.URL.Path)
		}

		issues := []Issue{
			{ID: 1, Number: 1, Title: "First issue", State: "open"},
			{ID: 2, Number: 2, Title: "Second issue", State: "open"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClient("test-token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	issues, err := client.FetchIssues(ctx, "open")
	if err != nil {
		t.Fatalf("FetchIssues() error = %v", err)
	}

	if len(issues) != 2 {
		t.Errorf("FetchIssues() returned %d issues, want 2", len(issues))
	}
	if issues[0].Title != "First issue" {
		t.Errorf("issues[0].Title = %q, want %q", issues[0].Title, "First issue")
	}
}

// TestFetchIssues_FiltersPullRequests verifies PRs are filtered out.
func TestFetchIssues_FiltersPullRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issues := []Issue{
			{ID: 1, Number: 1, Title: "Issue", State: "open"},
			{ID: 2, Number: 2, Title: "PR", State: "open", PullRequest: &PullRef{URL: "https://api.github.com/repos/o/r/pulls/2"}},
			{ID: 3, Number: 3, Title: "Another issue", State: "open"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	issues, err := client.FetchIssues(ctx, "open")
	if err != nil {
		t.Fatalf("FetchIssues() error = %v", err)
	}

	if len(issues) != 2 {
		t.Errorf("FetchIssues() returned %d issues, want 2 (PR filtered)", len(issues))
	}
}

// TestFetchIssues_Pagination verifies client handles paginated responses via Link header.
func TestFetchIssues_Pagination(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")

		if page == 1 {
			w.Header().Set("Link", `<`+r.URL.String()+`?page=2>; rel="next"`)
			issues := []Issue{{ID: 1, Number: 1, Title: "Issue 1"}}
			_ = json.NewEncoder(w).Encode(issues)
		} else {
			issues := []Issue{{ID: 2, Number: 2, Title: "Issue 2"}}
			_ = json.NewEncoder(w).Encode(issues)
		}
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	issues, err := client.FetchIssues(ctx, "all")
	if err != nil {
		t.Fatalf("FetchIssues() error = %v", err)
	}

	if len(issues) != 2 {
		t.Errorf("FetchIssues() returned %d issues, want 2 (from 2 pages)", len(issues))
	}
}

// TestFetchIssuesSince verifies incremental sync with since param.
func TestFetchIssuesSince(t *testing.T) {
	since := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	var capturedURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Issue{})
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	_, err := client.FetchIssuesSince(ctx, "all", since)
	if err != nil {
		t.Fatalf("FetchIssuesSince() error = %v", err)
	}

	if !strings.Contains(capturedURL, "since=2024-01-15") {
		t.Errorf("URL = %s, want to contain since=2024-01-15", capturedURL)
	}
}

// TestCreateIssue_Success verifies creating an issue via POST.
func TestCreateIssue_Success(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/repos/owner/repo/issues") {
			t.Errorf("URL path = %s, want to contain /repos/owner/repo/issues", r.URL.Path)
		}

		_ = json.NewDecoder(r.Body).Decode(&capturedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Issue{
			ID:     100,
			Number: 42,
			Title:  "New issue",
			State:  "open",
		})
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	issue, err := client.CreateIssue(ctx, "New issue", "Description here", []string{"bug", "priority:high"})
	if err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}

	if issue.Number != 42 {
		t.Errorf("issue.Number = %d, want 42", issue.Number)
	}
	if capturedBody["title"] != "New issue" {
		t.Errorf("request body title = %v, want %q", capturedBody["title"], "New issue")
	}
	if capturedBody["body"] != "Description here" {
		t.Errorf("request body body = %v, want %q", capturedBody["body"], "Description here")
	}
}

// TestUpdateIssue_Success verifies updating an issue via PATCH.
func TestUpdateIssue_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("Method = %s, want PATCH", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/repos/owner/repo/issues/42") {
			t.Errorf("URL path = %s, want to contain /repos/owner/repo/issues/42", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Issue{
			ID:     100,
			Number: 42,
			Title:  "Updated title",
			State:  "open",
		})
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	issue, err := client.UpdateIssue(ctx, 42, map[string]interface{}{
		"title": "Updated title",
	})
	if err != nil {
		t.Fatalf("UpdateIssue() error = %v", err)
	}

	if issue.Title != "Updated title" {
		t.Errorf("issue.Title = %q, want %q", issue.Title, "Updated title")
	}
}

// TestFetchIssueByNumber_Success verifies fetching a single issue.
func TestFetchIssueByNumber_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repos/owner/repo/issues/42") {
			t.Errorf("URL path = %s, want to contain /repos/owner/repo/issues/42", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Issue{
			ID:     100,
			Number: 42,
			Title:  "Test issue",
			State:  "open",
		})
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	issue, err := client.FetchIssueByNumber(ctx, 42)
	if err != nil {
		t.Fatalf("FetchIssueByNumber() error = %v", err)
	}

	if issue.Number != 42 {
		t.Errorf("issue.Number = %d, want 42", issue.Number)
	}
}

// TestFetchIssues_APIError verifies error handling for non-2xx responses.
func TestFetchIssues_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "Server error"}`))
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	_, err := client.FetchIssues(ctx, "open")
	if err == nil {
		t.Fatal("FetchIssues() error = nil, want error for 500")
	}
}

// TestFetchIssues_RateLimitRetry verifies rate limit handling with retry.
func TestFetchIssues_RateLimitRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Issue{{ID: 1, Number: 1, Title: "After retry"}})
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	issues, err := client.FetchIssues(ctx, "open")
	if err != nil {
		t.Fatalf("FetchIssues() error = %v, want success after retries", err)
	}

	if attempts < 3 {
		t.Errorf("attempts = %d, want >= 3 (initial + 2 retries)", attempts)
	}
	if len(issues) != 1 || issues[0].Title != "After retry" {
		t.Errorf("unexpected issues after retry: %v", issues)
	}
}

// TestHasNextPage verifies Link header parsing.
func TestHasNextPage(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		wantURL  string
		wantNext bool
	}{
		{
			name:     "has next page",
			link:     `<https://api.github.com/repos/o/r/issues?page=2>; rel="next", <https://api.github.com/repos/o/r/issues?page=5>; rel="last"`,
			wantURL:  "https://api.github.com/repos/o/r/issues?page=2",
			wantNext: true,
		},
		{
			name:     "no next page",
			link:     `<https://api.github.com/repos/o/r/issues?page=1>; rel="prev"`,
			wantURL:  "",
			wantNext: false,
		},
		{
			name:     "empty link header",
			link:     "",
			wantURL:  "",
			wantNext: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.link != "" {
				headers.Set("Link", tt.link)
			}
			gotURL, gotNext := hasNextPage(headers)
			if gotNext != tt.wantNext {
				t.Errorf("hasNextPage() next = %v, want %v", gotNext, tt.wantNext)
			}
			if gotURL != tt.wantURL {
				t.Errorf("hasNextPage() url = %q, want %q", gotURL, tt.wantURL)
			}
		})
	}
}

// TestFetchIssues_PaginationLimit verifies that FetchIssues stops after MaxPages.
func TestFetchIssues_PaginationLimit(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<http://example.com?page=999>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]Issue{{ID: requestCount, Number: requestCount, Title: "Issue"}})
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	_, err := client.FetchIssues(ctx, "all")

	if err == nil {
		t.Fatal("FetchIssues() error = nil, want pagination limit error")
	}
	if !strings.Contains(err.Error(), "pagination limit exceeded") {
		t.Errorf("error = %v, want to contain 'pagination limit exceeded'", err)
	}
	if requestCount > MaxPages+1 {
		t.Errorf("requestCount = %d, want <= %d (MaxPages+1)", requestCount, MaxPages+1)
	}
}

// TestFetchIssues_ContextCancellation verifies context cancellation stops pagination.
func TestFetchIssues_ContextCancellation(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<http://example.com?page=2>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]Issue{{ID: int(count), Number: int(count), Title: "Issue"}})
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := client.FetchIssues(ctx, "all")

	if err == nil {
		t.Fatal("FetchIssues() error = nil, want error to stop infinite loop")
	}

	isContextCanceled := err == context.Canceled || strings.Contains(err.Error(), "context canceled")
	isPaginationLimit := strings.Contains(err.Error(), "pagination limit exceeded")
	if !isContextCanceled && !isPaginationLimit {
		t.Errorf("error = %v, want context.Canceled or pagination limit exceeded", err)
	}
}

// TestCreateIssue_InvalidJSON verifies JSON parse error handling.
func TestCreateIssue_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	client := NewClient("token", "owner", "repo").WithBaseURL(server.URL)
	ctx := context.Background()

	_, err := client.CreateIssue(ctx, "Test", "Description", []string{})
	if err == nil {
		t.Fatal("CreateIssue() error = nil, want error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse create response") {
		t.Errorf("error = %v, want to contain 'failed to parse create response'", err)
	}
}
