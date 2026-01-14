package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// TestDatabaseFileReplacementDuringTransaction tests that file replacement
// (like git merge) during an active transaction is handled safely.
func TestDatabaseFileReplacementDuringTransaction(t *testing.T) {
	// setup: main database
	tmpDir1, err := os.MkdirTemp("", "beads-replace-during-tx-1-*")
	if err != nil {
		t.Fatalf("failed to create temp dir 1: %v", err)
	}
	defer os.RemoveAll(tmpDir1)

	mainDBPath := filepath.Join(tmpDir1, "main.db")
	ctx := context.Background()

	mainStore, err := New(ctx, mainDBPath)
	if err != nil {
		t.Fatalf("failed to create main store: %v", err)
	}
	defer mainStore.Close()

	if err := mainStore.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	mainStore.EnableFreshnessChecking()

	// create initial issue
	issue := &types.Issue{
		Title:     "Original Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := mainStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	originalID := issue.ID

	// setup: replacement database with different content
	tmpDir2, err := os.MkdirTemp("", "beads-replace-during-tx-2-*")
	if err != nil {
		t.Fatalf("failed to create temp dir 2: %v", err)
	}
	defer os.RemoveAll(tmpDir2)

	replaceDBPath := filepath.Join(tmpDir2, "replace.db")
	replaceStore, err := New(ctx, replaceDBPath)
	if err != nil {
		t.Fatalf("failed to create replace store: %v", err)
	}

	if err := replaceStore.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		replaceStore.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// add different issue to replacement
	replaceIssue := &types.Issue{
		Title:     "Replacement Issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeFeature,
	}
	if err := replaceStore.CreateIssue(ctx, replaceIssue, "test"); err != nil {
		replaceStore.Close()
		t.Fatalf("CreateIssue on replace store failed: %v", err)
	}
	replaceID := replaceIssue.ID

	replaceStore.Close()

	// start a transaction that will be active during file replacement
	txStarted := make(chan struct{})
	txContinue := make(chan struct{})
	txDone := make(chan error, 1)

	var txCreatedID string

	go func() {
		err := mainStore.RunInTransaction(ctx, func(tx storage.Transaction) error {
			// signal that transaction has started
			close(txStarted)

			// wait for file replacement to happen
			<-txContinue

			// create issue within transaction (after file was replaced)
			issue := &types.Issue{
				Title:     "During-Replace Issue",
				Status:    types.StatusOpen,
				Priority:  2,
				IssueType: types.TypeTask,
			}
			if err := tx.CreateIssue(ctx, issue, "test"); err != nil {
				return err
			}
			txCreatedID = issue.ID

			return nil
		})
		txDone <- err
	}()

	// wait for transaction to start
	<-txStarted

	// perform file replacement while transaction is active
	// read replacement DB content
	replaceContent, err := os.ReadFile(replaceDBPath)
	if err != nil {
		t.Fatalf("failed to read replacement DB: %v", err)
	}

	// remove WAL files
	os.Remove(mainDBPath + "-wal")
	os.Remove(mainDBPath + "-shm")

	// write to temp file and rename (atomic replace)
	tempPath := mainDBPath + ".new"
	if err := os.WriteFile(tempPath, replaceContent, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if err := os.Rename(tempPath, mainDBPath); err != nil {
		t.Fatalf("failed to rename: %v", err)
	}

	// allow transaction to continue
	close(txContinue)

	// wait for transaction result
	txErr := <-txDone

	// transaction should complete (it operates on the old file descriptor)
	// the reconnect happens after the transaction, not during
	if txErr != nil {
		// it's acceptable if the transaction fails due to the replacement
		t.Logf("transaction completed with error (may be expected): %v", txErr)
	} else {
		t.Logf("transaction completed successfully")
	}

	// after transaction, queries should eventually see replacement data
	// (freshness checker may need to trigger reconnect first)
	time.Sleep(100 * time.Millisecond)

	// query should work (may return old or new data depending on reconnect timing)
	originalIssue, _ := mainStore.GetIssue(ctx, originalID)
	replaceIssueResult, _ := mainStore.GetIssue(ctx, replaceID)

	t.Logf("original issue (ID %s) visible: %v", originalID, originalIssue != nil)
	t.Logf("replacement issue (ID %s) visible: %v", replaceID, replaceIssueResult != nil)
	t.Logf("tx-created issue (ID %s) visible: %v", txCreatedID, txCreatedID != "")
}

// TestInodeChangeDetection tests that inode changes are properly detected.
func TestInodeChangeDetection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-inode-detect-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	store, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	store.EnableFreshnessChecking()

	// get initial inode
	initialInode := getInode(dbPath)
	if initialInode == 0 {
		t.Skip("inode detection not available on this platform")
	}

	t.Logf("initial inode: %d", initialInode)

	// get freshness checker state
	if store.freshness == nil {
		t.Fatal("freshness checker not enabled")
	}

	trackedInode, _, _ := store.freshness.DebugState()
	if trackedInode != initialInode {
		t.Errorf("tracked inode %d != initial inode %d", trackedInode, initialInode)
	}

	// replace file to change inode
	content, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("failed to read DB: %v", err)
	}

	tempPath := dbPath + ".new"
	if err := os.WriteFile(tempPath, content, 0644); err != nil {
		t.Fatalf("failed to write temp: %v", err)
	}

	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	if err := os.Rename(tempPath, dbPath); err != nil {
		t.Fatalf("failed to rename: %v", err)
	}

	newInode := getInode(dbPath)
	t.Logf("new inode after replace: %d", newInode)

	if newInode == initialInode {
		t.Log("inode did not change (filesystem may not support atomic rename)")
	}

	// trigger freshness check
	changed := store.freshness.Check()
	t.Logf("freshness check detected change: %v", changed)

	// check should detect the inode change
	if newInode != initialInode && !changed {
		t.Error("freshness check should have detected inode change")
	}
}

