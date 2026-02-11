// Package github provides client and data types for the GitHub REST API.
package github

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestDefaultMappingConfig verifies the default configuration has proper mappings.
func TestDefaultMappingConfig(t *testing.T) {
	config := DefaultMappingConfig()

	if len(config.PriorityMap) == 0 {
		t.Error("PriorityMap is empty, want default priority mappings")
	}
	if p, ok := config.PriorityMap["high"]; !ok || p != 1 {
		t.Errorf("PriorityMap[\"high\"] = %d, want 1", p)
	}

	if len(config.StateMap) == 0 {
		t.Error("StateMap is empty, want default state mappings")
	}
	// GitHub uses "open" not "opened"
	if s, ok := config.StateMap["open"]; !ok || s != "open" {
		t.Errorf("StateMap[\"open\"] = %q, want \"open\"", s)
	}

	if len(config.LabelTypeMap) == 0 {
		t.Error("LabelTypeMap is empty, want default type mappings")
	}
	if typ, ok := config.LabelTypeMap["bug"]; !ok || typ != "bug" {
		t.Errorf("LabelTypeMap[\"bug\"] = %q, want \"bug\"", typ)
	}
}

// TestPriorityFromLabels verifies parsing priority labels.
func TestPriorityFromLabels(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		name         string
		labels       []Label
		wantPriority int
	}{
		{
			name:         "priority:critical",
			labels:       []Label{{Name: "priority:critical"}, {Name: "bug"}},
			wantPriority: 0,
		},
		{
			name:         "priority:high",
			labels:       []Label{{Name: "priority:high"}},
			wantPriority: 1,
		},
		{
			name:         "priority/medium (slash separator)",
			labels:       []Label{{Name: "priority/medium"}},
			wantPriority: 2,
		},
		{
			name:         "P0 shorthand",
			labels:       []Label{{Name: "P0"}},
			wantPriority: 0,
		},
		{
			name:         "P1 shorthand",
			labels:       []Label{{Name: "P1"}},
			wantPriority: 1,
		},
		{
			name:         "P3 shorthand",
			labels:       []Label{{Name: "p3"}},
			wantPriority: 3,
		},
		{
			name:         "no priority label defaults to medium",
			labels:       []Label{{Name: "bug"}, {Name: "backend"}},
			wantPriority: 2,
		},
		{
			name:         "empty labels defaults to medium",
			labels:       []Label{},
			wantPriority: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PriorityFromLabels(tt.labels, config)
			if got != tt.wantPriority {
				t.Errorf("PriorityFromLabels() = %d, want %d", got, tt.wantPriority)
			}
		})
	}
}

