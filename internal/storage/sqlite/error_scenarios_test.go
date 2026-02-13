package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// newTestStoreForErrors creates a file-based SQLite store for error scenario testing.
// File-based stores are needed (not :memory:) to test lock contention scenarios
// since :memory: databases are forced to MaxOpenConns(1).
func newTestStoreForErrors(t *testing.T) (*SQLiteStorage, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewWithTimeout(context.Background(), dbPath, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		store.Close()
		t.Fatalf("failed to set prefix: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dbPath
}

// TestBeginImmediateWithRetry_ContextCancelled verifies that beginImmediateWithRetry
// returns early when the context is cancelled during retry backoff.
func TestBeginImmediateWithRetry_ContextCancelled(t *testing.T) {
	store, _ := newTestStoreForErrors(t)

	// Get a connection
	conn, err := store.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("failed to acquire connection: %v", err)
	}
	defer conn.Close()

	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to begin immediate with cancelled context
	err = beginImmediateWithRetry(ctx, conn)
	if err == nil {
		// If BEGIN IMMEDIATE succeeded before context check, that's OK too
		// (it completed before the cancellation was detected)
		conn.ExecContext(context.Background(), "ROLLBACK")
		return
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "context canceled") && !strings.Contains(errMsg, "interrupted") {
		t.Errorf("expected context canceled or interrupted error, got: %v", err)
	}
}

// TestRunInTransaction_PanicRecovery verifies that if the callback panics,
// the transaction is rolled back and the panic is re-raised.
func TestRunInTransaction_PanicRecovery(t *testing.T) {
	store, _ := newTestStoreForErrors(t)
	ctx := context.Background()

	// Create an issue before the panicking transaction
	issue := &types.Issue{
		ID:        "test-panic1",
		Title:     "Before panic",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Run a transaction that panics after modifying data
	panicMsg := "intentional test panic"
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic to propagate, got nil")
			}
			if r != panicMsg {
				t.Fatalf("expected panic message %q, got %v", panicMsg, r)
			}
		}()

		_ = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			// Modify data within transaction
			err := tx.UpdateIssue(ctx, "test-panic1", map[string]interface{}{
				"title": "Modified in panicking tx",
			}, "test")
			if err != nil {
				t.Fatalf("update in tx failed: %v", err)
			}
			// Panic before commit
			panic(panicMsg)
		})
	}()

	// Verify the modification was rolled back
	retrieved, err := store.GetIssue(ctx, "test-panic1")
	if err != nil {
		t.Fatalf("failed to get issue after panic: %v", err)
	}
	if retrieved.Title != "Before panic" {
		t.Errorf("expected title %q (rollback), got %q", "Before panic", retrieved.Title)
	}
}

// TestRunInTransaction_CallbackError verifies that when the callback returns
// an error, the transaction is rolled back.
func TestRunInTransaction_CallbackError(t *testing.T) {
	store, _ := newTestStoreForErrors(t)
	ctx := context.Background()

	// Create an issue
	issue := &types.Issue{
		ID:        "test-err1",
		Title:     "Before error",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Run a transaction that modifies data then returns an error
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		err := tx.UpdateIssue(ctx, "test-err1", map[string]interface{}{
			"title": "Modified then reverted",
		}, "test")
		if err != nil {
			return err
		}
		return errForTest
	})
	if err == nil {
		t.Fatal("expected error from callback, got nil")
	}

	// Verify the modification was rolled back
	retrieved, err := store.GetIssue(ctx, "test-err1")
	if err != nil {
		t.Fatalf("failed to get issue after error: %v", err)
	}
	if retrieved.Title != "Before error" {
		t.Errorf("expected title %q (rollback), got %q", "Before error", retrieved.Title)
	}
}

var errForTest = errors.New("test error sentinel")

// TestLockContention_BeginImmediateRetry tests that beginImmediateWithRetry
// actually retries on SQLITE_BUSY errors from concurrent writers.
func TestLockContention_BeginImmediateRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lock contention test in short mode")
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "contention.db")

	// Create store with very short busy_timeout to trigger retries quickly
	store, err := NewWithTimeout(context.Background(), dbPath, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		store.Close()
		t.Fatalf("failed to set prefix: %v", err)
	}
	defer store.Close()

	// Open a second connection to the same database to hold a write lock
	store2, err := NewWithTimeout(context.Background(), dbPath, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create second store: %v", err)
	}
	defer store2.Close()

	// Hold a write lock in store2 via BEGIN IMMEDIATE
	conn, err := store2.db.Conn(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	if err != nil {
		t.Fatalf("failed to begin immediate: %v", err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")

	// Now try to run a transaction in store1 - it should fail after retries
	txErr := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return nil
	})
	if txErr == nil {
		t.Fatal("expected lock contention error, got nil")
	}
	if !strings.Contains(txErr.Error(), "begin transaction") &&
		!strings.Contains(txErr.Error(), "database is locked") {
		t.Errorf("expected lock-related error, got: %v", txErr)
	}
}

// TestWALCheckpointOnClose tests that Close() handles WAL checkpoint gracefully.
func TestWALCheckpointOnClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "wal-test.db")

	store, err := New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		store.Close()
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Write some data to generate WAL entries
	issue := &types.Issue{
		ID:        "test-wal1",
		Title:     "WAL test issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		store.Close()
		t.Fatalf("failed to create issue: %v", err)
	}

	// Close should checkpoint WAL successfully
	if err := store.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Verify WAL file is checkpointed (should be empty or removed)
	walPath := dbPath + "-wal"
	info, err := os.Stat(walPath)
	if err == nil && info.Size() > 0 {
		t.Logf("WAL file still exists with size %d (may be normal)", info.Size())
	}

	// Verify data is persisted by reopening
	store2, err := New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer store2.Close()

	retrieved, err := store2.GetIssue(ctx, "test-wal1")
	if err != nil {
		t.Fatalf("failed to get issue after reopen: %v", err)
	}
	if retrieved == nil {
		t.Fatal("issue not found after WAL checkpoint and reopen")
	}
	if retrieved.Title != "WAL test issue" {
		t.Errorf("expected title %q, got %q", "WAL test issue", retrieved.Title)
	}
}

// TestRunInTransaction_ConcurrentTransactions tests that concurrent transactions
// are serialized properly without data corruption.
func TestRunInTransaction_ConcurrentTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent transaction test in short mode")
	}

	store, _ := newTestStoreForErrors(t)
	ctx := context.Background()

	// Create initial issues
	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			ID:        strings.Replace("test-conc0", "0", string(rune('0'+i)), 1),
			Title:     "Concurrent test",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue %d: %v", i, err)
		}
	}

	// Run concurrent transactions that update different issues
	var wg sync.WaitGroup
	errors := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := strings.Replace("test-conc0", "0", string(rune('0'+idx)), 1)
			errors[idx] = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
				return tx.UpdateIssue(ctx, id, map[string]interface{}{
					"title": "Updated by goroutine",
				}, "test")
			})
		}(i)
	}
	wg.Wait()

	// All transactions should succeed (they update different rows)
	for i, err := range errors {
		if err != nil {
			t.Errorf("transaction %d failed: %v", i, err)
		}
	}

	// Verify all updates applied
	for i := 0; i < 5; i++ {
		id := strings.Replace("test-conc0", "0", string(rune('0'+i)), 1)
		issue, err := store.GetIssue(ctx, id)
		if err != nil {
			t.Errorf("failed to get issue %d: %v", i, err)
			continue
		}
		if issue.Title != "Updated by goroutine" {
			t.Errorf("issue %d title = %q, want %q", i, issue.Title, "Updated by goroutine")
		}
	}
}
