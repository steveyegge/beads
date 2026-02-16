//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExtractIssuePrefix removed: extractIssuePrefix local wrapper was removed.
// Covered by TestExtractPrefix in helpers_test.go via utils.ExtractIssuePrefix.

func TestLoadIssuesFromJSONL(t *testing.T) {
	tempDir := t.TempDir()
	jsonlPath := filepath.Join(tempDir, "test.jsonl")

	// Create test JSONL file
	content := `{"id":"bd-1","title":"Test Issue 1","description":"Test"}
{"id":"bd-2","title":"Test Issue 2","description":"Another test"}

{"id":"bd-3","title":"Test Issue 3","description":"Third test"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	issues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		t.Fatalf("loadIssuesFromJSONL failed: %v", err)
	}

	if len(issues) != 3 {
		t.Errorf("Expected 3 issues, got %d", len(issues))
	}

	if issues[0].ID != "bd-1" || issues[0].Title != "Test Issue 1" {
		t.Errorf("First issue mismatch: %+v", issues[0])
	}
	if issues[1].ID != "bd-2" {
		t.Errorf("Second issue ID mismatch: %s", issues[1].ID)
	}
	if issues[2].ID != "bd-3" {
		t.Errorf("Third issue ID mismatch: %s", issues[2].ID)
	}
}

func TestLoadIssuesFromJSONL_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	jsonlPath := filepath.Join(tempDir, "invalid.jsonl")

	content := `{"id":"bd-1","title":"Valid"}
invalid json here
{"id":"bd-2","title":"Another valid"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// loadIssuesFromJSONL now skips invalid lines with a warning (not an error)
	issues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("Expected 2 valid issues (invalid lines skipped), got %d", len(issues))
	}
}

func TestLoadIssuesFromJSONL_NonExistent(t *testing.T) {
	// loadIssuesFromJSONL now returns nil, nil for non-existent files
	issues, err := loadIssuesFromJSONL("/nonexistent/file.jsonl")
	if err != nil {
		t.Errorf("Expected nil error for non-existent file, got: %v", err)
	}
	if issues != nil {
		t.Errorf("Expected nil issues for non-existent file, got %d", len(issues))
	}
}

// NOTE: TestDetectPrefix was removed because it referenced the deleted memory backend and the detectPrefix function which no longer exists.

func TestInitializeNoDbMode_SetsStoreActive(t *testing.T) {
	t.Skip("no-db mode has been removed; beads now requires Dolt")
	// This test verifies the fix for bd comment --no-db not working.
	// The bug was that initializeNoDbMode() set `store` but not `storeActive`,
	// so ensureStoreActive() would try to find a SQLite database.

	// Reset global state for test isolation
	ensureCleanGlobalState(t)

	tempDir := t.TempDir()
	beadsDir := filepath.Join(tempDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create a minimal JSONL file with one issue
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	content := `{"id":"bd-1","title":"Test Issue","status":"open"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write JSONL: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldStoreActive := storeActive
	oldCwd, _ := os.Getwd()
	defer func() {
		storeMutex.Lock()
		store = oldStore
		storeActive = oldStoreActive
		storeMutex.Unlock()
		_ = os.Chdir(oldCwd)
	}()

	// Change to temp dir so initializeNoDbMode finds .beads
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Reset global state
	storeMutex.Lock()
	store = nil
	storeActive = false
	storeMutex.Unlock()

	// Initialize no-db mode
	if err := initializeNoDbMode(); err != nil {
		t.Fatalf("initializeNoDbMode failed: %v", err)
	}

	// Verify storeActive is now true
	storeMutex.Lock()
	active := storeActive
	s := store
	storeMutex.Unlock()

	if !active {
		t.Error("storeActive should be true after initializeNoDbMode")
	}
	if s == nil {
		t.Fatal("store should not be nil after initializeNoDbMode")
	}

	// ensureStoreActive should now return immediately without error
	if err := ensureStoreActive(); err != nil {
		t.Errorf("ensureStoreActive should succeed after initializeNoDbMode: %v", err)
	}

	// Verify comments work (this was the failing case)
	ctx := rootCtx
	comment, err := s.AddIssueComment(ctx, "bd-1", "testuser", "Test comment")
	if err != nil {
		t.Fatalf("AddIssueComment failed: %v", err)
	}
	if comment.Text != "Test comment" {
		t.Errorf("Expected 'Test comment', got %s", comment.Text)
	}

	comments, err := s.GetIssueComments(ctx, "bd-1")
	if err != nil {
		t.Fatalf("GetIssueComments failed: %v", err)
	}
	if len(comments) != 1 {
		t.Errorf("Expected 1 comment, got %d", len(comments))
	}
}

func TestInitializeNoDbMode_SetsCmdCtxStoreActive(t *testing.T) {
	t.Skip("no-db mode has been removed; beads now requires Dolt")
	// GH#897: Verify that initializeNoDbMode sets storeActive global.
	// This is critical for commands like `comments add` that call ensureStoreActive().
	ensureCleanGlobalState(t)

	tempDir := t.TempDir()
	beadsDir := filepath.Join(tempDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create a minimal JSONL file with one issue
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	content := `{"id":"mmm-155","title":"Test Issue","status":"open"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write JSONL: %v", err)
	}

	// Initialize CommandContext (simulates what PersistentPreRun does)
	initCommandContext()

	oldCwd, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldCwd)
		resetCommandContext()
	}()

	// Change to temp dir so initializeNoDbMode finds .beads
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Initialize no-db mode
	if err := initializeNoDbMode(); err != nil {
		t.Fatalf("initializeNoDbMode failed: %v", err)
	}

	// Verify storeActive global is true
	storeMutex.Lock()
	active := storeActive
	storeMutex.Unlock()
	if !active {
		t.Error("storeActive should be true after initializeNoDbMode (GH#897)")
	}
	if store == nil {
		t.Error("store should not be nil after initializeNoDbMode")
	}

	// ensureStoreActive should succeed
	if err := ensureStoreActive(); err != nil {
		t.Errorf("ensureStoreActive should succeed after initializeNoDbMode: %v", err)
	}

	// Comments should work
	comment, err := store.AddIssueComment(rootCtx, "mmm-155", "testuser", "Test comment")
	if err != nil {
		t.Fatalf("AddIssueComment failed: %v", err)
	}
	if comment.Text != "Test comment" {
		t.Errorf("Expected 'Test comment', got %s", comment.Text)
	}
}
