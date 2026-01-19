//go:build cgo

package dolt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// Bootstrap Lock Tests
// =============================================================================

// TestBootstrapLockAcquisition tests basic lock acquisition and release
func TestBootstrapLockAcquisition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lock-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire lock
	f, err := acquireBootstrapLock(lockPath, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	if f == nil {
		t.Fatal("lock file should not be nil")
	}

	// Verify lock file exists
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("lock file should exist after acquisition")
	}

	// Release lock
	releaseBootstrapLock(f, lockPath)

	// Verify lock file is removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file should be removed after release")
	}
}

// TestBootstrapLockBlocking tests that locks properly block concurrent access
func TestBootstrapLockBlocking(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lock-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// First lock holder acquires the lock
	f1, err := acquireBootstrapLock(lockPath, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire first lock: %v", err)
	}

	// Try to acquire lock in goroutine - it should block
	acquired := make(chan struct{})
	go func() {
		f2, err := acquireBootstrapLock(lockPath, 3*time.Second)
		if err == nil && f2 != nil {
			close(acquired)
			// Hold briefly then release
			time.Sleep(100 * time.Millisecond)
			releaseBootstrapLock(f2, lockPath)
		}
	}()

	// Wait a bit to ensure second goroutine is waiting
	time.Sleep(200 * time.Millisecond)

	// Verify second goroutine hasn't acquired lock yet
	select {
	case <-acquired:
		t.Error("second lock should not have been acquired while first is held")
	default:
		t.Log("second lock correctly blocked")
	}

	// Release first lock
	releaseBootstrapLock(f1, lockPath)

	// Now second goroutine should acquire the lock
	select {
	case <-acquired:
		t.Log("second lock acquired after first released")
	case <-time.After(2 * time.Second):
		t.Error("second lock failed to acquire after first released")
	}
}

// TestBootstrapLockConcurrent tests concurrent lock acquisition
func TestBootstrapLockConcurrent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lock-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	const numGoroutines = 10
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var timeoutCount atomic.Int32

	// Launch multiple goroutines trying to acquire the lock
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			f, err := acquireBootstrapLock(lockPath, 2*time.Second)
			if err != nil {
				timeoutCount.Add(1)
				t.Logf("goroutine %d: lock timeout (expected under contention)", n)
				return
			}
			successCount.Add(1)
			// Hold lock briefly
			time.Sleep(100 * time.Millisecond)
			releaseBootstrapLock(f, lockPath)
		}(i)
	}

	wg.Wait()

	// At least one should succeed
	if successCount.Load() == 0 {
		t.Error("expected at least one goroutine to acquire lock")
	}

	// Most should timeout due to contention
	if timeoutCount.Load() == 0 {
		t.Log("warning: expected some timeouts under high contention")
	}

	t.Logf("Results: %d succeeded, %d timed out", successCount.Load(), timeoutCount.Load())
}

// TestBootstrapLockSequential tests sequential lock acquisition and release
func TestBootstrapLockSequential(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lock-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire and release lock multiple times sequentially
	for i := 0; i < 5; i++ {
		f, err := acquireBootstrapLock(lockPath, 2*time.Second)
		if err != nil {
			t.Fatalf("iteration %d: failed to acquire lock: %v", i, err)
		}
		if f == nil {
			t.Fatalf("iteration %d: lock file is nil", i)
		}

		// Verify lock is held
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			t.Errorf("iteration %d: lock file should exist", i)
		}

		releaseBootstrapLock(f, lockPath)

		// Verify lock is released
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Errorf("iteration %d: lock file should be removed", i)
		}
	}
}

// TestBootstrapLockDoubleRelease tests that releasing a lock twice is safe
func TestBootstrapLockDoubleRelease(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lock-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	f, err := acquireBootstrapLock(lockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Release once
	releaseBootstrapLock(f, lockPath)

	// Release again - should not panic
	releaseBootstrapLock(f, lockPath)

	// Release with nil - should not panic
	releaseBootstrapLock(nil, lockPath)
}

// TestBootstrapLockWithNilFile tests releasing a nil lock file
func TestBootstrapLockWithNilFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lock-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Should not panic
	releaseBootstrapLock(nil, lockPath)
}

// =============================================================================
// Context Timeout Tests
// =============================================================================

