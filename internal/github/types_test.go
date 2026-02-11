// Package github provides client and data types for the GitHub REST API.
package github

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestIssueJSONUnmarshal verifies that GitHub API JSON responses
// can be correctly unmarshaled into our Issue type.
func TestIssueJSONUnmarshal(t *testing.T) {
	jsonData := `{
		"id": 123456,
		"number": 42,
		"title": "Fix authentication bug",
		"body": "Users cannot log in with SSO",
		"state": "open",
		"created_at": "2024-01-15T10:30:00Z",
		"updated_at": "2024-01-16T14:45:00Z",
		"closed_at": null,
		"labels": [
			{"id": 1, "name": "bug", "color": "d73a4a"},
			{"id": 2, "name": "priority:high", "color": "ff0000"}
		],
		"assignee": {
			"id": 101,
			"login": "jdoe",
			"name": "John Doe"
		},
		"user": {
			"id": 102,
			"login": "alice",
			"name": "Alice Smith"
		},
		"milestone": {
			"id": 5,
			"number": 1,
			"title": "Sprint 5"
		},
		"html_url": "https://github.com/owner/repo/issues/42"
	}`

	var issue Issue
	err := json.Unmarshal([]byte(jsonData), &issue)
	if err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	if issue.ID != 123456 {
		t.Errorf("ID = %d, want 123456", issue.ID)
	}
	if issue.Number != 42 {
		t.Errorf("Number = %d, want 42", issue.Number)
	}
	if issue.Title != "Fix authentication bug" {
		t.Errorf("Title = %q, want %q", issue.Title, "Fix authentication bug")
	}
	if issue.Body != "Users cannot log in with SSO" {
		t.Errorf("Body = %q, want %q", issue.Body, "Users cannot log in with SSO")
	}
	if issue.State != "open" {
		t.Errorf("State = %q, want %q", issue.State, "open")
	}
	if issue.HTMLURL != "https://github.com/owner/repo/issues/42" {
		t.Errorf("HTMLURL = %q, want %q", issue.HTMLURL, "https://github.com/owner/repo/issues/42")
	}

	if len(issue.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(issue.Labels))
	}
	if issue.Labels[0].Name != "bug" {
		t.Errorf("Labels[0].Name = %q, want %q", issue.Labels[0].Name, "bug")
	}

	if issue.Assignee == nil {
		t.Fatal("Assignee is nil, want non-nil")
	}
	if issue.Assignee.Login != "jdoe" {
		t.Errorf("Assignee.Login = %q, want %q", issue.Assignee.Login, "jdoe")
	}

	if issue.User == nil {
		t.Fatal("User is nil, want non-nil")
	}
	if issue.User.Login != "alice" {
		t.Errorf("User.Login = %q, want %q", issue.User.Login, "alice")
	}

	if issue.Milestone == nil {
		t.Fatal("Milestone is nil, want non-nil")
	}
	if issue.Milestone.Title != "Sprint 5" {
		t.Errorf("Milestone.Title = %q, want %q", issue.Milestone.Title, "Sprint 5")
	}

	if issue.PullRequest != nil {
		t.Error("PullRequest should be nil for a regular issue")
	}
}

// TestIssueWithPullRequest verifies PR detection.
func TestIssueWithPullRequest(t *testing.T) {
	jsonData := `{
		"id": 100,
		"number": 10,
		"title": "PR title",
		"state": "open",
		"pull_request": {
			"url": "https://api.github.com/repos/owner/repo/pulls/10"
		}
	}`

	var issue Issue
	err := json.Unmarshal([]byte(jsonData), &issue)
	if err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	if issue.PullRequest == nil {
		t.Error("PullRequest is nil, want non-nil for PR")
	}
}

// TestStateMapping verifies GitHub states are valid.
func TestStateMapping(t *testing.T) {
	tests := []struct {
		state     string
		wantValid bool
	}{
		{"open", true},
		{"closed", true},
		{"reopened", false}, // GitHub doesn't have "reopened" state
		{"invalid", false},
	}

	for _, tt := range tests {
		got := IsValidState(tt.state)
		if got != tt.wantValid {
			t.Errorf("IsValidState(%q) = %v, want %v", tt.state, got, tt.wantValid)
		}
	}
}

