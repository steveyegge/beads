package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
)

func TestMarkSpecChangedBySpecIDs_UpdatesTimestamp(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create an issue with a spec_id
	issue := &types.Issue{
		Title:     "Implement login flow",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		SpecID:    "specs/login.md",
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record original updated_at
	original, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	originalUpdatedAt := original.UpdatedAt

	// Wait a moment so timestamps differ
	time.Sleep(10 * time.Millisecond)
	changedAt := time.Now().UTC().Truncate(time.Second)

	// Mark spec as changed
	affected, err := store.MarkSpecChangedBySpecIDs(ctx, []string{"specs/login.md"}, changedAt)
	if err != nil {
		t.Fatalf("MarkSpecChangedBySpecIDs failed: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 affected issue, got %d", affected)
	}

	// Fetch the issue again
	updated, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after mark failed: %v", err)
	}

	// Verify spec_changed_at is set
	if updated.SpecChangedAt == nil {
		t.Error("expected spec_changed_at to be set, got nil")
	} else if !updated.SpecChangedAt.Equal(changedAt) {
		t.Errorf("spec_changed_at = %v, want %v", *updated.SpecChangedAt, changedAt)
	}

	// Verify updated_at was also updated (the fix we're testing)
	// Compare Unix timestamps to avoid timezone issues
	if updated.UpdatedAt.Unix() < originalUpdatedAt.Unix() {
		t.Errorf("updated_at should be >= original: got %v (%d), original was %v (%d)",
			updated.UpdatedAt, updated.UpdatedAt.Unix(), originalUpdatedAt, originalUpdatedAt.Unix())
	}
	// Also verify it matches changedAt (the value we passed in)
	if updated.UpdatedAt.Unix() != changedAt.Unix() {
		t.Errorf("updated_at should equal changedAt: got %v, want %v",
			updated.UpdatedAt.Unix(), changedAt.Unix())
	}
}

func TestMarkSpecChangedBySpecIDs_NoMatch(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create an issue with a different spec_id
	issue := &types.Issue{
		Title:     "Unrelated task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		SpecID:    "specs/other.md",
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	changedAt := time.Now().UTC().Truncate(time.Second)

	// Mark a different spec as changed
	affected, err := store.MarkSpecChangedBySpecIDs(ctx, []string{"specs/login.md"}, changedAt)
	if err != nil {
		t.Fatalf("MarkSpecChangedBySpecIDs failed: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected 0 affected issues, got %d", affected)
	}

	// Verify issue is unchanged
	fetched, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if fetched.SpecChangedAt != nil {
		t.Error("expected spec_changed_at to be nil for unrelated issue")
	}
}

func TestMarkSpecChangedBySpecIDs_MultipleIssues(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create multiple issues linked to the same spec
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			Title:     "Task linked to login spec",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
			SpecID:    "specs/login.md",
		}
		if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
			t.Fatalf("CreateIssue %d failed: %v", i, err)
		}
	}

	changedAt := time.Now().UTC().Truncate(time.Second)

	// Mark spec as changed
	affected, err := store.MarkSpecChangedBySpecIDs(ctx, []string{"specs/login.md"}, changedAt)
	if err != nil {
		t.Fatalf("MarkSpecChangedBySpecIDs failed: %v", err)
	}
	if affected != 3 {
		t.Errorf("expected 3 affected issues, got %d", affected)
	}
}

func TestSpecRegistryUpsert(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)

	// Insert new entry
	entries := []spec.SpecRegistryEntry{
		{
			SpecID:        "specs/login.md",
			Path:          "specs/login.md",
			Title:         "Login Feature",
			SHA256:        "abc123",
			Mtime:         now,
			DiscoveredAt:  now,
			LastScannedAt: now,
		},
	}
	if err := store.UpsertSpecRegistry(ctx, entries); err != nil {
		t.Fatalf("UpsertSpecRegistry failed: %v", err)
	}

	// Verify inserted
	fetched, err := store.GetSpecRegistry(ctx, "specs/login.md")
	if err != nil {
		t.Fatalf("GetSpecRegistry failed: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected spec entry, got nil")
	}
	if fetched.Title != "Login Feature" {
		t.Errorf("Title = %q, want %q", fetched.Title, "Login Feature")
	}

	// Update the entry
	entries[0].Title = "Login Feature v2"
	entries[0].SHA256 = "def456"
	if err := store.UpsertSpecRegistry(ctx, entries); err != nil {
		t.Fatalf("UpsertSpecRegistry update failed: %v", err)
	}

	// Verify updated
	fetched, err = store.GetSpecRegistry(ctx, "specs/login.md")
	if err != nil {
		t.Fatalf("GetSpecRegistry after update failed: %v", err)
	}
	if fetched.Title != "Login Feature v2" {
		t.Errorf("Title after update = %q, want %q", fetched.Title, "Login Feature v2")
	}
	if fetched.SHA256 != "def456" {
		t.Errorf("SHA256 after update = %q, want %q", fetched.SHA256, "def456")
	}
}
