//go:build integration
// +build integration

package importer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/deletions"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// TestConcurrentExternalRefUpdates tests concurrent updates to same external_ref with different timestamps
// This is a slow integration test that verifies no deadlocks occur
func TestConcurrentExternalRefUpdates(t *testing.T) {
	store, err := sqlite.New(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("Failed to set prefix: %v", err)
	}

	externalRef := "JIRA-200"
	existing := &types.Issue{
		ID:          "bd-1",
		Title:       "Existing issue",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		ExternalRef: &externalRef,
	}

	if err := store.CreateIssue(ctx, existing, "test"); err != nil {
		t.Fatalf("Failed to create existing issue: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]*Result, 3)
	done := make(chan bool, 1)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			
			updated := &types.Issue{
				ID:          "bd-import-" + string(rune('1'+idx)),
				Title:       "Updated from worker " + string(rune('A'+idx)),
				Status:      types.StatusInProgress,
				Priority:    2,
				IssueType:   types.TypeTask,
				ExternalRef: &externalRef,
				UpdatedAt:   time.Now().Add(time.Duration(idx) * time.Second),
			}

			result, _ := ImportIssues(ctx, "", store, []*types.Issue{updated}, Options{})
			results[idx] = result
		}(i)
	}

	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Test completed normally
	case <-time.After(30 * time.Second):
		t.Fatal("Test timed out after 30 seconds - likely deadlock in concurrent imports")
	}

	finalIssue, err := store.GetIssueByExternalRef(ctx, externalRef)
	if err != nil {
		t.Fatalf("Failed to get final issue: %v", err)
	}

	if finalIssue == nil {
		t.Fatal("Expected final issue to exist")
	}

	// Verify that we got the update with the latest timestamp (worker 2)
	if finalIssue.Title != "Updated from worker C" {
		t.Errorf("Expected last update to win, got title: %s", finalIssue.Title)
	}
}

// TestCrossCloneDeletionPropagation tests that deletions propagate across clones
// via the deletions manifest. Simulates:
// 1. Clone A and Clone B both have issue bd-test-123
// 2. Clone A deletes bd-test-123 (recorded in deletions.jsonl)
// 3. Clone B pulls and imports - issue should be purged from Clone B's DB
func TestCrossCloneDeletionPropagation(t *testing.T) {
	ctx := context.Background()

	// Create temp directory structure for "Clone B" (the clone that receives the deletion)
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create database in .beads/ (required for purgeDeletedIssues to find deletions.jsonl)
	dbPath := filepath.Join(beadsDir, "beads.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("Failed to set prefix: %v", err)
	}

	// Create an issue in Clone B's database (simulating it was synced before)
	issueToDelete := &types.Issue{
		ID:        "bd-test-123",
		Title:     "Issue that will be deleted in Clone A",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issueToDelete, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Also create another issue that should NOT be deleted
	issueToKeep := &types.Issue{
		ID:        "bd-test-456",
		Title:     "Issue that stays",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issueToKeep, "test"); err != nil {
		t.Fatalf("Failed to create kept issue: %v", err)
	}

	// Verify both issues exist
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("Expected 2 issues before import, got %d", len(issues))
	}

	// Simulate Clone A deleting bd-test-123 by writing to deletions manifest
	deletionsPath := filepath.Join(beadsDir, "deletions.jsonl")
	record := deletions.DeletionRecord{
		ID:        "bd-test-123",
		Timestamp: time.Now().UTC(),
		Actor:     "clone-a-user",
		Reason:    "test deletion",
	}
	if err := deletions.AppendDeletion(deletionsPath, record); err != nil {
		t.Fatalf("Failed to write deletion record: %v", err)
	}

	// Create JSONL with only the kept issue (simulating git pull from remote)
	// The deleted issue is NOT in the JSONL (it was removed in Clone A)
	jsonlIssues := []*types.Issue{issueToKeep}

	// Import with Options that uses the database path (triggers purgeDeletedIssues)
	result, err := ImportIssues(ctx, dbPath, store, jsonlIssues, Options{})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Verify the purge happened
	if result.Purged != 1 {
		t.Errorf("Expected 1 purged issue, got %d", result.Purged)
	}
	if len(result.PurgedIDs) != 1 || result.PurgedIDs[0] != "bd-test-123" {
		t.Errorf("Expected purged ID bd-test-123, got %v", result.PurgedIDs)
	}

	// Verify database state
	finalIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search final issues: %v", err)
	}

	if len(finalIssues) != 1 {
		t.Errorf("Expected 1 issue after import, got %d", len(finalIssues))
	}

	// The kept issue should still exist
	keptIssue, err := store.GetIssue(ctx, "bd-test-456")
	if err != nil {
		t.Fatalf("Failed to get kept issue: %v", err)
	}
	if keptIssue == nil {
		t.Error("Expected bd-test-456 to still exist")
	}

	// The deleted issue should be gone
	deletedIssue, err := store.GetIssue(ctx, "bd-test-123")
	if err != nil {
		t.Fatalf("Failed to query deleted issue: %v", err)
	}
	if deletedIssue != nil {
		t.Error("Expected bd-test-123 to be purged")
	}
}

