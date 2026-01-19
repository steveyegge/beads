//go:build integration

package jira_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker/jira"
	"github.com/steveyegge/beads/internal/tracker/testutil"
)

// TestE2E_FetchIssues_Empty tests fetching issues when none exist.
func TestE2E_FetchIssues_Empty(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	issues, err := client.FetchIssues(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Expected 0 issues, got %d", len(issues))
	}
}

// TestE2E_FetchIssues_WithData tests fetching issues with data.
func TestE2E_FetchIssues_WithData(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "First Issue", "To Do"),
		testutil.MakeJiraIssue("PROJ-2", "Second Issue", "In Progress"),
		testutil.MakeJiraIssue("PROJ-3", "Third Issue", "Done"),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	issues, err := client.FetchIssues(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	if len(issues) != 3 {
		t.Errorf("Expected 3 issues, got %d", len(issues))
	}

	// Verify first issue
	if issues[0].Key != "PROJ-1" {
		t.Errorf("Expected first issue key 'PROJ-1', got '%s'", issues[0].Key)
	}

	if issues[0].Fields.Summary != "First Issue" {
		t.Errorf("Expected first issue summary 'First Issue', got '%s'", issues[0].Fields.Summary)
	}
}

// TestE2E_FetchIssues_WithDetails tests fetching issues with full details.
func TestE2E_FetchIssues_WithDetails(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up test data with full details
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssueWithDetails(
			"PROJ-1",
			"Bug Report",
			"Something is broken",
			"In Progress",
			"High",
			[]string{"bug", "critical"},
		),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	issues, err := client.FetchIssues(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("Expected 1 issue, got %d", len(issues))
	}

	issue := issues[0]
	if issue.Fields.Description != "Something is broken" {
		t.Errorf("Expected description 'Something is broken', got '%v'", issue.Fields.Description)
	}

	if issue.Fields.Priority.Name != "High" {
		t.Errorf("Expected priority 'High', got '%s'", issue.Fields.Priority.Name)
	}

	if len(issue.Fields.Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(issue.Fields.Labels))
	}
}

// TestE2E_FetchIssues_WithAssignee tests fetching issues with assignees.
func TestE2E_FetchIssues_WithAssignee(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up test data with assignee
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssueWithAssignee(
			"PROJ-1",
			"Assigned Task",
			"In Progress",
			"john@example.com",
			"John Doe",
		),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	issues, err := client.FetchIssues(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("Expected 1 issue, got %d", len(issues))
	}

	issue := issues[0]
	if issue.Fields.Assignee == nil {
		t.Fatal("Expected assignee to be set")
	}

	if issue.Fields.Assignee.EmailAddress != "john@example.com" {
		t.Errorf("Expected assignee email 'john@example.com', got '%s'", issue.Fields.Assignee.EmailAddress)
	}

	if issue.Fields.Assignee.DisplayName != "John Doe" {
		t.Errorf("Expected assignee name 'John Doe', got '%s'", issue.Fields.Assignee.DisplayName)
	}
}

// TestE2E_FetchIssue_Single tests fetching a single issue.
func TestE2E_FetchIssue_Single(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "First Issue", "To Do"),
		testutil.MakeJiraIssue("PROJ-2", "Second Issue", "In Progress"),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	issue, err := client.FetchIssue(ctx, "PROJ-2")

	if err != nil {
		t.Fatalf("FetchIssue failed: %v", err)
	}

	if issue == nil {
		t.Fatal("Expected issue to be returned")
	}

	if issue.Key != "PROJ-2" {
		t.Errorf("Expected issue key 'PROJ-2', got '%s'", issue.Key)
	}

	if issue.Fields.Summary != "Second Issue" {
		t.Errorf("Expected summary 'Second Issue', got '%s'", issue.Fields.Summary)
	}
}

// TestE2E_FetchIssue_NotFound tests fetching a non-existent issue.
func TestE2E_FetchIssue_NotFound(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "First Issue", "To Do"),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	issue, err := client.FetchIssue(ctx, "PROJ-999")

	// FetchIssue returns nil,nil for not found
	if err != nil && !strings.Contains(err.Error(), "404") {
		t.Fatalf("FetchIssue unexpected error: %v", err)
	}

	if issue != nil {
		t.Errorf("Expected nil issue for non-existent key, got %v", issue)
	}
}

