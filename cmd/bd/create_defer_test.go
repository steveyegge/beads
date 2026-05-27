package main

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestBuildCreateIssueDeferStatus(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	tests := []struct {
		name       string
		deferUntil *time.Time
		want       types.Status
	}{
		{"future defer is deferred", &future, types.StatusDeferred},
		{"past defer stays open", &past, types.StatusOpen},
		{"no defer stays open", nil, types.StatusOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := buildCreateIssue(createIssueParams{
				Title:      "defer create",
				Priority:   2,
				IssueType:  types.TypeTask,
				DeferUntil: tt.deferUntil,
			})
			if issue.Status != tt.want {
				t.Fatalf("status = %q, want %q", issue.Status, tt.want)
			}
		})
	}
}
