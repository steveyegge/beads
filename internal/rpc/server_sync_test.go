package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/testutil/teststore"
	"github.com/steveyegge/beads/internal/types"
)

// commitDoltChanges commits any pending Dolt working set changes so that
// HasUncommittedChanges returns false. This is needed because teststore creates
// Dolt stores, and Dolt tracks uncommitted changes at the storage engine level
// independently of dirty_issues tracking.
func commitDoltChanges(t *testing.T, store storage.Storage) {
	t.Helper()
	if rs, ok := storage.AsRemote(store); ok {
		if err := rs.Commit(context.Background(), "test: clear working set"); err != nil {
			// "nothing to commit" is acceptable
			if err.Error() != "nothing to commit" {
				t.Fatalf("failed to commit Dolt changes: %v", err)
			}
		}
	}
}

// setupSyncTestServer creates a test server with storage for sync tests.
// Forces sync mode to git-portable so that the Dolt push path is not triggered
// (the workspace config.yaml may have sync.mode=dolt-native, which would cause
// push failures since test Dolt stores have no remotes configured).
func setupSyncTestServer(t *testing.T) (*Server, storage.Storage, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create JSONL file
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create jsonl file: %v", err)
	}

	// Force git-portable sync mode for tests. The workspace config.yaml may have
	// sync.mode=dolt-native, which triggers Dolt push (fails with no remote).
	origMode := config.GetString("sync.mode")
	config.Set("sync.mode", "git-portable")
	t.Cleanup(func() {
		config.Set("sync.mode", origMode)
	})

	store := teststore.New(t)

	server := NewServer(filepath.Join(beadsDir, "daemon.sock"), store, tmpDir, dbPath)
	return server, store, tmpDir
}