// TestContextTimeoutOnQuery tests query with context timeout
func TestContextTimeoutOnQuery(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create test issue
	issue := &types.Issue{
		ID:          "test-ctx-timeout",
		Title:       "Context Timeout Test",
		Description: "Test issue",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	ctx := context.Background()
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Use very short timeout for query
	shortCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for context to expire
	time.Sleep(10 * time.Millisecond)

	// Query should fail with context timeout
	_, err := store.GetIssue(shortCtx, issue.ID)
	if err == nil {
		t.Error("expected error due to context timeout")
	}
	if err != nil {
		t.Logf("got expected error: %v", err)
	}
}

// TestContextCancellationDuringOperation tests canceling context during operation
func TestContextCancellationDuringOperation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	// Operation should fail
	issue := &types.Issue{
		ID:          "test-cancelled",
		Title:       "Cancelled Test",
		Description: "Should not be created",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	err := store.CreateIssue(ctx, issue, "tester")
	if err == nil {
		t.Error("expected error due to cancelled context")
	}
	t.Logf("got expected error: %v", err)
}

// TestContextTimeoutOnTransaction tests transaction with context timeout
func TestContextTimeoutOnTransaction(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create test issue
	issue := &types.Issue{
		ID:          "test-tx-timeout",
		Title:       "Transaction Timeout Test",
		Description: "Test issue",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Use short timeout for transaction
	shortCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for context to expire
	time.Sleep(10 * time.Millisecond)

	// Transaction should fail
	err := store.RunInTransaction(shortCtx, func(tx storage.Transaction) error {
		return tx.UpdateIssue(shortCtx, issue.ID, map[string]interface{}{
			"description": "Updated",
		}, "tester")
	})

	if err == nil {
		t.Error("expected error due to context timeout")
	}
	t.Logf("got expected error: %v", err)
}

// TestContextTimeoutOnCommit tests commit operation with timeout
func TestContextTimeoutOnCommit(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create and commit an issue normally first
	issue := &types.Issue{
		ID:          "test-commit-timeout",
		Title:       "Commit Timeout Test",
		Description: "Test issue",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Use short timeout for commit
	shortCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for context to expire
	time.Sleep(10 * time.Millisecond)

	// Commit should fail
	err := store.Commit(shortCtx, "Test commit")
	if err == nil {
		t.Log("commit succeeded despite short timeout (may be too fast to timeout)")
	} else {
		t.Logf("got expected error: %v", err)
	}
}

// =============================================================================
// Transaction Lock Tests
// =============================================================================

// TestTransactionSerializability tests that transactions are serializable
func TestTransactionSerializability(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create test issue
	issue := &types.Issue{
		ID:          "test-serializable",
		Title:       "Serializable Test",
		Description: "Initial value",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	var wg sync.WaitGroup
	var tx1Done, tx2Done atomic.Bool
	errors := make(chan error, 2)

	// Transaction 1: Read, sleep, update
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			// Read current value
			issue, err := tx.GetIssue(ctx, "test-serializable")
			if err != nil {
				return err
			}

			// Sleep to allow contention
			time.Sleep(100 * time.Millisecond)

			// Update
			return tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
				"description": "Updated by TX1",
			}, "tx1")
		})
		tx1Done.Store(true)
		if err != nil {
			errors <- fmt.Errorf("tx1: %w", err)
		}
	}()

	// Transaction 2: Read, update
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait a bit to ensure TX1 starts first
		time.Sleep(50 * time.Millisecond)

		err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			// Read current value
			issue, err := tx.GetIssue(ctx, "test-serializable")
			if err != nil {
				return err
			}

			// Update
			return tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
				"description": "Updated by TX2",
			}, "tx2")
		})
		tx2Done.Store(true)
		if err != nil {
			errors <- fmt.Errorf("tx2: %w", err)
		}
	}()

	wg.Wait()
	close(errors)

	// Check errors
	errCount := 0
	for err := range errors {
		t.Logf("transaction error: %v", err)
		errCount++
	}

	// At least one transaction should complete
	if !tx1Done.Load() && !tx2Done.Load() {
		t.Fatal("both transactions failed")
	}

	// Verify final state is consistent
	final, err := store.GetIssue(ctx, "test-serializable")
	if err != nil {
		t.Fatalf("failed to get final state: %v", err)
	}
	if final == nil {
		t.Fatal("issue not found")
	}

	t.Logf("Final description: %q", final.Description)
}

// TestTransactionRollback tests transaction rollback on error
func TestTransactionRollback(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create test issue
	issue := &types.Issue{
		ID:          "test-rollback",
		Title:       "Rollback Test",
		Description: "Original value",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Transaction that updates then fails
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Update
		if err := tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
			"description": "This should be rolled back",
		}, "tester"); err != nil {
			return err
		}

		// Force error
		return fmt.Errorf("intentional error to trigger rollback")
	})

	if err == nil {
		t.Fatal("expected transaction to fail")
	}

	// Verify original value is preserved
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if retrieved.Description != "Original value" {
		t.Errorf("expected description 'Original value', got %q (rollback failed)", retrieved.Description)
	}
}

// =============================================================================
// RWMutex Tests
// =============================================================================