// TestStatusFromLabelsAndState verifies status determination.
func TestStatusFromLabelsAndState(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		name       string
		labels     []Label
		state      string
		wantStatus string
	}{
		{
			name:       "status:in_progress overrides open state",
			labels:     []Label{{Name: "status:in_progress"}},
			state:      "open",
			wantStatus: "in_progress",
		},
		{
			name:       "status:in-progress with hyphen",
			labels:     []Label{{Name: "status:in-progress"}},
			state:      "open",
			wantStatus: "in_progress",
		},
		{
			name:       "status:blocked",
			labels:     []Label{{Name: "status:blocked"}, {Name: "bug"}},
			state:      "open",
			wantStatus: "blocked",
		},
		{
			name:       "status:deferred",
			labels:     []Label{{Name: "status:deferred"}},
			state:      "open",
			wantStatus: "deferred",
		},
		{
			name:       "closed state wins over status labels",
			labels:     []Label{{Name: "status:in_progress"}},
			state:      "closed",
			wantStatus: "closed",
		},
		{
			name:       "open state without status label",
			labels:     []Label{{Name: "bug"}},
			state:      "open",
			wantStatus: "open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StatusFromLabelsAndState(tt.labels, tt.state, config)
			if got != tt.wantStatus {
				t.Errorf("StatusFromLabelsAndState() = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

// TestTypeFromLabels verifies parsing type labels.
func TestTypeFromLabels(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		name     string
		labels   []Label
		wantType string
	}{
		{
			name:     "type:bug",
			labels:   []Label{{Name: "type:bug"}, {Name: "priority:high"}},
			wantType: "bug",
		},
		{
			name:     "type:feature",
			labels:   []Label{{Name: "type:feature"}},
			wantType: "feature",
		},
		{
			name:     "type/task (slash separator)",
			labels:   []Label{{Name: "type/task"}},
			wantType: "task",
		},
		{
			name:     "bare bug label (no prefix)",
			labels:   []Label{{Name: "bug"}},
			wantType: "bug",
		},
		{
			name:     "enhancement maps to feature",
			labels:   []Label{{Name: "enhancement"}},
			wantType: "feature",
		},
		{
			name:     "no type label defaults to task",
			labels:   []Label{{Name: "priority:high"}},
			wantType: "task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TypeFromLabels(tt.labels, config)
			if got != tt.wantType {
				t.Errorf("TypeFromLabels() = %q, want %q", got, tt.wantType)
			}
		})
	}
}

// TestGitHubIssueToBeads verifies full conversion from GitHub Issue to beads Issue.
func TestGitHubIssueToBeads(t *testing.T) {
	config := DefaultMappingConfig()

	createdAt := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2024, 1, 16, 14, 0, 0, 0, time.UTC)

	ghIssue := &Issue{
		ID:        123456,
		Number:    42,
		Title:     "Fix authentication bug",
		Body:      "Users cannot log in with SSO",
		State:     "open",
		CreatedAt: &createdAt,
		UpdatedAt: &updatedAt,
		Labels: []Label{
			{Name: "type:bug"},
			{Name: "priority:high"},
			{Name: "status:in_progress"},
			{Name: "backend"},
		},
		Assignee: &User{
			ID:    101,
			Login: "jdoe",
			Name:  "John Doe",
		},
		User: &User{
			ID:    102,
			Login: "alice",
			Name:  "Alice Smith",
		},
		HTMLURL: "https://github.com/myorg/myrepo/issues/42",
	}

	conversion := GitHubIssueToBeads(ghIssue, "myorg", "myrepo", config)
	issue := conversion.Issue

	if issue.Title != "Fix authentication bug" {
		t.Errorf("Title = %q, want %q", issue.Title, "Fix authentication bug")
	}
	if issue.Description != "Users cannot log in with SSO" {
		t.Errorf("Description = %q, want %q", issue.Description, "Users cannot log in with SSO")
	}
	if issue.SourceSystem != "github:myorg/myrepo:42" {
		t.Errorf("SourceSystem = %q, want %q", issue.SourceSystem, "github:myorg/myrepo:42")
	}
	if issue.ExternalRef == nil || *issue.ExternalRef != "https://github.com/myorg/myrepo/issues/42" {
		t.Errorf("ExternalRef = %v, want %q", issue.ExternalRef, "https://github.com/myorg/myrepo/issues/42")
	}
	if issue.IssueType != "bug" {
		t.Errorf("IssueType = %q, want %q", issue.IssueType, "bug")
	}
	if issue.Priority != 1 {
		t.Errorf("Priority = %d, want 1 (high)", issue.Priority)
	}
	if issue.Status != "in_progress" {
		t.Errorf("Status = %q, want %q", issue.Status, "in_progress")
	}
	if issue.Assignee != "jdoe" {
		t.Errorf("Assignee = %q, want %q", issue.Assignee, "jdoe")
	}
	if !issue.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt = %v, want %v", issue.CreatedAt, createdAt)
	}
	if !issue.UpdatedAt.Equal(updatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", issue.UpdatedAt, updatedAt)
	}

	// Labels should only contain non-scoped labels
	hasBackend := false
	for _, l := range issue.Labels {
		if l == "backend" {
			hasBackend = true
		}
		if l == "type:bug" || l == "priority:high" || l == "status:in_progress" {
			t.Errorf("Labels should not contain scoped label %q", l)
		}
	}
	if !hasBackend {
		t.Error("Labels should contain 'backend'")
	}
}

