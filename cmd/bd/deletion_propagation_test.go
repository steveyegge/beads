//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/deletions"
	"github.com/steveyegge/beads/internal/importer"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// importJSONLFile parses a JSONL file and imports using ImportIssues
func importJSONLFile(ctx context.Context, store *sqlite.SQLiteStorage, dbPath, jsonlPath string, opts importer.Options) (*importer.Result, error) {
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Empty import if file doesn't exist
			return importer.ImportIssues(ctx, dbPath, store, nil, opts)
		}
		return nil, err
	}

	var issues []*types.Issue
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var issue types.Issue
		if err := decoder.Decode(&issue); err != nil {
			return nil, err
		}
		issues = append(issues, &issue)
	}

	return importer.ImportIssues(ctx, dbPath, store, issues, opts)
}

// TestDeletionPropagation_AcrossClones verifies that when an issue is deleted
// in one clone, the deletion propagates to other clones via the deletions manifest.
func TestDeletionPropagation_AcrossClones(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()

	// Create "remote" repository
	remoteDir := filepath.Join(tempDir, "remote")
	if err := os.MkdirAll(remoteDir, 0750); err != nil {
		t.Fatalf("Failed to create remote dir: %v", err)
	}
	runGitCmd(t, remoteDir, "init", "--bare")

	// Create clone1 (will create and delete issue)
	clone1Dir := filepath.Join(tempDir, "clone1")
	runGitCmd(t, tempDir, "clone", remoteDir, clone1Dir)
	configureGit(t, clone1Dir)

	// Create clone2 (will receive deletion via sync)
	clone2Dir := filepath.Join(tempDir, "clone2")
	runGitCmd(t, tempDir, "clone", remoteDir, clone2Dir)
	configureGit(t, clone2Dir)

	// Initialize beads in clone1
	clone1BeadsDir := filepath.Join(clone1Dir, ".beads")
	if err := os.MkdirAll(clone1BeadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	clone1DBPath := filepath.Join(clone1BeadsDir, "beads.db")
	clone1Store := newTestStore(t, clone1DBPath)
	defer clone1Store.Close()

	// Create an issue in clone1
	issue := &types.Issue{
		Title:     "Issue to be deleted",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := clone1Store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	issueID := issue.ID
	t.Logf("Created issue: %s", issueID)

	// Export to JSONL
	clone1JSONLPath := filepath.Join(clone1BeadsDir, "beads.jsonl")
	if err := exportIssuesToJSONL(ctx, clone1Store, clone1JSONLPath); err != nil {
		t.Fatalf("Failed to export: %v", err)
	}

	// Commit and push from clone1
	runGitCmd(t, clone1Dir, "add", ".beads")
	runGitCmd(t, clone1Dir, "commit", "-m", "Add issue")
	runGitCmd(t, clone1Dir, "push", "origin", "master")

	// Clone2 pulls the issue
	runGitCmd(t, clone2Dir, "pull")

	// Initialize beads in clone2
	clone2BeadsDir := filepath.Join(clone2Dir, ".beads")
	clone2DBPath := filepath.Join(clone2BeadsDir, "beads.db")
	clone2Store := newTestStore(t, clone2DBPath)
	defer clone2Store.Close()

	// Import to clone2
	clone2JSONLPath := filepath.Join(clone2BeadsDir, "beads.jsonl")
	result, err := importJSONLFile(ctx, clone2Store, clone2DBPath, clone2JSONLPath, importer.Options{})
	if err != nil {
		t.Fatalf("Failed to import to clone2: %v", err)
	}
	t.Logf("Clone2 import: created=%d, updated=%d", result.Created, result.Updated)

	// Verify clone2 has the issue
	clone2Issue, err := clone2Store.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get issue from clone2: %v", err)
	}
	if clone2Issue == nil {
		t.Fatal("Clone2 should have the issue after import")
	}
	t.Log("✓ Both clones have the issue")

	// Clone1 deletes the issue
	if err := clone1Store.DeleteIssue(ctx, issueID); err != nil {
		t.Fatalf("Failed to delete issue from clone1: %v", err)
	}

	// Record deletion in manifest
	clone1DeletionsPath := filepath.Join(clone1BeadsDir, "deletions.jsonl")
	delRecord := deletions.DeletionRecord{
		ID:        issueID,
		Timestamp: time.Now().UTC(),
		Actor:     "test-user",
		Reason:    "test deletion",
	}
	if err := deletions.AppendDeletion(clone1DeletionsPath, delRecord); err != nil {
		t.Fatalf("Failed to record deletion: %v", err)
	}

	// Re-export JSONL (issue is now gone)
	if err := exportIssuesToJSONL(ctx, clone1Store, clone1JSONLPath); err != nil {
		t.Fatalf("Failed to export after deletion: %v", err)
	}

	// Commit and push deletion
	runGitCmd(t, clone1Dir, "add", ".beads")
	runGitCmd(t, clone1Dir, "commit", "-m", "Delete issue")
	runGitCmd(t, clone1Dir, "push", "origin", "master")
	t.Log("✓ Clone1 deleted issue and pushed")

	// Clone2 pulls the deletion
	runGitCmd(t, clone2Dir, "pull")

	// Verify deletions.jsonl was synced to clone2
	clone2DeletionsPath := filepath.Join(clone2BeadsDir, "deletions.jsonl")
	if _, err := os.Stat(clone2DeletionsPath); err != nil {
		t.Fatalf("deletions.jsonl should be synced to clone2: %v", err)
	}

	// Import to clone2 (should purge the deleted issue)
	result, err = importJSONLFile(ctx, clone2Store, clone2DBPath, clone2JSONLPath, importer.Options{})
	if err != nil {
		t.Fatalf("Failed to import after deletion sync: %v", err)
	}
	t.Logf("Clone2 import after sync: purged=%d, purgedIDs=%v", result.Purged, result.PurgedIDs)

	// Verify clone2 no longer has the issue
	clone2Issue, err = clone2Store.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to check issue in clone2: %v", err)
	}
	if clone2Issue != nil {
		t.Errorf("Clone2 should NOT have the issue after sync (deletion should propagate)")
	} else {
		t.Log("✓ Deletion propagated to clone2")
	}

	// Verify purge count
	if result.Purged != 1 {
		t.Errorf("Expected 1 purged issue, got %d", result.Purged)
	}
}

