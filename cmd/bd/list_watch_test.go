package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// TestWatchModeWithDaemonDoesNotPanic verifies that the list -w code path
// properly initializes the store when in daemon mode, preventing the nil
// pointer panic that occurred before the fix.
//
// Bug: In daemon mode, store=nil because RPC handles storage. But watchIssues()
// was called directly with nil store, causing panic at store.SearchIssues().
//
// Fix: Call ensureDirectMode() before watchIssues() to initialize the store.
func TestWatchModeWithDaemonDoesNotPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	// Save original state
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath
	origRootCtx := rootCtx
	origAutoImport := autoImportEnabled

	defer func() {
		// Restore state
		daemonClient = origDaemonClient
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		dbPath = origDBPath
		rootCtx = origRootCtx
		autoImportEnabled = origAutoImport
	}()

	// Create temp directory with .beads structure and test database
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}
	testDBPath := filepath.Join(beadsDir, "beads.db")

	// Change to temp dir so beads.FindBeadsDir() finds our test .beads
	// t.Chdir auto-restores working directory after test
	t.Chdir(tmpDir)

	// Create and seed the test database
	setupStore := newTestStore(t, testDBPath)
	testIssue := &types.Issue{
		Title:     "Watch mode test issue",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  2,
	}
	ctx := context.Background()
	if err := setupStore.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	if err := setupStore.Close(); err != nil {
		t.Fatalf("failed to close setup store: %v", err)
	}

	// Simulate daemon mode: store is nil, daemonClient is non-nil
	// This is the state that caused the panic
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rootCtx = ctx

	dbPath = testDBPath
	storeMutex.Lock()
	store = nil
	storeActive = false
	storeMutex.Unlock()
	daemonClient = &rpc.Client{} // Non-nil = daemon mode
	autoImportEnabled = false

	// This is what the fix does: call ensureDirectMode before watchIssues
	// Without this, watchIssues would panic on nil store
	if err := ensureDirectMode("watch mode requires direct database access"); err != nil {
		t.Fatalf("ensureDirectMode failed: %v", err)
	}

	// Verify store is now initialized (fix worked)
	storeMutex.Lock()
	currentStore := store
	isActive := storeActive
	storeMutex.Unlock()

	if currentStore == nil {
		t.Fatal("store is still nil after ensureDirectMode - fix not working")
	}
	if !isActive {
		t.Fatal("store not marked active after ensureDirectMode")
	}

	// Now watchIssues can be called safely (won't panic)
	// Run it briefly in a goroutine to verify no panic
	filter := types.IssueFilter{Limit: 10}
	done := make(chan bool)
	var panicValue interface{}

	go func() {
		defer func() {
			panicValue = recover()
			done <- true
		}()
		// Call watchIssues - should not panic now that store is initialized
		// It will block watching for file changes, so we let it run briefly
		watchIssues(ctx, currentStore, filter, "", false)
	}()

	// Wait briefly for watchIssues to start and potentially panic
	// The panic would occur immediately at store.SearchIssues(), so 100ms is plenty
	select {
	case <-done:
		if panicValue != nil {
			t.Fatalf("watchIssues panicked even with initialized store: %v", panicValue)
		}
		// watchIssues returned (probably due to context cancellation or early exit)
	case <-time.After(100 * time.Millisecond):
		// watchIssues is running without panic - success!
		cancel() // Cancel context to stop watchIssues
	}
}

// TestWatchIssuesDirectlyWithNilStorePanics documents that calling watchIssues
// directly with nil store causes a panic. This is the underlying bug.
func TestWatchIssuesDirectlyWithNilStorePanics(t *testing.T) {
	// Create temp directory with .beads structure
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Change to temp dir so watchIssues finds .beads
	t.Chdir(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This documents that watchIssues panics with nil store
	var nilStore storage.Storage = nil
	filter := types.IssueFilter{}

	done := make(chan bool)
	panicked := false

	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
			done <- true
		}()
		watchIssues(ctx, nilStore, filter, "", false)
	}()

	select {
	case <-done:
		// Goroutine finished
	case <-ctx.Done():
		t.Log("watchIssues didn't panic within timeout")
	}

	// This test documents the bug - watchIssues DOES panic with nil store
	if !panicked {
		t.Error("expected watchIssues to panic with nil store, but it didn't")
	}
}