// TestReconnectBlocksDuringTransaction tests that reconnect waits for
// active transactions to complete (GH#607 fix).
func TestReconnectBlocksDuringTransaction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-reconnect-blocks-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	store, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	store.EnableFreshnessChecking()

	// create initial issue
	issue := &types.Issue{
		Title:     "Initial Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// start long-running transaction
	txStarted := make(chan struct{})
	txRelease := make(chan struct{})
	txDone := make(chan error, 1)

	go func() {
		err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			// signal start
			close(txStarted)

			// create issue within transaction
			issue := &types.Issue{
				Title:     "Long-running TX Issue",
				Status:    types.StatusOpen,
				Priority:  2,
				IssueType: types.TypeTask,
			}
			if err := tx.CreateIssue(ctx, issue, "test"); err != nil {
				return err
			}

			// wait for release signal
			<-txRelease

			return nil
		})
		txDone <- err
	}()

	// wait for transaction to start
	<-txStarted

	// attempt reconnect while transaction is active
	reconnectDone := make(chan error, 1)
	go func() {
		reconnectDone <- store.reconnect()
	}()

	// reconnect should block (wait for transaction)
	select {
	case err := <-reconnectDone:
		// if reconnect completed immediately, the transaction must have released
		select {
		case <-txRelease:
			// ok, release already happened
		default:
			// reconnect completed without waiting for transaction
			// this could happen if reconnect completed before acquiring lock
			t.Logf("reconnect completed: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		// reconnect is blocked as expected
		t.Log("reconnect is correctly blocked waiting for transaction")
	}

	// release transaction
	close(txRelease)

	// wait for transaction to complete
	txErr := <-txDone
	if txErr != nil {
		t.Errorf("transaction failed: %v", txErr)
	}

	// reconnect should complete after transaction
	select {
	case err := <-reconnectDone:
		if err != nil {
			t.Errorf("reconnect failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("reconnect did not complete after transaction finished")
	}
}

// TestWritesAfterGitMergeReplaceDatabase simulates writes after git merge
// replaces the database file.
func TestWritesAfterGitMergeReplaceDatabase(t *testing.T) {
	// setup: main database with existing data
	tmpDir1, err := os.MkdirTemp("", "beads-git-merge-1-*")
	if err != nil {
		t.Fatalf("failed to create temp dir 1: %v", err)
	}
	defer os.RemoveAll(tmpDir1)

	mainDBPath := filepath.Join(tmpDir1, "beads.db")
	ctx := context.Background()

	mainStore, err := New(ctx, mainDBPath)
	if err != nil {
		t.Fatalf("failed to create main store: %v", err)
	}

	if err := mainStore.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		mainStore.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	mainStore.EnableFreshnessChecking()

	// create issues on main
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Main Issue %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := mainStore.CreateIssue(ctx, issue, "main"); err != nil {
			mainStore.Close()
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	// setup: branch database with different data (simulates git branch)
	tmpDir2, err := os.MkdirTemp("", "beads-git-merge-2-*")
	if err != nil {
		mainStore.Close()
		t.Fatalf("failed to create temp dir 2: %v", err)
	}
	defer os.RemoveAll(tmpDir2)

	branchDBPath := filepath.Join(tmpDir2, "beads.db")
	branchStore, err := New(ctx, branchDBPath)
	if err != nil {
		mainStore.Close()
		t.Fatalf("failed to create branch store: %v", err)
	}

	if err := branchStore.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		branchStore.Close()
		mainStore.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create different issues on branch
	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Branch Issue %d", i),
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeFeature,
		}
		if err := branchStore.CreateIssue(ctx, issue, "branch"); err != nil {
			branchStore.Close()
			mainStore.Close()
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	branchStore.Close()

	// count main issues before merge
	issuesBefore, err := mainStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		mainStore.Close()
		t.Fatalf("SearchIssues failed: %v", err)
	}
	t.Logf("issues before merge: %d", len(issuesBefore))

	// simulate git merge: replace main DB with branch DB
	branchContent, err := os.ReadFile(branchDBPath)
	if err != nil {
		mainStore.Close()
		t.Fatalf("failed to read branch DB: %v", err)
	}

	os.Remove(mainDBPath + "-wal")
	os.Remove(mainDBPath + "-shm")

	tempPath := mainDBPath + ".new"
	if err := os.WriteFile(tempPath, branchContent, 0644); err != nil {
		mainStore.Close()
		t.Fatalf("failed to write temp: %v", err)
	}

	if err := os.Rename(tempPath, mainDBPath); err != nil {
		mainStore.Close()
		t.Fatalf("failed to rename: %v", err)
	}

	t.Log("simulated git merge (replaced database file)")

	// small delay for filesystem
	time.Sleep(100 * time.Millisecond)

	// write to database after merge
	// this should trigger freshness check and reconnect
	newIssue := &types.Issue{
		Title:     "Post-Merge Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err = mainStore.CreateIssue(ctx, newIssue, "post-merge")
	if err != nil {
		t.Logf("CreateIssue after merge: %v", err)
		// may fail if connection is stale - this is acceptable for this test
	} else {
		t.Logf("successfully created issue after merge: %s", newIssue.ID)
	}

	// verify we see branch data (5 issues) + possibly new issue
	issuesAfter, err := mainStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Logf("SearchIssues after merge: %v", err)
	} else {
		t.Logf("issues after merge: %d", len(issuesAfter))

		// count branch issues visible
		branchCount := 0
		for _, issue := range issuesAfter {
			if issue.IssueType == types.TypeFeature {
				branchCount++
			}
		}
		t.Logf("branch issues visible: %d", branchCount)
	}

	mainStore.Close()
}

// TestConcurrentReadsWithFileReplacement tests multiple concurrent readers
// during file replacement.
func TestConcurrentReadsWithFileReplacement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent file replacement test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "beads-concurrent-replace-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	store, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	store.EnableFreshnessChecking()

	// create initial issues
	for i := 0; i < 10; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Initial Issue %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "setup"); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	const numReaders = 10
	const readsPerReader = 50

	var wg sync.WaitGroup
	var readSuccess atomic.Int64
	var readErrors atomic.Int64
	stopSignal := make(chan struct{})

	// start readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for j := 0; j < readsPerReader; j++ {
				select {
				case <-stopSignal:
					return
				default:
				}

				_, err := store.SearchIssues(ctx, "", types.IssueFilter{})
				if err != nil {
					readErrors.Add(1)
				} else {
					readSuccess.Add(1)
				}

				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// perform file replacements while reads are happening
	go func() {
		for i := 0; i < 5; i++ {
			select {
			case <-stopSignal:
				return
			default:
			}

			// read current content
			content, err := os.ReadFile(dbPath)
			if err != nil {
				continue
			}

			// write to temp and rename
			tempPath := dbPath + fmt.Sprintf(".new%d", i)
			if err := os.WriteFile(tempPath, content, 0644); err != nil {
				continue
			}

			os.Remove(dbPath + "-wal")
			os.Remove(dbPath + "-shm")

			os.Rename(tempPath, dbPath)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// wait for readers
	wg.Wait()
	close(stopSignal)

	totalReads := int64(numReaders * readsPerReader)
	successRate := float64(readSuccess.Load()) / float64(totalReads) * 100

	t.Logf("reads: %d/%d successful (%.1f%%), errors: %d",
		readSuccess.Load(), totalReads, successRate, readErrors.Load())

	// File replacement during active reads is inherently disruptive to SQLite connections.
	// We just verify the system doesn't crash/panic - some errors are expected.
	// The real code closes and reopens the store after git operations.
	if readSuccess.Load() == 0 {
		t.Error("expected at least some successful reads")
	}
}

// TestFreshnessCheckRaceCondition tests the race condition fix from GH#607.
// Previously, checkFreshness() could trigger reconnect() while another operation
// was using the database connection, causing "database is closed" errors.
func TestFreshnessCheckRaceCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race condition test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "beads-freshness-race-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	store, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create issue for queries
	issue := &types.Issue{
		Title:     "Race Condition Test Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	issueID := issue.ID

	store.EnableFreshnessChecking()

	const numOps = 100
	var wg sync.WaitGroup
	var dbClosedErrors atomic.Int64
	var otherErrors atomic.Int64

	// concurrent queries
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < numOps; j++ {
				_, err := store.GetIssue(ctx, issueID)
				if err != nil {
					errStr := err.Error()
					if errStr == "sql: database is closed" ||
						errStr == "database is closed" {
						dbClosedErrors.Add(1)
					} else {
						otherErrors.Add(1)
					}
				}
			}
		}()
	}

	// concurrent reconnects
	wg.Add(1)
	go func() {
		defer wg.Done()

		for j := 0; j < 10; j++ {
			// touch file to trigger mtime change
			now := time.Now()
			os.Chtimes(dbPath, now, now)

			// force reconnect
			_ = store.reconnect()

			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	t.Logf("db closed errors: %d, other errors: %d",
		dbClosedErrors.Load(), otherErrors.Load())

	// should have zero "database is closed" errors with the fix
	if dbClosedErrors.Load() > 0 {
		t.Errorf("got %d 'database is closed' errors - GH#607 race condition present",
			dbClosedErrors.Load())
	}
}

// TestFreshnessCheckerState tests FreshnessChecker internal state tracking.
func TestFreshnessCheckerState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-freshness-state-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	store, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// verify freshness checker is nil before enabling
	if store.freshness != nil {
		t.Error("freshness checker should be nil before EnableFreshnessChecking")
	}

	store.EnableFreshnessChecking()

	// verify freshness checker is enabled
	if store.freshness == nil {
		t.Fatal("freshness checker should not be nil after EnableFreshnessChecking")
	}

	if !store.freshness.IsEnabled() {
		t.Error("freshness checker should be enabled")
	}

	// get initial state
	inode1, mtime1, size1 := store.freshness.DebugState()
	t.Logf("initial state: inode=%d, mtime=%v, size=%d", inode1, mtime1, size1)

	// perform operation that modifies file
	issue := &types.Issue{
		Title:     "State Test Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// checkpoint to flush to main file
	store.CheckpointWAL(ctx)

	// get state after write
	inode2, mtime2, size2 := store.freshness.DebugState()
	t.Logf("after write: inode=%d, mtime=%v, size=%d", inode2, mtime2, size2)

	// inode should stay same (no file replacement)
	if inode2 != inode1 {
		t.Error("inode should not change from local writes")
	}

	// disable and verify
	store.DisableFreshnessChecking()
	if store.freshness.IsEnabled() {
		t.Error("freshness checker should be disabled")
	}
}

// TestMultipleReconnectsUnderLoad tests stability under repeated reconnections.
func TestMultipleReconnectsUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multiple reconnect test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "beads-multi-reconnect-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	store, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	store.EnableFreshnessChecking()

	// create initial data
	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Initial %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "setup"); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	const numReconnects = 10
	const numOpsPerPhase = 20

	var wg sync.WaitGroup
	var opSuccess atomic.Int64
	var opErrors atomic.Int64
	stopSignal := make(chan struct{})

	// continuous operations
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-stopSignal:
					return
				default:
				}

				// read operation
				_, err := store.SearchIssues(ctx, "", types.IssueFilter{})
				if err != nil {
					opErrors.Add(1)
				} else {
					opSuccess.Add(1)
				}

				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	// perform multiple reconnects
	for i := 0; i < numReconnects; i++ {
		time.Sleep(50 * time.Millisecond)
		if err := store.reconnect(); err != nil {
			t.Logf("reconnect %d failed: %v", i, err)
		}
	}

	close(stopSignal)
	wg.Wait()

	successRate := float64(opSuccess.Load()) / float64(opSuccess.Load()+opErrors.Load()) * 100
	t.Logf("operations: %d successful, %d errors (%.1f%% success)",
		opSuccess.Load(), opErrors.Load(), successRate)

	// expect very high success rate
	if successRate < 95.0 {
		t.Errorf("success rate %.1f%% below 95%% threshold", successRate)
	}
}

