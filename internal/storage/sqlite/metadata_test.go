package sqlite

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestIssueMetadataRoundTrip verifies that issue metadata can be created, retrieved,
// searched, and updated correctly (GH#1406).
func TestIssueMetadataRoundTrip(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("create and get issue with metadata", func(t *testing.T) {
		metadata := json.RawMessage(`{"files":["a.go","b.go"],"tool":"linter"}`)
		issue := &types.Issue{
			Title:     "Issue with metadata",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			Metadata:  metadata,
		}

		if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		// Verify GetIssue returns metadata
		got, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if string(got.Metadata) != string(metadata) {
			t.Errorf("GetIssue metadata = %q, want %q", got.Metadata, metadata)
		}
	})

	t.Run("search returns issues with metadata", func(t *testing.T) {
		metadata := json.RawMessage(`{"files":["search.go"]}`)
		issue := &types.Issue{
			Title:     "Searchable issue with metadata",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			Metadata:  metadata,
		}

		if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		// Search for the issue
		results, err := store.SearchIssues(ctx, "Searchable", types.IssueFilter{})
		if err != nil {
			t.Fatalf("SearchIssues failed: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("SearchIssues returned no results")
		}

		found := false
		for _, r := range results {
			if r.ID == issue.ID {
				found = true
				if string(r.Metadata) != string(metadata) {
					t.Errorf("SearchIssues metadata = %q, want %q", r.Metadata, metadata)
				}
				break
			}
		}
		if !found {
			t.Error("SearchIssues did not return the expected issue")
		}
	})

	t.Run("update issue metadata", func(t *testing.T) {
		issue := &types.Issue{
			Title:     "Issue to update metadata",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			Metadata:  json.RawMessage(`{"version":1}`),
		}

		if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		// Update metadata
		newMetadata := `{"version":2,"updated":true}`
		if err := store.UpdateIssue(ctx, issue.ID, map[string]interface{}{
			"metadata": newMetadata,
		}, "test-user"); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}

		// Verify update
		got, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if string(got.Metadata) != newMetadata {
			t.Errorf("Updated metadata = %q, want %q", got.Metadata, newMetadata)
		}
	})

	t.Run("issue without metadata has nil Metadata field", func(t *testing.T) {
		issue := &types.Issue{
			Title:     "Issue without metadata",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}

		if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		got, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if got.Metadata != nil {
			t.Errorf("Expected nil Metadata for issue without metadata, got %q", got.Metadata)
		}
	})
}