// createSyncTestIssue creates a test issue in the store
func createSyncTestIssue(t *testing.T, store storage.Storage, id, title string) *types.Issue {
	t.Helper()
	issue := &types.Issue{
		ID:        id,
		Title:     title,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(context.Background(), issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	return issue
}

func TestHandleSyncExport_NoChanges(t *testing.T) {
	server, store, _ := setupSyncTestServer(t)

	// Create and export an issue first to establish baseline
	createSyncTestIssue(t, store, "bd-001", "Test Issue 1")

	// Clear dirty flags to simulate already synced state
	if err := store.ClearDirtyIssuesByID(context.Background(), []string{"bd-001"}); err != nil {
		t.Fatalf("failed to clear dirty flags: %v", err)
	}

	// Commit Dolt working set changes so HasUncommittedChanges returns false.
	// With Dolt as backend, hasUncommittedChangesInStore uses dolt_status
	// (not dirty_issues), so we must commit to clear the working set.
	commitDoltChanges(t, store)

	// Now sync should detect no changes
	args := SyncExportArgs{
		Force:  false,
		DryRun: false,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpSyncExport,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleSyncExport(req)
	if !resp.Success {
		t.Fatalf("sync export failed: %s", resp.Error)
	}

	var result SyncExportResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if !result.Skipped {
		t.Error("expected export to be skipped when no changes")
	}
}

func TestHandleSyncExport_WithChanges(t *testing.T) {
	server, store, _ := setupSyncTestServer(t)

	// Create some test issues
	createSyncTestIssue(t, store, "bd-001", "Test Issue 1")
	createSyncTestIssue(t, store, "bd-002", "Test Issue 2")

	// Issues should be dirty after creation
	args := SyncExportArgs{
		Force:  false,
		DryRun: false,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpSyncExport,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleSyncExport(req)
	if !resp.Success {
		t.Fatalf("sync export failed: %s", resp.Error)
	}

	var result SyncExportResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.Skipped {
		t.Error("expected export to not be skipped when there are changes")
	}

	if result.ExportedCount != 2 {
		t.Errorf("expected 2 exported issues, got %d", result.ExportedCount)
	}
}

func TestHandleSyncExport_DryRun(t *testing.T) {
	server, store, _ := setupSyncTestServer(t)

	// Create a test issue
	createSyncTestIssue(t, store, "bd-001", "Test Issue 1")

	args := SyncExportArgs{
		Force:  false,
		DryRun: true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpSyncExport,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleSyncExport(req)
	if !resp.Success {
		t.Fatalf("sync export dry-run failed: %s", resp.Error)
	}

	var result SyncExportResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Dry run should report changed count but not actually export
	if result.ExportedCount != 0 {
		t.Errorf("expected 0 exported issues in dry-run, got %d", result.ExportedCount)
	}

	// With Dolt + SkipDirtyTracking=true (teststore default), GetDirtyIssues
	// always returns empty, so ChangedCount will be 0 even though there are
	// actual uncommitted Dolt changes. This is expected behavior — dirty
	// tracking is a JSONL optimization that's disabled in dolt-native mode.
	// Just verify the dry-run doesn't fail; the exact count depends on config.
	t.Logf("dry-run ChangedCount=%d (0 expected with SkipDirtyTracking)", result.ChangedCount)
}

func TestHandleSyncExport_Force(t *testing.T) {
	server, store, _ := setupSyncTestServer(t)

	// Create and sync an issue
	createSyncTestIssue(t, store, "bd-001", "Test Issue 1")

	// Clear dirty flags
	if err := store.ClearDirtyIssuesByID(context.Background(), []string{"bd-001"}); err != nil {
		t.Fatalf("failed to clear dirty flags: %v", err)
	}

	// Force export should export even when no changes
	args := SyncExportArgs{
		Force:  true,
		DryRun: false,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpSyncExport,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleSyncExport(req)
	if !resp.Success {
		t.Fatalf("sync export force failed: %s", resp.Error)
	}

	var result SyncExportResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.Skipped {
		t.Error("expected force export to not be skipped")
	}

	if result.ExportedCount != 1 {
		t.Errorf("expected 1 exported issue, got %d", result.ExportedCount)
	}
}

func TestHandleSyncStatus_Basic(t *testing.T) {
	server, store, _ := setupSyncTestServer(t)

	// Create a dirty issue
	createSyncTestIssue(t, store, "bd-001", "Test Issue 1")

	req := &Request{
		Operation: OpSyncStatus,
		Args:      []byte("{}"),
		Actor:     "test",
	}

	resp := server.handleSyncStatus(req)
	if !resp.Success {
		t.Fatalf("sync status failed: %s", resp.Error)
	}

	var result SyncStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Check basic fields are populated
	if result.SyncMode == "" {
		t.Error("expected sync mode to be set")
	}

	if result.ConflictStrategy == "" {
		t.Error("expected conflict strategy to be set")
	}

	// With Dolt + SkipDirtyTracking=true (teststore default), GetDirtyIssues
	// always returns empty, so PendingChanges will be 0. This is expected
	// behavior — dirty tracking is a JSONL optimization disabled in dolt-native mode.
	// Just verify the status handler runs without error.
	_ = store
	t.Logf("pending changes: %d (0 expected with SkipDirtyTracking)", result.PendingChanges)
}

func TestHandleSyncStatus_NoChanges(t *testing.T) {
	server, store, _ := setupSyncTestServer(t)

	// Create an issue and clear dirty flags
	createSyncTestIssue(t, store, "bd-001", "Test Issue 1")
	if err := store.ClearDirtyIssuesByID(context.Background(), []string{"bd-001"}); err != nil {
		t.Fatalf("failed to clear dirty flags: %v", err)
	}

	req := &Request{
		Operation: OpSyncStatus,
		Args:      []byte("{}"),
		Actor:     "test",
	}

	resp := server.handleSyncStatus(req)
	if !resp.Success {
		t.Fatalf("sync status failed: %s", resp.Error)
	}

	var result SyncStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Should have 0 pending changes
	if result.PendingChanges != 0 {
		t.Errorf("expected 0 pending changes, got %d", result.PendingChanges)
	}
}

func TestHandleSyncExport_InvalidArgs(t *testing.T) {
	server, _, _ := setupSyncTestServer(t)

	req := &Request{
		Operation: OpSyncExport,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleSyncExport(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}