// TestStoreRWMutexConcurrentReads tests concurrent reads with RWMutex
func TestStoreRWMutexConcurrentReads(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create test issue
	issue := &types.Issue{
		ID:          "test-rwmutex-reads",
		Title:       "RWMutex Read Test",
		Description: "Test concurrent reads",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	const numReaders = 20
	const readsPerReader = 50

	var wg sync.WaitGroup
	var successfulReads atomic.Int32
	var failedReads atomic.Int32

	// Launch concurrent readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				_, err := store.GetIssue(ctx, issue.ID)
				if err != nil {
					failedReads.Add(1)
				} else {
					successfulReads.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	totalReads := numReaders * readsPerReader
	t.Logf("Concurrent reads: %d successful, %d failed out of %d total",
		successfulReads.Load(), failedReads.Load(), totalReads)

	// Most reads should succeed
	if successfulReads.Load() < int32(totalReads)*9/10 {
		t.Errorf("too many failed reads: %d/%d", failedReads.Load(), totalReads)
	}
}

// =============================================================================
// Long-Running Operation Tests
// =============================================================================

// TestLongRunningTransaction tests behavior with long-running transactions
func TestLongRunningTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running test in short mode")
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create test issue
	issue := &types.Issue{
		ID:          "test-long-tx",
		Title:       "Long Transaction Test",
		Description: "Test long-running transaction",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Start long transaction
	longTxDone := make(chan error)
	go func() {
		err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			// Hold transaction for a while
			time.Sleep(2 * time.Second)
			return tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
				"description": "Updated by long tx",
			}, "long-tx")
		})
		longTxDone <- err
	}()

	// Wait a bit, then try short transaction
	time.Sleep(500 * time.Millisecond)

	shortCtx, shortCancel := context.WithTimeout(ctx, 3*time.Second)
	defer shortCancel()

	shortErr := store.RunInTransaction(shortCtx, func(tx storage.Transaction) error {
		return tx.UpdateIssue(shortCtx, issue.ID, map[string]interface{}{
			"notes": "Updated by short tx",
		}, "short-tx")
	})

	t.Logf("Short transaction result: %v", shortErr)

	// Wait for long transaction
	longErr := <-longTxDone
	t.Logf("Long transaction result: %v", longErr)

	// At least one should succeed
	if longErr != nil && shortErr != nil {
		t.Error("both transactions failed")
	}

	// Verify final state
	final, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get final state: %v", err)
	}
	if final == nil {
		t.Fatal("issue not found")
	}
	t.Logf("Final state: description=%q, notes=%q", final.Description, final.Notes)
}

// TestFileBasedLockUnlock tests the underlying file lock mechanism
func TestFileBasedLockUnlock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lock-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "flock.lock")

	// Create and lock file
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create lock file: %v", err)
	}

	// Acquire exclusive lock
	if err := lockfile.FlockExclusiveBlocking(f); err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Try to acquire lock again from same process - should succeed
	// (locks are per process, not per file descriptor)
	if err := lockfile.FlockExclusiveBlocking(f); err != nil {
		t.Fatalf("failed to re-acquire lock: %v", err)
	}

	// Unlock
	if err := lockfile.FlockUnlock(f); err != nil {
		t.Fatalf("failed to unlock: %v", err)
	}

	// Close file
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	// Clean up
	os.Remove(lockPath)
}

// =============================================================================
// Lock Retry Tests (rig-358fc7)
// =============================================================================

// TestLockRetryConfiguration tests that retry configuration is properly applied
func TestLockRetryConfiguration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dolt-retry-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Test with custom retry configuration
	cfg := &Config{
		Path:           tmpDir,
		Database:       "test",
		LockRetries:    5,
		LockRetryDelay: 50 * time.Millisecond,
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify store was created successfully
	if store == nil {
		t.Fatal("store should not be nil")
	}
}

// TestLockRetryDefaults tests that default retry values are applied
func TestLockRetryDefaults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dolt-retry-defaults-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Create store without specifying retry config
	cfg := &Config{
		Path:     tmpDir,
		Database: "test",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify defaults were applied (no direct way to check, but creation should succeed)
	if store == nil {
		t.Fatal("store should not be nil")
	}
}

// TestIsLockError tests the lock error detection function
func TestIsLockError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "database is read only",
			err:      fmt.Errorf("database is read only"),
			expected: true,
		},
		{
			name:     "database is locked",
			err:      fmt.Errorf("database is locked"),
			expected: true,
		},
		{
			name:     "lock timeout",
			err:      fmt.Errorf("lock timeout exceeded"),
			expected: true,
		},
		{
			name:     "lock contention",
			err:      fmt.Errorf("lock contention detected"),
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      fmt.Errorf("connection refused"),
			expected: false,
		},
		{
			name:     "uppercase lock error",
			err:      fmt.Errorf("DATABASE IS LOCKED"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLockError(tt.err)
			if result != tt.expected {
				t.Errorf("isLockError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}