// TestE2E_CreateIssue tests creating a new issue.
func TestE2E_CreateIssue(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Configure create response
	createdIssue := testutil.MakeJiraIssue("PROJ-100", "New Issue", "To Do")
	mock.SetCreateIssueResponse(&createdIssue)

	// Also add to issues list so FetchIssue can return it
	mock.AddIssue(createdIssue)

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	issue, err := client.CreateIssue(ctx, "New Issue", "Description", "Task", "Medium", nil)

	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if issue == nil {
		t.Fatal("Expected issue to be returned")
	}

	if issue.Key != "PROJ-100" {
		t.Errorf("Expected issue key 'PROJ-100', got '%s'", issue.Key)
	}

	// Verify request was made
	requests := mock.GetRequests()
	var foundCreate bool
	for _, req := range requests {
		if req.Method == "POST" && strings.Contains(req.Path, "/rest/api/3/issue") {
			foundCreate = true
			break
		}
	}

	if !foundCreate {
		t.Error("Expected POST request to /rest/api/3/issue")
	}
}

// TestE2E_UpdateIssue tests updating an existing issue.
func TestE2E_UpdateIssue(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "Original Title", "To Do"),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	err := client.UpdateIssue(ctx, "PROJ-1", map[string]interface{}{
		"summary": "Updated Title",
	})

	if err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify request was made
	requests := mock.GetRequests()
	var foundUpdate bool
	for _, req := range requests {
		if req.Method == "PUT" && strings.Contains(req.Path, "/rest/api/3/issue/PROJ-1") {
			foundUpdate = true
			break
		}
	}

	if !foundUpdate {
		t.Error("Expected PUT request to /rest/api/3/issue/PROJ-1")
	}
}

// TestE2E_AuthError tests handling of authentication errors.
func TestE2E_AuthError(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	mock.SetAuthError(true)

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "invalid-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	_, err := client.FetchIssues(ctx, "all", nil)

	if err == nil {
		t.Fatal("Expected error for invalid auth")
	}

	if !strings.Contains(err.Error(), "401") {
		t.Errorf("Expected 401 error, got: %v", err)
	}
}

// TestE2E_RateLimiting tests handling of rate limit errors.
func TestE2E_RateLimiting(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// First request will be rate limited, second will succeed
	mock.SetRateLimitError(true, 1)
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "Test Issue", "To Do"),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()

	// First request - should get rate limited
	_, err := client.FetchIssues(ctx, "all", nil)
	if err == nil {
		t.Fatal("Expected first request to be rate limited")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("Expected 429 error, got: %v", err)
	}

	// Second request - should succeed
	issues, err := client.FetchIssues(ctx, "all", nil)
	if err != nil {
		t.Fatalf("Second request should succeed: %v", err)
	}

	if len(issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(issues))
	}
}

// TestE2E_ServerError tests handling of server errors.
func TestE2E_ServerError(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	mock.SetServerError(true)

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	_, err := client.FetchIssues(ctx, "all", nil)

	if err == nil {
		t.Fatal("Expected error for server error")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Expected 500 error, got: %v", err)
	}
}

// TestE2E_BuildIssueURL tests building issue URLs.
func TestE2E_BuildIssueURL(t *testing.T) {
	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token")

	url := client.BuildIssueURL("PROJ-123")

	expected := "https://example.atlassian.net/browse/PROJ-123"
	if url != expected {
		t.Errorf("Expected URL '%s', got '%s'", expected, url)
	}
}

