package api

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestSortClosedQueueIssuesOrdersByClosedTimestamp(t *testing.T) {
	now := time.Date(2025, 10, 30, 12, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{ID: "ui-02", ClosedAt: timePtr(now.Add(-5 * time.Minute))},
		{ID: "ops-7", ClosedAt: timePtr(now.Add(-2 * time.Minute))},
		{ID: "ui-11", ClosedAt: timePtr(now.Add(-1 * time.Minute))},
		{ID: "ui-03", ClosedAt: timePtr(now.Add(-3 * time.Minute))},
	}

	sortClosedQueueIssues(issues)

	expected := []string{"ui-11", "ops-7", "ui-03", "ui-02"}
	for i, issue := range issues {
		if issue == nil {
			t.Fatalf("issue at index %d is nil", i)
		}
		if issue.ID != expected[i] {
			t.Fatalf("expected id %q at index %d, got %q", expected[i], i, issue.ID)
		}
	}
}

func TestSortClosedQueueIssuesUsesIDFallback(t *testing.T) {
	now := time.Date(2025, 10, 30, 12, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{ID: "ui-04", ClosedAt: timePtr(now.Add(-10 * time.Minute))},
		{ID: "ui-02", ClosedAt: timePtr(now.Add(-10 * time.Minute))},
		{ID: "ui-03", ClosedAt: timePtr(now.Add(-10 * time.Minute))},
	}

	sortClosedQueueIssues(issues)

	expected := []string{"ui-04", "ui-03", "ui-02"}
	for i, issue := range issues {
		if issue == nil {
			t.Fatalf("issue at index %d is nil", i)
		}
		if issue.ID != expected[i] {
			t.Fatalf("expected id %q at index %d, got %q", expected[i], i, issue.ID)
		}
	}
}

func TestExtractNumericSuffixHandlesNonNumericIDs(t *testing.T) {
	if value, ok := extractNumericSuffix("abc"); ok || value != 0 {
		t.Fatalf("expected non-numeric id to return false, got (%d, %v)", value, ok)
	}
	if value, ok := extractNumericSuffix("abc123"); !ok || value != 123 {
		t.Fatalf("expected numeric suffix 123, got (%d, %v)", value, ok)
	}
}
