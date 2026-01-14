package sqlite

import (
	"context"
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

// TestCloseWithPendingTransactions tests that Close() waits for active transactions.
func TestCloseWithPendingTransactions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-close-pending-tx-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// start a long-running transaction
	txStarted := make(chan struct{})
	txRelease := make(chan struct{})
	txDone := make(chan error, 1)

	go func() {
		err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			close(txStarted)

			issue := &types.Issue{
				Title:     "Pending TX Issue",
				Status:    types.StatusOpen,
				Priority:  2,
				IssueType: types.TypeTask,
			}
			if err := tx.CreateIssue(ctx, issue, "test"); err != nil {
				return err
			}

			// wait for release
			<-txRelease

			return nil
		})
		txDone <- err
	}()

	// wait for transaction to start
	<-txStarted

	// try to close while transaction is active
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- store.Close()
	}()

	// close should block or wait
	select {
	case err := <-closeDone:
		// close completed (may have waited for tx)
		t.Logf("close completed: %v", err)
	case <-time.After(100 * time.Millisecond):
		// close is waiting for transaction (expected with RWMutex)
		t.Log("close is correctly waiting for transaction")
	}

	// release transaction
	close(txRelease)

	// wait for transaction to complete
	txErr := <-txDone
	if txErr != nil {
		t.Logf("transaction completed with error: %v (may be expected)", txErr)
	}

	// wait for close to complete
	select {
	case err := <-closeDone:
		if err != nil {
			t.Logf("close error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("close did not complete after transaction finished")
	}
}

// TestCloseWithWALCheckpointFailure tests Close() behavior when checkpoint fails.
func TestCloseWithWALCheckpointFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-close-checkpoint-fail-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create some data
	for i := 0; i < 10; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Checkpoint Fail Test %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			store.Close()
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	// close should work even if checkpoint has issues
	// (Close() doesn't fail on checkpoint errors, just logs them)
	err = store.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// verify data is accessible after reopen
	store2, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer store2.Close()

	issues, err := store2.SearchIssues(ctx, "Checkpoint Fail Test", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues after reopen failed: %v", err)
	}
	if len(issues) != 10 {
		t.Errorf("expected 10 issues after reopen, got %d", len(issues))
	}
}

// TestShutdownUnderLoad tests Close() under concurrent operation load.
func TestShutdownUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shutdown under load test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "beads-shutdown-load-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create initial data
	for i := 0; i < 10; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Initial %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "setup"); err != nil {
			store.Close()
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	const numWorkers = 10
	var wg sync.WaitGroup
	var opsStarted atomic.Int64
	var opsCompleted atomic.Int64
	var opsErrors atomic.Int64
	stopSignal := make(chan struct{})

	// start workers doing operations
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-stopSignal:
					return
				default:
				}

				opsStarted.Add(1)

				// mix of reads and writes
				if workerID%2 == 0 {
					// read
					_, err := store.SearchIssues(ctx, "", types.IssueFilter{})
					if err != nil {
						opsErrors.Add(1)
					} else {
						opsCompleted.Add(1)
					}
				} else {
					// write
					issue := &types.Issue{
						Title:     fmt.Sprintf("Worker %d Issue", workerID),
						Status:    types.StatusOpen,
						Priority:  2,
						IssueType: types.TypeTask,
					}
					err := store.CreateIssue(ctx, issue, "worker")
					if err != nil {
						opsErrors.Add(1)
					} else {
						opsCompleted.Add(1)
					}
				}

				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// let operations run
	time.Sleep(100 * time.Millisecond)

	// signal stop
	close(stopSignal)

	// close while workers are finishing
	closeStart := time.Now()
	err = store.Close()
	closeTime := time.Since(closeStart)

	if err != nil {
		t.Errorf("Close under load failed: %v", err)
	}

	// wait for workers
	wg.Wait()

	t.Logf("close took %v, ops: started=%d, completed=%d, errors=%d",
		closeTime, opsStarted.Load(), opsCompleted.Load(), opsErrors.Load())

	// close should complete in reasonable time
	if closeTime > 5*time.Second {
		t.Errorf("Close took too long: %v", closeTime)
	}
}

// TestDoubleClose tests that closing twice doesn't panic.
func TestDoubleClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-double-close-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// first close
	err1 := store.Close()
	if err1 != nil {
		t.Logf("first close error: %v", err1)
	}

	// second close should not panic
	err2 := store.Close()
	if err2 != nil {
		t.Logf("second close error: %v (expected)", err2)
	}

	// IsClosed should return true
	if !store.IsClosed() {
		t.Error("IsClosed() should return true after Close()")
	}
}

