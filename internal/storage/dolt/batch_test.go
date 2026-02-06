package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestBatchIN_EmptyIDs(t *testing.T) {
	// BatchIN with empty input should return an empty map without touching the DB
	result, err := BatchIN(context.Background(), nil, nil, DefaultBatchSize,
		"SELECT x FROM t WHERE id IN (%s)",
		func(rows *sql.Rows) (string, string, error) {
			t.Fatal("scanRow should not be called for empty input")
			return "", "", nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(result))
	}
}

func TestBatchIN_Integration(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create test issues with comments
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			ID:        fmt.Sprintf("test-%d", i),
			Title:     fmt.Sprintf("Test issue %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
		if _, err := store.AddIssueComment(ctx, issue.ID, "tester", fmt.Sprintf("comment on %d", i)); err != nil {
			t.Fatalf("failed to add comment: %v", err)
		}
	}

	// Test GetCommentsForIssues (uses BatchIN internally)
	comments, err := store.GetCommentsForIssues(ctx, []string{"test-0", "test-1", "test-2"})
	if err != nil {
		t.Fatalf("GetCommentsForIssues failed: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("expected 3 issue entries, got %d", len(comments))
	}
	for _, id := range []string{"test-0", "test-1", "test-2"} {
		if len(comments[id]) != 1 {
			t.Errorf("expected 1 comment for %s, got %d", id, len(comments[id]))
		}
	}
}

func TestBatchIN_BatchSizeBoundary(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create enough issues to force multiple batches with a small batch size
	issueIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("batch-%d", i)
		issueIDs[i] = id
		issue := &types.Issue{
			ID:        id,
			Title:     fmt.Sprintf("Batch test %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
		if err := store.AddLabel(ctx, id, "test-label", "tester"); err != nil {
			t.Fatalf("failed to add label: %v", err)
		}
	}

	// Use batch size of 2 to force 3 batches (2+2+1)
	result, err := BatchIN(ctx, store.db, issueIDs, 2,
		`SELECT issue_id, label FROM labels WHERE issue_id IN (%s) ORDER BY issue_id, label`,
		func(rows *sql.Rows) (string, string, error) {
			var issueID, label string
			err := rows.Scan(&issueID, &label)
			return issueID, label, err
		},
	)
	if err != nil {
		t.Fatalf("BatchIN failed: %v", err)
	}
	if len(result) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(result))
	}
	for _, id := range issueIDs {
		if len(result[id]) != 1 || result[id][0] != "test-label" {
			t.Errorf("expected [test-label] for %s, got %v", id, result[id])
		}
	}
}