// TestBeadsIssueToGitHubFields verifies conversion from beads Issue to GitHub fields.
func TestBeadsIssueToGitHubFields(t *testing.T) {
	config := DefaultMappingConfig()

	estimatedMinutes := 300
	issue := &types.Issue{
		Title:            "Feature request",
		Description:      "Add dark mode",
		IssueType:        "feature",
		Priority:         1,
		Status:           types.StatusInProgress,
		Labels:           []string{"frontend"},
		EstimatedMinutes: &estimatedMinutes,
	}

	fields := BeadsIssueToGitHubFields(issue, config)

	if fields["title"] != "Feature request" {
		t.Errorf("fields[title] = %v, want %q", fields["title"], "Feature request")
	}
	if fields["body"] != "Add dark mode" {
		t.Errorf("fields[body] = %v, want %q", fields["body"], "Add dark mode")
	}

	labels, ok := fields["labels"].([]string)
	if !ok {
		t.Fatal("fields[labels] is not []string")
	}

	hasType := false
	hasPriority := false
	hasStatus := false
	hasFrontend := false
	for _, l := range labels {
		if l == "type:feature" {
			hasType = true
		}
		if l == "priority:high" {
			hasPriority = true
		}
		if l == "status:in_progress" {
			hasStatus = true
		}
		if l == "frontend" {
			hasFrontend = true
		}
	}
	if !hasType {
		t.Errorf("labels missing type:feature, got %v", labels)
	}
	if !hasPriority {
		t.Errorf("labels missing priority:high, got %v", labels)
	}
	if !hasStatus {
		t.Errorf("labels missing status:in_progress, got %v", labels)
	}
	if !hasFrontend {
		t.Errorf("labels missing frontend, got %v", labels)
	}
}

// TestBeadsIssueToGitHubFields_Closed verifies state is set for closed issues.
func TestBeadsIssueToGitHubFields_Closed(t *testing.T) {
	config := DefaultMappingConfig()

	closedIssue := &types.Issue{
		Title:  "Completed task",
		Status: types.StatusClosed,
	}

	fields := BeadsIssueToGitHubFields(closedIssue, config)

	if fields["state"] != "closed" {
		t.Errorf("fields[state] = %v, want %q", fields["state"], "closed")
	}
}

// TestFilterNonScopedLabels verifies that scoped labels are removed.
func TestFilterNonScopedLabels(t *testing.T) {
	labels := []string{
		"type:bug",
		"priority:high",
		"status:in_progress",
		"backend",
		"needs-review",
		"urgent",
	}

	filtered := FilterNonScopedLabels(labels)

	expected := []string{"backend", "needs-review", "urgent"}
	if len(filtered) != len(expected) {
		t.Fatalf("FilterNonScopedLabels returned %d labels, want %d", len(filtered), len(expected))
	}

	for i, l := range filtered {
		if l != expected[i] {
			t.Errorf("filtered[%d] = %q, want %q", i, l, expected[i])
		}
	}
}

// TestPriorityToLabel verifies conversion from beads priority to label value.
func TestPriorityToLabel(t *testing.T) {
	tests := []struct {
		priority int
		want     string
	}{
		{0, "critical"},
		{1, "high"},
		{2, "medium"},
		{3, "low"},
		{4, "none"},
		{-1, "medium"},
		{5, "medium"},
	}

	for _, tt := range tests {
		got := priorityToLabel(tt.priority)
		if got != tt.want {
			t.Errorf("priorityToLabel(%d) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}

// TestMappingConfigUsesTypesConstants verifies DefaultMappingConfig uses
// the exported mapping constants from types.go.
func TestMappingConfigUsesTypesConstants(t *testing.T) {
	config := DefaultMappingConfig()

	if p := config.PriorityMap["critical"]; p != PriorityMapping["critical"] {
		t.Errorf("PriorityMap[critical] = %d, want %d", p, PriorityMapping["critical"])
	}
	if p := config.PriorityMap["high"]; p != PriorityMapping["high"] {
		t.Errorf("PriorityMap[high] = %d, want %d", p, PriorityMapping["high"])
	}

	if typ := config.LabelTypeMap["bug"]; typ != TypeMapping["bug"] {
		t.Errorf("LabelTypeMap[bug] = %q, want %q", typ, TypeMapping["bug"])
	}
	if typ := config.LabelTypeMap["enhancement"]; typ != TypeMapping["enhancement"] {
		t.Errorf("LabelTypeMap[enhancement] = %q, want %q", typ, TypeMapping["enhancement"])
	}
}