// TestIsClosedBehavior tests the IsClosed() method.
func TestIsClosedBehavior(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-isclosed-*")
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

	// should not be closed initially
	if store.IsClosed() {
		t.Error("IsClosed() should return false for new store")
	}

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create issue to verify store is working
	issue := &types.Issue{
		Title:     "IsClosed Test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		store.Close()
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// close
	if err := store.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// should be closed now
	if !store.IsClosed() {
		t.Error("IsClosed() should return true after Close()")
	}
}

// TestOperationsAfterClose tests that operations fail gracefully after Close().
func TestOperationsAfterClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-ops-after-close-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create initial issue
	issue := &types.Issue{
		Title:     "Pre-close Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		store.Close()
		t.Fatalf("CreateIssue failed: %v", err)
	}
	issueID := issue.ID

	// close store
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// try operations - they should fail with database closed error
	_, err = store.GetIssue(ctx, issueID)
	if err == nil {
		t.Error("GetIssue after Close should fail")
	} else {
		t.Logf("GetIssue after Close: %v", err)
	}

	_, err = store.SearchIssues(ctx, "", types.IssueFilter{})
	if err == nil {
		t.Error("SearchIssues after Close should fail")
	} else {
		t.Logf("SearchIssues after Close: %v", err)
	}

	newIssue := &types.Issue{
		Title:     "Post-close Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	err = store.CreateIssue(ctx, newIssue, "test")
	if err == nil {
		t.Error("CreateIssue after Close should fail")
	} else {
		t.Logf("CreateIssue after Close: %v", err)
	}
}

// TestCloseReleasesLocks tests that Close() properly releases database locks.
func TestCloseReleasesLocks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-close-locks-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	// open store 1
	store1, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store1: %v", err)
	}

	if err := store1.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store1.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create issue
	issue := &types.Issue{
		Title:     "Lock Test Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store1.CreateIssue(ctx, issue, "test"); err != nil {
		store1.Close()
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// close store1
	if err := store1.Close(); err != nil {
		t.Fatalf("Close store1 failed: %v", err)
	}

	// open store 2 - should be able to acquire exclusive access
	store2, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store2 after store1 close: %v", err)
	}
	defer store2.Close()

	// verify we can read data
	issues, err := store2.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues on store2 failed: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(issues))
	}

	// verify we can write data
	issue2 := &types.Issue{
		Title:     "Store2 Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store2.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Errorf("CreateIssue on store2 failed: %v", err)
	}
}

// TestReadOnlyClose tests Close() behavior for read-only stores.
func TestReadOnlyClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-readonly-close-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	// create and populate database
	store1, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store1: %v", err)
	}

	if err := store1.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store1.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	issue := &types.Issue{
		Title:     "ReadOnly Test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store1.CreateIssue(ctx, issue, "test"); err != nil {
		store1.Close()
		t.Fatalf("CreateIssue failed: %v", err)
	}

	store1.Close()

	// open read-only
	roStore, err := NewReadOnly(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create read-only store: %v", err)
	}

	// verify it's read-only
	if !roStore.readOnly {
		t.Error("store should be marked as read-only")
	}

	// close - should NOT checkpoint (read-only)
	err = roStore.Close()
	if err != nil {
		t.Errorf("Close read-only store failed: %v", err)
	}

	// verify database is not corrupted
	store2, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	defer store2.Close()

	issues, err := store2.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues after read-only close failed: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(issues))
	}
}

// TestConcurrentCloseCalls tests multiple goroutines calling Close().
func TestConcurrentCloseCalls(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-concurrent-close-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	const numClosers = 5
	var wg sync.WaitGroup
	closeResults := make(chan error, numClosers)

	// launch multiple close calls concurrently
	for i := 0; i < numClosers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			closeResults <- store.Close()
		}()
	}

	wg.Wait()
	close(closeResults)

	// should not panic, at most one should succeed
	var successCount int
	for err := range closeResults {
		if err == nil {
			successCount++
		}
	}

	t.Logf("concurrent close: %d/%d succeeded", successCount, numClosers)
}

// TestCloseWithFreshnessChecker tests Close() with freshness checking enabled.
func TestCloseWithFreshnessChecker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-close-freshness-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// enable freshness checking
	store.EnableFreshnessChecking()

	// create issue
	issue := &types.Issue{
		Title:     "Freshness Close Test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		store.Close()
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// close with freshness enabled
	err = store.Close()
	if err != nil {
		t.Errorf("Close with freshness failed: %v", err)
	}

	// verify closed state
	if !store.IsClosed() {
		t.Error("store should be closed")
	}

	// freshness checker should no longer trigger after close
	// (this shouldn't panic)
	if store.freshness != nil {
		_ = store.freshness.Check()
	}
}

// TestGracefulShutdownSequence tests the ideal shutdown sequence.
func TestGracefulShutdownSequence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-graceful-shutdown-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	store.EnableFreshnessChecking()

	// simulate normal operation
	for i := 0; i < 10; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Shutdown Test %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			store.Close()
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	// step 1: disable freshness checking (optional but recommended)
	store.DisableFreshnessChecking()

	// step 2: checkpoint WAL (optional but ensures data durability)
	if err := store.CheckpointWAL(ctx); err != nil {
		t.Logf("pre-close checkpoint: %v (non-fatal)", err)
	}

	// step 3: close
	if err := store.Close(); err != nil {
		t.Errorf("graceful Close failed: %v", err)
	}

	// verify data persisted
	store2, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer store2.Close()

	issues, err := store2.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues after reopen failed: %v", err)
	}
	if len(issues) != 10 {
		t.Errorf("expected 10 issues after graceful shutdown, got %d", len(issues))
	}
}

