package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// TestSyncFlushRace_SyncWhileDaemonFlushing tests bd sync running while
// the daemon is performing an auto-flush operation.
// Run with: go test -race -run TestSyncFlushRace_SyncWhileDaemonFlushing
func TestSyncFlushRace_SyncWhileDaemonFlushing(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "sync"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive
	oldFlushManager := flushManager

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		if flushManager != nil {
			flushManager.Shutdown()
		}
		flushManager = oldFlushManager
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Create FlushManager for this test
	flushManager = NewFlushManager(true, 10*time.Millisecond)

	// Create initial issues (CreateIssue marks them dirty automatically)
	for i := 0; i < 20; i++ {
		issue := &types.Issue{
			ID:        generateUniqueTestID(t, "sync", i),
			Title:     "Sync Flush Test",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	var wg sync.WaitGroup
	var daemonFlushes atomic.Int64
	var syncExports atomic.Int64

	// Simulate daemon auto-flush (uses FlushManager)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			flushManager.MarkDirty(i%5 == 0) // Occasional full export
			daemonFlushes.Add(1)
			time.Sleep(time.Millisecond * 5)
		}
	}()

	// Simulate bd sync export operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			// Direct flush (simulating sync command)
			flushToJSONLWithState(flushState{forceDirty: true, forceFullExport: true})
			syncExports.Add(1)
			time.Sleep(time.Millisecond * 10)
		}
	}()

	wg.Wait()

	// Ensure final flush completes
	flushManager.FlushNow()

	t.Logf("daemon flushes: %d, sync exports: %d", daemonFlushes.Load(), syncExports.Load())

	// Verify data integrity
	issues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	if len(issues) != 20 {
		t.Errorf("expected 20 issues, got %d", len(issues))
	}

	// Verify JSONL is valid
	issueMap, err := readExistingJSONL(jsonlPath)
	if err != nil {
		t.Errorf("JSONL is corrupt: %v", err)
	}
	if len(issueMap) != 20 {
		t.Errorf("JSONL has %d issues, expected 20", len(issueMap))
	}
}

// TestSyncFlushRace_ExportDuringGitOperations tests export happening while
// git operations (pull/checkout) might be modifying the JSONL.
func TestSyncFlushRace_ExportDuringGitOperations(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "git"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Create issues in DB (CreateIssue marks them dirty automatically)
	for i := 0; i < 15; i++ {
		issue := &types.Issue{
			ID:        generateUniqueTestID(t,"git", i),
			Title:     "Git Operation Test",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	var wg sync.WaitGroup
	var gitModifications atomic.Int64
	var exportOperations atomic.Int64

	// Simulate git operations modifying JSONL (like git pull)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			// Simulate git writing to JSONL
			gitIssues := make([]*types.Issue, 15)
			for j := 0; j < 15; j++ {
				gitIssues[j] = &types.Issue{
					ID:        generateUniqueTestID(t,"git", j),
					Title:     "Updated by git",
					Status:    types.StatusOpen,
					Priority:  2,
					IssueType: types.TypeTask,
				}
			}

			f, err := os.Create(jsonlPath)
			if err != nil {
				continue
			}
			encoder := json.NewEncoder(f)
			for _, issue := range gitIssues {
				encoder.Encode(issue)
			}
			f.Close()
			gitModifications.Add(1)
			time.Sleep(time.Millisecond * 5)
		}
	}()

	// Export operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			flushToJSONLWithState(flushState{forceDirty: true})
			exportOperations.Add(1)
			time.Sleep(time.Millisecond * 7)
		}
	}()

	wg.Wait()

	t.Logf("git modifications: %d, export operations: %d", gitModifications.Load(), exportOperations.Load())

	// File should still be valid JSON
	issueMap, err := readExistingJSONL(jsonlPath)
	if err != nil {
		t.Errorf("JSONL is corrupt after concurrent operations: %v", err)
	}
	if len(issueMap) == 0 {
		t.Error("JSONL is empty after concurrent operations")
	}
}

// TestSyncFlushRace_ImportDuringLocalModifications tests import running while
// local modifications are being made to the database.
func TestSyncFlushRace_ImportDuringLocalModifications(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "local"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive
	oldNoAutoImport := noAutoImport

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	noAutoImport = false
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		noAutoImport = oldNoAutoImport
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Create initial JSONL with issues to import
	importIssues := make([]*types.Issue, 10)
	for i := 0; i < 10; i++ {
		importIssues[i] = &types.Issue{
			ID:        generateUniqueTestID(t,"local", i),
			Title:     "Import Source",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
	}

	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("failed to create JSONL: %v", err)
	}
	encoder := json.NewEncoder(f)
	for _, issue := range importIssues {
		if err := encoder.Encode(issue); err != nil {
			t.Fatalf("failed to encode issue: %v", err)
		}
	}
	f.Close()

	var wg sync.WaitGroup
	var localCreates atomic.Int64
	var importAttempts atomic.Int64

	// Local modification goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 10; i < 30; i++ {
			issue := &types.Issue{
				ID:        generateUniqueTestID(t,"local", i),
				Title:     "Local Create",
				Status:    types.StatusOpen,
				Priority:  3,
				IssueType: types.TypeTask,
			}
			if err := testStore.CreateIssue(ctx, issue, "test"); err == nil {
				localCreates.Add(1)
			}
			time.Sleep(time.Millisecond * 2)
		}
	}()

	// Import goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			autoImportIfNewer()
			importAttempts.Add(1)
			time.Sleep(time.Millisecond * 10)
		}
	}()

	wg.Wait()

	t.Logf("local creates: %d, import attempts: %d", localCreates.Load(), importAttempts.Load())

	// Verify database integrity
	issues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}

	// Should have both imported and locally created issues
	if len(issues) < 10 {
		t.Errorf("expected at least 10 issues, got %d", len(issues))
	}
}