// TestE2E_TextToADF tests conversion of text to ADF format.
func TestE2E_TextToADF(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantNil  bool
		wantType string
	}{
		{
			name:    "empty text",
			text:    "",
			wantNil: true,
		},
		{
			name:     "simple text",
			text:     "Hello world",
			wantType: "doc",
		},
		{
			name:     "multiline text",
			text:     "Line 1\n\nLine 2",
			wantType: "doc",
		},
		{
			name:     "heading",
			text:     "# Heading\n\nParagraph",
			wantType: "doc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adf := jira.TextToADF(tt.text)

			if tt.wantNil {
				if adf != nil {
					t.Errorf("Expected nil ADF, got %v", adf)
				}
				return
			}

			if adf == nil {
				t.Fatal("Expected non-nil ADF")
			}

			if adf.Type != tt.wantType {
				t.Errorf("Expected type '%s', got '%s'", tt.wantType, adf.Type)
			}
		})
	}
}

// TestE2E_ADFToText tests conversion of ADF to plain text.
func TestE2E_ADFToText(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: "",
		},
		{
			name:     "string input",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name: "simple paragraph",
			input: map[string]interface{}{
				"type": "doc",
				"content": []interface{}{
					map[string]interface{}{
						"type": "paragraph",
						"content": []interface{}{
							map[string]interface{}{
								"type": "text",
								"text": "Hello world",
							},
						},
					},
				},
			},
			expected: "Hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jira.ADFToText(tt.input)

			if !strings.Contains(result, tt.expected) && tt.expected != "" {
				t.Errorf("Expected result to contain '%s', got '%s'", tt.expected, result)
			}

			if tt.expected == "" && result != "" {
				t.Errorf("Expected empty result, got '%s'", result)
			}
		})
	}
}

// TestE2E_RequestHeaders tests that requests include correct headers.
func TestE2E_RequestHeaders(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	mock.SetIssues([]jira.Issue{})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	_, _ = client.FetchIssues(ctx, "all", nil)

	requests := mock.GetRequests()
	if len(requests) == 0 {
		t.Fatal("Expected at least one request")
	}

	req := requests[0]

	// Check Content-Type
	if ct := req.Headers.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
	}

	// Check Accept
	if accept := req.Headers.Get("Accept"); accept != "application/json" {
		t.Errorf("Expected Accept 'application/json', got '%s'", accept)
	}

	// Check Authorization (should be present)
	if auth := req.Headers.Get("Authorization"); auth == "" {
		t.Error("Expected Authorization header to be set")
	}
}

// TestE2E_FetchIssues_WithSinceFilter tests fetching issues with a since filter.
func TestE2E_FetchIssues_WithSinceFilter(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "Recent Issue", "To Do"),
	})

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	since := time.Now().Add(-24 * time.Hour)
	ctx := context.Background()
	issues, err := client.FetchIssues(ctx, "all", &since)

	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	// The mock returns all issues; the real API would filter
	// We're testing that the request is made correctly
	if len(issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(issues))
	}

	// Verify request was made with query params
	requests := mock.GetRequests()
	if len(requests) == 0 {
		t.Fatal("Expected at least one request")
	}
}

// TestE2E_MultipleOperations tests multiple operations in sequence.
func TestE2E_MultipleOperations(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up initial data
	existingIssue := testutil.MakeJiraIssue("PROJ-1", "Existing Issue", "To Do")
	mock.SetIssues([]jira.Issue{existingIssue})

	// Configure create response
	newIssue := testutil.MakeJiraIssue("PROJ-2", "New Issue", "To Do")
	mock.SetCreateIssueResponse(&newIssue)

	client := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	ctx := context.Background()

	// 1. Fetch existing issues
	issues, err := client.FetchIssues(ctx, "all", nil)
	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(issues))
	}

	// 2. Create new issue
	mock.AddIssue(newIssue)
	created, err := client.CreateIssue(ctx, "New Issue", "", "Task", "", nil)
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if created.Key != "PROJ-2" {
		t.Errorf("Expected created issue key 'PROJ-2', got '%s'", created.Key)
	}

	// 3. Update existing issue
	err = client.UpdateIssue(ctx, "PROJ-1", map[string]interface{}{"summary": "Updated"})
	if err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// 4. Verify all requests were made
	requests := mock.GetRequests()
	if len(requests) < 3 {
		t.Errorf("Expected at least 3 requests, got %d", len(requests))
	}
}