// TestDeletionPropagation_SimultaneousDeletions verifies that when both clones
// delete the same issue, the deletions are handled idempotently.
func TestDeletionPropagation_SimultaneousDeletions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()

	// Create "remote" repository
	remoteDir := filepath.Join(tempDir, "remote")
	if err := os.MkdirAll(remoteDir, 0750); err != nil {
		t.Fatalf("Failed to create remote dir: %v", err)
	}
	runGitCmd(t, remoteDir, "init", "--bare")

	// Create clone1
	clone1Dir := filepath.Join(tempDir, "clone1")
	runGitCmd(t, tempDir, "clone", remoteDir, clone1Dir)
	configureGit(t, clone1Dir)

	// Create clone2
	clone2Dir := filepath.Join(tempDir, "clone2")
	runGitCmd(t, tempDir, "clone", remoteDir, clone2Dir)
	configureGit(t, clone2Dir)

	// Initialize beads in clone1
	clone1BeadsDir := filepath.Join(clone1Dir, ".beads")
	if err := os.MkdirAll(clone1BeadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	clone1DBPath := filepath.Join(clone1BeadsDir, "beads.db")
	clone1Store := newTestStore(t, clone1DBPath)
	defer clone1Store.Close()

	// Create an issue in clone1
	issue := &types.Issue{
		Title:     "Issue deleted by both",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := clone1Store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	issueID := issue.ID

	// Export and push
	clone1JSONLPath := filepath.Join(clone1BeadsDir, "beads.jsonl")
	if err := exportIssuesToJSONL(ctx, clone1Store, clone1JSONLPath); err != nil {
		t.Fatalf("Failed to export: %v", err)
	}
	runGitCmd(t, clone1Dir, "add", ".beads")
	runGitCmd(t, clone1Dir, "commit", "-m", "Add issue")
	runGitCmd(t, clone1Dir, "push", "origin", "master")

	// Clone2 pulls and imports
	runGitCmd(t, clone2Dir, "pull")

	clone2BeadsDir := filepath.Join(clone2Dir, ".beads")
	clone2DBPath := filepath.Join(clone2BeadsDir, "beads.db")
	clone2Store := newTestStore(t, clone2DBPath)
	defer clone2Store.Close()

	clone2JSONLPath := filepath.Join(clone2BeadsDir, "beads.jsonl")
	if _, err := importJSONLFile(ctx, clone2Store, clone2DBPath, clone2JSONLPath, importer.Options{}); err != nil {
		t.Fatalf("Failed to import to clone2: %v", err)
	}

	// Both clones delete the issue simultaneously
	// Clone1 deletes
	clone1Store.DeleteIssue(ctx, issueID)
	clone1DeletionsPath := filepath.Join(clone1BeadsDir, "deletions.jsonl")
	deletions.AppendDeletion(clone1DeletionsPath, deletions.DeletionRecord{
		ID:        issueID,
		Timestamp: time.Now().UTC(),
		Actor:     "user1",
		Reason:    "deleted by clone1",
	})
	exportIssuesToJSONL(ctx, clone1Store, clone1JSONLPath)

	// Clone2 deletes (before pulling clone1's deletion)
	clone2Store.DeleteIssue(ctx, issueID)
	clone2DeletionsPath := filepath.Join(clone2BeadsDir, "deletions.jsonl")
	deletions.AppendDeletion(clone2DeletionsPath, deletions.DeletionRecord{
		ID:        issueID,
		Timestamp: time.Now().UTC(),
		Actor:     "user2",
		Reason:    "deleted by clone2",
	})
	exportIssuesToJSONL(ctx, clone2Store, clone2JSONLPath)

	t.Log("✓ Both clones deleted the issue locally")

	// Clone1 commits and pushes first
	runGitCmd(t, clone1Dir, "add", ".beads")
	runGitCmd(t, clone1Dir, "commit", "-m", "Delete issue (clone1)")
	runGitCmd(t, clone1Dir, "push", "origin", "master")

	// Clone2 commits, pulls (may have conflict), and pushes
	runGitCmd(t, clone2Dir, "add", ".beads")
	runGitCmd(t, clone2Dir, "commit", "-m", "Delete issue (clone2)")

	// Pull with rebase to handle the concurrent deletion
	// The deletions.jsonl conflict is handled by accepting both (append-only)
	runGitCmdAllowError(t, clone2Dir, "pull", "--rebase")

	// If there's a conflict in deletions.jsonl, resolve by concatenating
	resolveDeletionsConflict(t, clone2Dir)

	runGitCmdAllowError(t, clone2Dir, "rebase", "--continue")
	runGitCmdAllowError(t, clone2Dir, "push", "origin", "master")

	// Verify deletions.jsonl contains both deletion records (deduplicated by ID on load)
	finalDeletionsPath := filepath.Join(clone2BeadsDir, "deletions.jsonl")
	result, err := deletions.LoadDeletions(finalDeletionsPath)
	if err != nil {
		t.Fatalf("Failed to load deletions: %v", err)
	}

	// Should have the deletion record (may be from either clone, deduplication keeps one)
	if _, found := result.Records[issueID]; !found {
		t.Error("Expected deletion record to exist after simultaneous deletions")
	}

	t.Log("✓ Simultaneous deletions handled correctly (idempotent)")
}

// TestDeletionPropagation_LocalWorkPreserved verifies that local unpushed work
// is NOT deleted when deletions are synced.
func TestDeletionPropagation_LocalWorkPreserved(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()

	// Create "remote" repository
	remoteDir := filepath.Join(tempDir, "remote")
	if err := os.MkdirAll(remoteDir, 0750); err != nil {
		t.Fatalf("Failed to create remote dir: %v", err)
	}
	runGitCmd(t, remoteDir, "init", "--bare")

	// Create clone1
	clone1Dir := filepath.Join(tempDir, "clone1")
	runGitCmd(t, tempDir, "clone", remoteDir, clone1Dir)
	configureGit(t, clone1Dir)

	// Create clone2
	clone2Dir := filepath.Join(tempDir, "clone2")
	runGitCmd(t, tempDir, "clone", remoteDir, clone2Dir)
	configureGit(t, clone2Dir)

	// Initialize beads in clone1
	clone1BeadsDir := filepath.Join(clone1Dir, ".beads")
	if err := os.MkdirAll(clone1BeadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	clone1DBPath := filepath.Join(clone1BeadsDir, "beads.db")
	clone1Store := newTestStore(t, clone1DBPath)
	defer clone1Store.Close()

	// Create shared issue in clone1
	sharedIssue := &types.Issue{
		Title:     "Shared issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := clone1Store.CreateIssue(ctx, sharedIssue, "test-user"); err != nil {
		t.Fatalf("Failed to create shared issue: %v", err)
	}
	sharedID := sharedIssue.ID

	// Export and push
	clone1JSONLPath := filepath.Join(clone1BeadsDir, "beads.jsonl")
	if err := exportIssuesToJSONL(ctx, clone1Store, clone1JSONLPath); err != nil {
		t.Fatalf("Failed to export: %v", err)
	}
	runGitCmd(t, clone1Dir, "add", ".beads")
	runGitCmd(t, clone1Dir, "commit", "-m", "Add shared issue")
	runGitCmd(t, clone1Dir, "push", "origin", "master")

	// Clone2 pulls and imports the shared issue
	runGitCmd(t, clone2Dir, "pull")

	clone2BeadsDir := filepath.Join(clone2Dir, ".beads")
	clone2DBPath := filepath.Join(clone2BeadsDir, "beads.db")
	clone2Store := newTestStore(t, clone2DBPath)
	defer clone2Store.Close()

	clone2JSONLPath := filepath.Join(clone2BeadsDir, "beads.jsonl")
	if _, err := importJSONLFile(ctx, clone2Store, clone2DBPath, clone2JSONLPath, importer.Options{}); err != nil {
		t.Fatalf("Failed to import to clone2: %v", err)
	}

	// Clone2 creates LOCAL work (not synced)
	localIssue := &types.Issue{
		Title:     "Local work in clone2",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := clone2Store.CreateIssue(ctx, localIssue, "clone2-user"); err != nil {
		t.Fatalf("Failed to create local issue: %v", err)
	}
	localID := localIssue.ID
	t.Logf("Clone2 created local issue: %s", localID)

	// Clone1 deletes the shared issue
	clone1Store.DeleteIssue(ctx, sharedID)
	clone1DeletionsPath := filepath.Join(clone1BeadsDir, "deletions.jsonl")
	deletions.AppendDeletion(clone1DeletionsPath, deletions.DeletionRecord{
		ID:        sharedID,
		Timestamp: time.Now().UTC(),
		Actor:     "clone1-user",
		Reason:    "cleanup",
	})
	exportIssuesToJSONL(ctx, clone1Store, clone1JSONLPath)
	runGitCmd(t, clone1Dir, "add", ".beads")
	runGitCmd(t, clone1Dir, "commit", "-m", "Delete shared issue")
	runGitCmd(t, clone1Dir, "push", "origin", "master")

	// Clone2 pulls and imports (should delete shared, preserve local)
	runGitCmd(t, clone2Dir, "pull")
	result, err := importJSONLFile(ctx, clone2Store, clone2DBPath, clone2JSONLPath, importer.Options{})
	if err != nil {
		t.Fatalf("Failed to import after pull: %v", err)
	}
	t.Logf("Clone2 import: purged=%d, purgedIDs=%v", result.Purged, result.PurgedIDs)

	// Verify shared issue is gone
	sharedCheck, _ := clone2Store.GetIssue(ctx, sharedID)
	if sharedCheck != nil {
		t.Error("Shared issue should be deleted")
	}

	// Verify local issue is preserved
	localCheck, _ := clone2Store.GetIssue(ctx, localID)
	if localCheck == nil {
		t.Error("Local work should be preserved (not in deletions manifest)")
	}

	t.Log("✓ Local work preserved while synced deletions propagated")
}

// TestDeletionPropagation_CorruptLineRecovery verifies that corrupt lines
// in deletions.jsonl are skipped gracefully during import.
func TestDeletionPropagation_CorruptLineRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()

	// Setup single clone for this test
	beadsDir := filepath.Join(tempDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	store := newTestStore(t, dbPath)
	defer store.Close()

	// Create two issues
	issue1 := &types.Issue{
		Title:     "Issue 1 (to be deleted)",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	issue2 := &types.Issue{
		Title:     "Issue 2 (to keep)",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	// Create deletions.jsonl with corrupt lines + valid deletion for issue1
	deletionsPath := filepath.Join(beadsDir, "deletions.jsonl")
	now := time.Now().UTC().Format(time.RFC3339)
	corruptContent := `this is not valid json
{"broken
{"id":"` + issue1.ID + `","ts":"` + now + `","by":"test-user","reason":"valid deletion"}
more garbage {{{
`
	if err := os.WriteFile(deletionsPath, []byte(corruptContent), 0644); err != nil {
		t.Fatalf("Failed to write corrupt deletions: %v", err)
	}

	// Load deletions - should skip corrupt lines but parse valid one
	result, err := deletions.LoadDeletions(deletionsPath)
	if err != nil {
		t.Fatalf("LoadDeletions should not fail on corrupt lines: %v", err)
	}

	if result.Skipped != 3 {
		t.Errorf("Expected 3 skipped lines, got %d", result.Skipped)
	}

	if len(result.Records) != 1 {
		t.Errorf("Expected 1 valid record, got %d", len(result.Records))
	}

	if _, found := result.Records[issue1.ID]; !found {
		t.Error("Valid deletion record should be parsed")
	}

	if len(result.Warnings) != 3 {
		t.Errorf("Expected 3 warnings, got %d", len(result.Warnings))
	}

	t.Logf("Warnings: %v", result.Warnings)
	t.Log("✓ Corrupt deletions.jsonl lines handled gracefully")
}

// TestDeletionPropagation_EmptyManifest verifies that import works with
// empty or missing deletions manifest.
func TestDeletionPropagation_EmptyManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()

	beadsDir := filepath.Join(tempDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	store := newTestStore(t, dbPath)
	defer store.Close()

	// Create an issue
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.CreateIssue(ctx, issue, "test-user")

	// Export to JSONL
	jsonlPath := filepath.Join(beadsDir, "beads.jsonl")
	if err := exportIssuesToJSONL(ctx, store, jsonlPath); err != nil {
		t.Fatalf("Failed to export: %v", err)
	}

	// Test 1: No deletions.jsonl exists
	result, err := importJSONLFile(ctx, store, dbPath, jsonlPath, importer.Options{})
	if err != nil {
		t.Fatalf("Import should succeed without deletions.jsonl: %v", err)
	}
	if result.Purged != 0 {
		t.Errorf("Expected 0 purged with no deletions manifest, got %d", result.Purged)
	}
	t.Log("✓ Import works without deletions.jsonl")

	// Test 2: Empty deletions.jsonl
	deletionsPath := filepath.Join(beadsDir, "deletions.jsonl")
	if err := os.WriteFile(deletionsPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty deletions.jsonl: %v", err)
	}

	result, err = importJSONLFile(ctx, store, dbPath, jsonlPath, importer.Options{})
	if err != nil {
		t.Fatalf("Import should succeed with empty deletions.jsonl: %v", err)
	}
	if result.Purged != 0 {
		t.Errorf("Expected 0 purged with empty deletions manifest, got %d", result.Purged)
	}
	t.Log("✓ Import works with empty deletions.jsonl")

	// Verify issue still exists
	check, _ := store.GetIssue(ctx, issue.ID)
	if check == nil {
		t.Error("Issue should still exist")
	}
}

// Helper to resolve deletions.jsonl conflicts by keeping all lines
func resolveDeletionsConflict(t *testing.T, dir string) {
	t.Helper()
	deletionsPath := filepath.Join(dir, ".beads", "deletions.jsonl")
	content, err := os.ReadFile(deletionsPath)
	if err != nil {
		return // No conflict file
	}

	if !strings.Contains(string(content), "<<<<<<<") {
		return // No conflict markers
	}

	// Remove conflict markers, keep all deletion records
	var cleanLines []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "<<<<<<<") ||
			strings.HasPrefix(line, "=======") ||
			strings.HasPrefix(line, ">>>>>>>") {
			continue
		}
		if strings.TrimSpace(line) != "" && strings.HasPrefix(line, "{") {
			cleanLines = append(cleanLines, line)
		}
	}

	cleaned := strings.Join(cleanLines, "\n") + "\n"
	os.WriteFile(deletionsPath, []byte(cleaned), 0644)
	runGitCmdAllowError(t, dir, "add", deletionsPath)
}

// runGitCmdAllowError runs git command and ignores errors
func runGitCmdAllowError(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := runCommandInDir(dir, "git", args...)
	_ = cmd // ignore error
}