// TestSyncFlushRace_DebounceTimerVsSync tests the debounce timer firing
// while a sync operation is in progress.
func TestSyncFlushRace_DebounceTimerVsSync(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "debounce"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive
	oldFlushManager := flushManager

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		if flushManager != nil {
			flushManager.Shutdown()
		}
		flushManager = oldFlushManager
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Create FlushManager with very short debounce
	flushManager = NewFlushManager(true, 5*time.Millisecond)

	// Create issues (CreateIssue marks them dirty automatically)
	for i := 0; i < 10; i++ {
		issue := &types.Issue{
			ID:        generateUniqueTestID(t,"debounce", i),
			Title:     "Debounce Test",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	var wg sync.WaitGroup
	var markDirtyCount atomic.Int64
	var syncLockAcquired atomic.Int64
	lockPath := filepath.Join(beadsDir, ".sync.lock")

	// Rapid dirty marking to trigger debounce timer frequently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			flushManager.MarkDirty(false)
			markDirtyCount.Add(1)
			time.Sleep(time.Millisecond)
		}
	}()

	// Sync-like operation that holds the lock
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			lock := flock.New(lockPath)
			locked, err := lock.TryLock()
			if err != nil || !locked {
				continue
			}
			syncLockAcquired.Add(1)

			// Simulate sync operation duration
			time.Sleep(time.Millisecond * 10)

			// Export while holding lock
			flushToJSONLWithState(flushState{forceDirty: true})

			lock.Unlock()
			time.Sleep(time.Millisecond * 5)
		}
	}()

	wg.Wait()

	// Allow debounce timer to settle
	time.Sleep(50 * time.Millisecond)
	flushManager.FlushNow()

	t.Logf("mark dirty count: %d, sync lock acquired: %d", markDirtyCount.Load(), syncLockAcquired.Load())

	// Verify JSONL integrity
	issueMap, err := readExistingJSONL(jsonlPath)
	if err != nil {
		t.Errorf("JSONL is corrupt: %v", err)
	}
	if len(issueMap) != 10 {
		t.Errorf("expected 10 issues in JSONL, got %d", len(issueMap))
	}
}

// TestSyncFlushRace_MultipleFlushManagers tests what happens if multiple
// FlushManagers exist (simulating daemon and direct mode confusion).
func TestSyncFlushRace_MultipleFlushManagers(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "multi"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Create issues (CreateIssue marks them dirty automatically)
	for i := 0; i < 10; i++ {
		issue := &types.Issue{
			ID:        generateUniqueTestID(t,"multi", i),
			Title:     "Multi Manager Test",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Create two flush managers (simulating race between daemon and direct mode)
	fm1 := NewFlushManager(true, 10*time.Millisecond)
	fm2 := NewFlushManager(true, 15*time.Millisecond)

	var wg sync.WaitGroup
	var fm1Flushes atomic.Int64
	var fm2Flushes atomic.Int64

	// Both managers try to flush
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			fm1.MarkDirty(false)
			time.Sleep(time.Millisecond * 5)
			fm1Flushes.Add(1)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			fm2.MarkDirty(true)
			time.Sleep(time.Millisecond * 7)
			fm2Flushes.Add(1)
		}
	}()

	wg.Wait()

	// Shutdown both
	fm1.Shutdown()
	fm2.Shutdown()

	t.Logf("fm1 flushes: %d, fm2 flushes: %d", fm1Flushes.Load(), fm2Flushes.Load())

	// Verify JSONL is valid and contains all issues
	issueMap, err := readExistingJSONL(jsonlPath)
	if err != nil {
		t.Errorf("JSONL is corrupt: %v", err)
	}
	if len(issueMap) != 10 {
		t.Errorf("expected 10 issues in JSONL, got %d", len(issueMap))
	}
}

// TestSyncFlushRace_FlushDuringShutdown tests what happens when a flush
// is triggered during shutdown.
func TestSyncFlushRace_FlushDuringShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "shutdown"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Create issues (CreateIssue marks them dirty automatically)
	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			ID:        generateUniqueTestID(t,"shutdown", i),
			Title:     "Shutdown Test",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	fm := NewFlushManager(true, 100*time.Millisecond) // Long debounce

	var wg sync.WaitGroup
	var shutdownErrors atomic.Int64

	// Mark dirty repeatedly
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			fm.MarkDirty(false)
			time.Sleep(time.Millisecond)
		}
	}()

	// Trigger shutdown while marks are happening
	go func() {
		time.Sleep(20 * time.Millisecond)
		if err := fm.Shutdown(); err != nil {
			shutdownErrors.Add(1)
		}
	}()

	wg.Wait()

	// Additional shutdown calls should be idempotent
	if err := fm.Shutdown(); err != nil {
		t.Errorf("second shutdown returned error: %v", err)
	}

	// MarkDirty after shutdown should not panic
	fm.MarkDirty(false) // Should be ignored gracefully
}
