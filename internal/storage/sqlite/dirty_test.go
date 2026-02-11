package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestMarkIssueDirty(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue first
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	err := store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Issue should already be marked dirty by CreateIssue
	dirtyIssues, err := store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssues failed: %v", err)
	}
	if len(dirtyIssues) != 1 {
		t.Fatalf("Expected 1 dirty issue, got %d", len(dirtyIssues))
	}
	if dirtyIssues[0] != issue.ID {
		t.Errorf("Expected dirty issue %s, got %s", issue.ID, dirtyIssues[0])
	}

	// Clear dirty issues
	err = store.ClearDirtyIssuesByID(ctx, []string{issue.ID})
	if err != nil {
		t.Fatalf("ClearDirtyIssuesByID failed: %v", err)
	}

	// Verify cleared
	dirtyIssues, err = store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssues failed: %v", err)
	}
	if len(dirtyIssues) != 0 {
		t.Errorf("Expected 0 dirty issues after clear, got %d", len(dirtyIssues))
	}

	// Mark it dirty again manually
	err = store.MarkIssueDirty(ctx, issue.ID)
	if err != nil {
		t.Fatalf("MarkIssueDirty failed: %v", err)
	}

	// Verify it's dirty again
	dirtyIssues, err = store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssues failed: %v", err)
	}
	if len(dirtyIssues) != 1 {
		t.Errorf("Expected 1 dirty issue after marking, got %d", len(dirtyIssues))
	}
}

func TestMarkIssuesDirty(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple issues
	var issueIDs []string
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			Title:     "Test issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		err := store.CreateIssue(ctx, issue, "test-user")
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		issueIDs = append(issueIDs, issue.ID)
	}

	// Clear all dirty issues
	err := store.ClearDirtyIssuesByID(ctx, issueIDs)
	if err != nil {
		t.Fatalf("ClearDirtyIssuesByID failed: %v", err)
	}

	// Mark multiple issues dirty at once
	err = store.MarkIssuesDirty(ctx, issueIDs)
	if err != nil {
		t.Fatalf("MarkIssuesDirty failed: %v", err)
	}

	// Verify all are dirty
	dirtyIssues, err := store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssues failed: %v", err)
	}
	if len(dirtyIssues) != 3 {
		t.Errorf("Expected 3 dirty issues, got %d", len(dirtyIssues))
	}
}

func TestMarkIssuesDirtyEmpty(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Mark empty slice - should not error
	err := store.MarkIssuesDirty(ctx, []string{})
	if err != nil {
		t.Errorf("MarkIssuesDirty with empty slice should not error: %v", err)
	}
}

func TestGetDirtyIssueCount(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Count should be 0 initially
	count, err := store.GetDirtyIssueCount(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssueCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 dirty issues, got %d", count)
	}

	// Create an issue
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Count should be 1 now
	count, err = store.GetDirtyIssueCount(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssueCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 dirty issue, got %d", count)
	}
}

func TestClearDirtyIssuesByID(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple issues
	var issueIDs []string
	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			Title:     "Test issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		err := store.CreateIssue(ctx, issue, "test-user")
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		issueIDs = append(issueIDs, issue.ID)
	}

	// Verify all are dirty
	count, err := store.GetDirtyIssueCount(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssueCount failed: %v", err)
	}
	if count != 5 {
		t.Errorf("Expected 5 dirty issues, got %d", count)
	}

	// Clear only the first 3
	err = store.ClearDirtyIssuesByID(ctx, issueIDs[:3])
	if err != nil {
		t.Fatalf("ClearDirtyIssuesByID failed: %v", err)
	}

	// Should have 2 remaining
	count, err = store.GetDirtyIssueCount(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssueCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 dirty issues remaining, got %d", count)
	}

	// Verify the correct ones remain
	dirtyIssues, err := store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssues failed: %v", err)
	}
	for _, id := range dirtyIssues {
		if id == issueIDs[0] || id == issueIDs[1] || id == issueIDs[2] {
			t.Errorf("Issue %s should have been cleared", id)
		}
	}
}

func TestClearDirtyIssuesByIDEmpty(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Clear empty slice - should not error
	err := store.ClearDirtyIssuesByID(ctx, []string{})
	if err != nil {
		t.Errorf("ClearDirtyIssuesByID with empty slice should not error: %v", err)
	}
}

func TestDirtyIssuesOrdering(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create issues with slight time delays to ensure ordering
	var issueIDs []string
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			Title:     "Test issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		err := store.CreateIssue(ctx, issue, "test-user")
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		issueIDs = append(issueIDs, issue.ID)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Get dirty issues - should be in order by marked_at (oldest first)
	dirtyIssues, err := store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssues failed: %v", err)
	}

	if len(dirtyIssues) != 3 {
		t.Fatalf("Expected 3 dirty issues, got %d", len(dirtyIssues))
	}

	// Verify order matches creation order
	for i, id := range issueIDs {
		if dirtyIssues[i] != id {
			t.Errorf("Expected issue %d to be %s, got %s", i, id, dirtyIssues[i])
		}
	}
}

func TestDirtyIssuesUpdateTimestamp(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	err := store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Mark dirty again - should update timestamp
	err = store.MarkIssueDirty(ctx, issue.ID)
	if err != nil {
		t.Fatalf("MarkIssueDirty failed: %v", err)
	}

	// Should still have only 1 dirty issue (ON CONFLICT DO UPDATE)
	count, err := store.GetDirtyIssueCount(ctx)
	if err != nil {
		t.Fatalf("GetDirtyIssueCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 dirty issue after re-marking, got %d", count)
	}
}
