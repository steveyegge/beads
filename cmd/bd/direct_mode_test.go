package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestFallbackToDirectModeEnablesFlush(t *testing.T) {
	origDaemonClient := daemonClient
	origDaemonStatus := daemonStatus
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath
	origAutoImport := autoImportEnabled
	origAutoFlush := autoFlushEnabled
	origIsDirty := isDirty
	origNeedsFull := needsFullExport
	origFlushFailures := flushFailureCount
	origLastFlushErr := lastFlushError

	flushMutex.Lock()
	if flushTimer != nil {
		flushTimer.Stop()
		flushTimer = nil
	}
	flushMutex.Unlock()

	defer func() {
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()

		daemonClient = origDaemonClient
		daemonStatus = origDaemonStatus
		dbPath = origDBPath
		autoImportEnabled = origAutoImport
		autoFlushEnabled = origAutoFlush
		isDirty = origIsDirty
		needsFullExport = origNeedsFull
		flushFailureCount = origFlushFailures
		lastFlushError = origLastFlushErr

		flushMutex.Lock()
		if flushTimer != nil {
			flushTimer.Stop()
			flushTimer = nil
		}
		flushMutex.Unlock()
	}()

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}
	testDBPath := filepath.Join(beadsDir, "test.db")

	// Seed database with issues
	setupStore, err := sqlite.New(testDBPath)
	if err != nil {
		t.Fatalf("failed to create seed store: %v", err)
	}

	ctx := context.Background()
	target := &types.Issue{
		Title:     "Issue to delete",
		IssueType: types.TypeTask,
		Priority:  2,
		Status:    types.StatusOpen,
	}
	if err := setupStore.CreateIssue(ctx, target, "test"); err != nil {
		t.Fatalf("failed to create target issue: %v", err)
	}

	neighbor := &types.Issue{
		Title:       "Neighbor issue",
		Description: "See " + target.ID,
		IssueType:   types.TypeTask,
		Priority:    2,
		Status:      types.StatusOpen,
	}
	if err := setupStore.CreateIssue(ctx, neighbor, "test"); err != nil {
		t.Fatalf("failed to create neighbor issue: %v", err)
	}
	if err := setupStore.Close(); err != nil {
		t.Fatalf("failed to close seed store: %v", err)
	}

	// Simulate daemon-connected state before fallback
	dbPath = testDBPath
	storeMutex.Lock()
	store = nil
	storeActive = false
	storeMutex.Unlock()
	daemonClient = &rpc.Client{}
	daemonStatus = DaemonStatus{}
	autoImportEnabled = false
	autoFlushEnabled = true
	isDirty = false
	needsFullExport = false

	if err := fallbackToDirectMode("test fallback"); err != nil {
		t.Fatalf("fallbackToDirectMode failed: %v", err)
	}

	if daemonClient != nil {
		t.Fatal("expected daemonClient to be nil after fallback")
	}

	storeMutex.Lock()
	active := storeActive && store != nil
	storeMutex.Unlock()
	if !active {
		t.Fatal("expected store to be active after fallback")
	}

	// Force a full export and flush synchronously
	markDirtyAndScheduleFullExport()
	flushMutex.Lock()
	if flushTimer != nil {
		flushTimer.Stop()
		flushTimer = nil
	}
	flushMutex.Unlock()
	flushToJSONL()

	jsonlPath := findJSONLPath()
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to read JSONL export: %v", err)
	}

	if !bytes.Contains(data, []byte(target.ID)) {
		t.Fatalf("expected JSONL export to contain deleted issue ID %s", target.ID)
	}
	if !bytes.Contains(data, []byte(neighbor.ID)) {
		t.Fatalf("expected JSONL export to contain neighbor issue ID %s", neighbor.ID)
	}
}