// TestParseLabelName verifies label prefix parsing.
func TestParseLabelName(t *testing.T) {
	tests := []struct {
		label      string
		wantPrefix string
		wantValue  string
	}{
		{"priority:high", "priority", "high"},
		{"status:in_progress", "status", "in_progress"},
		{"type:bug", "type", "bug"},
		{"priority/high", "priority", "high"},
		{"type/feature", "type", "feature"},
		{"simple-label", "", "simple-label"},
		{"bug", "", "bug"},
	}

	for _, tt := range tests {
		prefix, value := ParseLabelName(tt.label)
		if prefix != tt.wantPrefix {
			t.Errorf("ParseLabelName(%q) prefix = %q, want %q", tt.label, prefix, tt.wantPrefix)
		}
		if value != tt.wantValue {
			t.Errorf("ParseLabelName(%q) value = %q, want %q", tt.label, value, tt.wantValue)
		}
	}
}

// TestGetPriorityFromLabel verifies priority label value to beads priority mapping.
func TestGetPriorityFromLabel(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"critical", 0},
		{"CRITICAL", 0},
		{"high", 1},
		{"medium", 2},
		{"low", 3},
		{"none", 4},
		{"invalid", -1},
		{"", -1},
	}

	for _, tt := range tests {
		got := GetPriorityFromLabel(tt.value)
		if got != tt.want {
			t.Errorf("GetPriorityFromLabel(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

// TestGetStatusFromLabel verifies status label value to beads status mapping.
func TestGetStatusFromLabel(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"open", "open"},
		{"OPEN", "open"},
		{"in_progress", "in_progress"},
		{"blocked", "blocked"},
		{"deferred", "deferred"},
		{"closed", "closed"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		got := GetStatusFromLabel(tt.value)
		if got != tt.want {
			t.Errorf("GetStatusFromLabel(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

// TestGetTypeFromLabel verifies type label value to beads issue type mapping.
func TestGetTypeFromLabel(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"bug", "bug"},
		{"feature", "feature"},
		{"task", "task"},
		{"epic", "epic"},
		{"chore", "chore"},
		{"enhancement", "feature"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		got := GetTypeFromLabel(tt.value)
		if got != tt.want {
			t.Errorf("GetTypeFromLabel(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

// TestLabelNames verifies extracting names from Label structs.
func TestLabelNames(t *testing.T) {
	labels := []Label{
		{ID: 1, Name: "bug"},
		{ID: 2, Name: "priority:high"},
		{ID: 3, Name: "enhancement"},
	}

	names := LabelNames(labels)
	if len(names) != 3 {
		t.Fatalf("LabelNames returned %d names, want 3", len(names))
	}
	if names[0] != "bug" {
		t.Errorf("names[0] = %q, want %q", names[0], "bug")
	}
	if names[1] != "priority:high" {
		t.Errorf("names[1] = %q, want %q", names[1], "priority:high")
	}
}

// TestSyncStatsZeroValue verifies SyncStats initializes correctly.
func TestSyncStatsZeroValue(t *testing.T) {
	stats := SyncStats{}
	if stats.Pulled != 0 {
		t.Errorf("Pulled = %d, want 0", stats.Pulled)
	}
	if stats.Pushed != 0 {
		t.Errorf("Pushed = %d, want 0", stats.Pushed)
	}
}

// TestConflictFields verifies Conflict type has required fields.
func TestConflictFields(t *testing.T) {
	now := time.Now()
	conflict := Conflict{
		IssueID:           "bd-abc123",
		LocalUpdated:      now,
		GitHubUpdated:     now.Add(time.Hour),
		GitHubExternalRef: "https://github.com/owner/repo/issues/42",
		GitHubNumber:      42,
		GitHubID:          123456,
	}

	if conflict.IssueID != "bd-abc123" {
		t.Errorf("IssueID = %q, want %q", conflict.IssueID, "bd-abc123")
	}
	if conflict.GitHubNumber != 42 {
		t.Errorf("GitHubNumber = %d, want 42", conflict.GitHubNumber)
	}
}

// TestIssueConversion verifies IssueConversion struct field access.
func TestIssueConversion(t *testing.T) {
	conversion := &IssueConversion{
		Issue: &types.Issue{
			Title:       "Test issue",
			Description: "Test description",
		},
		Dependencies: []DependencyInfo{},
	}

	if conversion.Issue == nil {
		t.Fatal("Issue field is nil, want *types.Issue")
	}
	if conversion.Issue.Title != "Test issue" {
		t.Errorf("Issue.Title = %q, want %q", conversion.Issue.Title, "Test issue")
	}
}