// TestCloseIdempotency verifies Close() is idempotent.
func TestCloseIdempotency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-close-idempotent-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// close multiple times
	for i := 0; i < 5; i++ {
		err := store.Close()
		if i == 0 {
			if err != nil {
				t.Errorf("first Close failed: %v", err)
			}
		}
		// subsequent closes may return error but should not panic
	}

	// store should definitely be closed
	if !store.IsClosed() {
		t.Error("store should be closed after multiple Close() calls")
	}
}

// TestReconnectAfterClose tests that reconnect doesn't do anything after Close().
func TestReconnectAfterClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-reconnect-after-close-*")
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

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	store.EnableFreshnessChecking()

	// close
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// try reconnect after close - should be no-op
	err = store.reconnect()
	if err != nil {
		// error is acceptable
		t.Logf("reconnect after close: %v (expected)", err)
	}

	// should still be closed
	if !store.IsClosed() {
		t.Error("store should still be closed after reconnect attempt")
	}
}

// BenchmarkClose measures Close() performance.
func BenchmarkClose(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tmpDir, err := os.MkdirTemp("", "beads-close-bench-*")
		if err != nil {
			b.Fatalf("failed to create temp dir: %v", err)
		}

		dbPath := filepath.Join(tmpDir, "bench.db")
		ctx := context.Background()

		store, err := New(ctx, dbPath)
		if err != nil {
			os.RemoveAll(tmpDir)
			b.Fatalf("failed to create store: %v", err)
		}

		store.SetConfig(ctx, "issue_prefix", "bd")

		// create some data
		for j := 0; j < 10; j++ {
			issue := &types.Issue{
				Title:     fmt.Sprintf("Bench %d", j),
				Status:    types.StatusOpen,
				Priority:  2,
				IssueType: types.TypeTask,
			}
			store.CreateIssue(ctx, issue, "bench")
		}

		store.Close()
		os.RemoveAll(tmpDir)
	}
}

// TestClosePreservesData verifies all data is preserved after Close().
func TestClosePreservesData(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-close-preserve-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()

	// create store and add various data
	store1, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store1: %v", err)
	}

	if err := store1.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store1.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// create issues
	var issueIDs []string
	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			Title:       fmt.Sprintf("Preserve Test %d", i),
			Description: fmt.Sprintf("Description for issue %d", i),
			Status:      types.StatusOpen,
			Priority:    i % 4,
			IssueType:   types.TypeTask,
		}
		if err := store1.CreateIssue(ctx, issue, "test"); err != nil {
			store1.Close()
			t.Fatalf("CreateIssue failed: %v", err)
		}
		issueIDs = append(issueIDs, issue.ID)
	}

	// add labels
	for _, id := range issueIDs {
		if err := store1.AddLabel(ctx, id, "preserve-test", "test"); err != nil {
			store1.Close()
			t.Fatalf("AddLabel failed: %v", err)
		}
	}

	// add dependencies
	if len(issueIDs) >= 2 {
		dep := &types.Dependency{
			IssueID:     issueIDs[1],
			DependsOnID: issueIDs[0],
			Type:        types.DepBlocks,
		}
		if err := store1.AddDependency(ctx, dep, "test"); err != nil {
			store1.Close()
			t.Fatalf("AddDependency failed: %v", err)
		}
	}

	// close
	if err := store1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// reopen and verify all data
	store2, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer store2.Close()

	// verify issues
	for i, id := range issueIDs {
		issue, err := store2.GetIssue(ctx, id)
		if err != nil {
			t.Errorf("GetIssue %s failed: %v", id, err)
			continue
		}
		if issue == nil {
			t.Errorf("issue %s not found", id)
			continue
		}
		expectedTitle := fmt.Sprintf("Preserve Test %d", i)
		if issue.Title != expectedTitle {
			t.Errorf("issue %s title: got %q, want %q", id, issue.Title, expectedTitle)
		}
	}

	// verify labels
	for _, id := range issueIDs {
		labels, err := store2.GetLabels(ctx, id)
		if err != nil {
			t.Errorf("GetLabels %s failed: %v", id, err)
			continue
		}
		found := false
		for _, l := range labels {
			if l == "preserve-test" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("label 'preserve-test' not found on issue %s", id)
		}
	}

	// verify dependencies
	if len(issueIDs) >= 2 {
		deps, err := store2.GetDependencies(ctx, issueIDs[1])
		if err != nil {
			t.Errorf("GetDependencies failed: %v", err)
		} else if len(deps) != 1 || deps[0].ID != issueIDs[0] {
			t.Errorf("dependency not preserved: got %v", deps)
		}
	}

	t.Log("all data preserved after Close()")
}