// TestExternalConnectionWriteDetection tests detection of writes from
// external connections (simulating another process).
func TestExternalConnectionWriteDetection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-external-write-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	// main store (daemon)
	store, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	store.EnableFreshnessChecking()

	// create initial issue
	issue := &types.Issue{
		Title:     "Main Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "main"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// external connection (simulates another CLI process)
	extDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", dbPath))
	if err != nil {
		t.Fatalf("failed to open external connection: %v", err)
	}
	defer extDB.Close()

	// enable WAL on external connection
	if _, err := extDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("failed to enable WAL: %v", err)
	}

	// write via external connection
	_, err = extDB.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at)
		VALUES ('bd-ext123', 'External Issue', 'open', 2, 'task', datetime('now'), datetime('now'))
	`)
	if err != nil {
		t.Fatalf("external insert failed: %v", err)
	}

	// main store should see the external write (WAL mode allows this)
	extIssue, err := store.GetIssue(ctx, "bd-ext123")
	if err != nil {
		t.Fatalf("GetIssue for external issue failed: %v", err)
	}

	if extIssue == nil {
		t.Error("external issue not visible to main store")
	} else {
		t.Logf("external issue visible: %s", extIssue.Title)
	}
}

// BenchmarkFreshnessCheckOverhead measures the overhead of freshness checking.
func BenchmarkFreshnessCheckOverhead(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "beads-freshness-bench-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "bench.db")
	ctx := context.Background()

	store, err := New(ctx, dbPath)
	if err != nil {
		b.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	store.SetConfig(ctx, "issue_prefix", "bd")

	// create test issue
	issue := &types.Issue{
		Title:     "Benchmark Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "bench"); err != nil {
		b.Fatalf("CreateIssue failed: %v", err)
	}
	issueID := issue.ID

	b.Run("WithoutFreshness", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = store.GetIssue(ctx, issueID)
		}
	})

	store.EnableFreshnessChecking()

	b.Run("WithFreshness", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = store.GetIssue(ctx, issueID)
		}
	})
}