// TestLocalUnpushedIssueNotDeleted verifies that local issues that were never
// in git are NOT deleted during import (they are local work, not deletions)
func TestLocalUnpushedIssueNotDeleted(t *testing.T) {
	ctx := context.Background()

	// Create temp directory structure
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("Failed to set prefix: %v", err)
	}

	// Create a local issue that was never exported/pushed
	localIssue := &types.Issue{
		ID:        "bd-local-work",
		Title:     "Local work in progress",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create local issue: %v", err)
	}

	// Create an issue that exists in JSONL (remote)
	remoteIssue := &types.Issue{
		ID:        "bd-remote-123",
		Title:     "Synced from remote",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, remoteIssue, "test"); err != nil {
		t.Fatalf("Failed to create remote issue: %v", err)
	}

	// Empty deletions manifest (no deletions)
	// Don't create the file - LoadDeletions handles missing file gracefully

	// JSONL only contains the remote issue (local issue was never exported)
	jsonlIssues := []*types.Issue{remoteIssue}

	// Import - local issue should NOT be purged
	result, err := ImportIssues(ctx, dbPath, store, jsonlIssues, Options{})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// No purges should happen (not in deletions manifest, not in git history)
	if result.Purged != 0 {
		t.Errorf("Expected 0 purged issues, got %d (purged: %v)", result.Purged, result.PurgedIDs)
	}

	// Both issues should still exist
	finalIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search final issues: %v", err)
	}

	if len(finalIssues) != 2 {
		t.Errorf("Expected 2 issues after import, got %d", len(finalIssues))
	}

	// Local work should still exist
	localFound, _ := store.GetIssue(ctx, "bd-local-work")
	if localFound == nil {
		t.Error("Local issue was incorrectly purged")
	}
}

// TestDeletionWithReason verifies that deletion reason is properly recorded
func TestDeletionWithReason(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("Failed to set prefix: %v", err)
	}

	// Create issue
	issue := &types.Issue{
		ID:        "bd-dup-001",
		Title:     "Duplicate issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Record deletion with reason "duplicate of bd-orig-001"
	deletionsPath := filepath.Join(beadsDir, "deletions.jsonl")
	record := deletions.DeletionRecord{
		ID:        "bd-dup-001",
		Timestamp: time.Now().UTC(),
		Actor:     "dedup-bot",
		Reason:    "duplicate of bd-orig-001",
	}
	if err := deletions.AppendDeletion(deletionsPath, record); err != nil {
		t.Fatalf("Failed to write deletion: %v", err)
	}

	// Verify record was written with reason
	loadResult, err := deletions.LoadDeletions(deletionsPath)
	if err != nil {
		t.Fatalf("Failed to load deletions: %v", err)
	}

	if loaded, ok := loadResult.Records["bd-dup-001"]; !ok {
		t.Error("Deletion record not found")
	} else {
		if loaded.Reason != "duplicate of bd-orig-001" {
			t.Errorf("Expected reason 'duplicate of bd-orig-001', got '%s'", loaded.Reason)
		}
		if loaded.Actor != "dedup-bot" {
			t.Errorf("Expected actor 'dedup-bot', got '%s'", loaded.Actor)
		}
	}

	// Import empty JSONL (issue was deleted)
	result, err := ImportIssues(ctx, dbPath, store, []*types.Issue{}, Options{})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if result.Purged != 1 {
		t.Errorf("Expected 1 purged, got %d", result.Purged)
	}
}
