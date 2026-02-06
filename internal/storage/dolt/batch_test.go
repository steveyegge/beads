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

func TestBatchExec_EmptyIDs(t *testing.T) {
	// BatchExec with empty input should be a no-op without touching the DB
	err := BatchExec(context.Background(), nil, nil, DefaultBatchSize,
		"DELETE FROM dirty_issues WHERE issue_id IN (%s)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBatchExec_ClearDirtyIssues(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create issues and mark them dirty
	issueIDs := make([]string, 7)
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("dirty-%d", i)
		issueIDs[i] = id
		issue := &types.Issue{
			ID:        id,
			Title:     fmt.Sprintf("Dirty test %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
		// CreateIssue marks dirty automatically via markDirty
	}

	// Verify all are dirty
	dirtyBefore, err := store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("failed to get dirty issues: %v", err)
	}
	if len(dirtyBefore) < 7 {
		t.Fatalf("expected at least 7 dirty issues, got %d", len(dirtyBefore))
	}

	// Clear with batch size 3 to force 3 batches (3+3+1)
	err = BatchExec(ctx, store.db, issueIDs, 3,
		"DELETE FROM dirty_issues WHERE issue_id IN (%s)")
	if err != nil {
		t.Fatalf("BatchExec failed: %v", err)
	}

	// Verify all are cleared
	dirtyAfter, err := store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("failed to get dirty issues after clear: %v", err)
	}
	for _, id := range issueIDs {
		for _, dirtyID := range dirtyAfter {
			if dirtyID == id {
				t.Errorf("issue %s should have been cleared from dirty list", id)
			}
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

func TestGetDependencyRecordsForIssues_Batched(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create 4 issues, add dependencies between them
	for i := 0; i < 4; i++ {
		issue := &types.Issue{
			ID:        fmt.Sprintf("dep-%d", i),
			Title:     fmt.Sprintf("Dep test %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}
	// dep-0 blocks dep-1, dep-2 blocks dep-3
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID: "dep-1", DependsOnID: "dep-0", Type: types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID: "dep-3", DependsOnID: "dep-2", Type: types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	records, err := store.GetDependencyRecordsForIssues(ctx, []string{"dep-1", "dep-3", "dep-0"})
	if err != nil {
		t.Fatalf("GetDependencyRecordsForIssues failed: %v", err)
	}
	if len(records["dep-1"]) != 1 || records["dep-1"][0].DependsOnID != "dep-0" {
		t.Errorf("expected dep-1 to depend on dep-0, got %v", records["dep-1"])
	}
	if len(records["dep-3"]) != 1 || records["dep-3"][0].DependsOnID != "dep-2" {
		t.Errorf("expected dep-3 to depend on dep-2, got %v", records["dep-3"])
	}
	if len(records["dep-0"]) != 0 {
		t.Errorf("expected dep-0 to have no dependencies, got %v", records["dep-0"])
	}
}

func TestGetDependencyCounts_Batched(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create 3 issues, dep-1 and dep-2 both block dep-0
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			ID:        fmt.Sprintf("cnt-%d", i),
			Title:     fmt.Sprintf("Count test %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}
	// cnt-0 depends on cnt-1 and cnt-2 (both block cnt-0)
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID: "cnt-0", DependsOnID: "cnt-1", Type: types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID: "cnt-0", DependsOnID: "cnt-2", Type: types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	counts, err := store.GetDependencyCounts(ctx, []string{"cnt-0", "cnt-1", "cnt-2"})
	if err != nil {
		t.Fatalf("GetDependencyCounts failed: %v", err)
	}
	// cnt-0 has 2 dependencies (blockers), 0 dependents
	if counts["cnt-0"].DependencyCount != 2 {
		t.Errorf("expected cnt-0 DependencyCount=2, got %d", counts["cnt-0"].DependencyCount)
	}
	if counts["cnt-0"].DependentCount != 0 {
		t.Errorf("expected cnt-0 DependentCount=0, got %d", counts["cnt-0"].DependentCount)
	}
	// cnt-1 has 0 dependencies, 1 dependent (cnt-0)
	if counts["cnt-1"].DependencyCount != 0 {
		t.Errorf("expected cnt-1 DependencyCount=0, got %d", counts["cnt-1"].DependencyCount)
	}
	if counts["cnt-1"].DependentCount != 1 {
		t.Errorf("expected cnt-1 DependentCount=1, got %d", counts["cnt-1"].DependentCount)
	}
}
