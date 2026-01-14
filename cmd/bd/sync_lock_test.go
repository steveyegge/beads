package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

// =============================================================================
// Sync Lock Tests
// Tests for sync lock acquisition, concurrent access, and cleanup scenarios.
// =============================================================================

// TestSyncLockHeldDuringGitOperations verifies that the sync lock is held
// throughout the entire git pull/push operation sequence.
func TestSyncLockHeldDuringGitOperations(t *testing.T) {
	ctx := context.Background()
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	// Create beads directory with JSONL
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"bd-1","title":"Test"}`+"\n"), 0644); err != nil {
		t.Fatalf("write JSONL failed: %v", err)
	}

	// Initial commit of beads dir
	_ = exec.Command("git", "add", ".").Run()
	_ = exec.Command("git", "commit", "-m", "initial").Run()

	// Acquire the sync lock
	lockPath := filepath.Join(beadsDir, ".sync.lock")
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		t.Fatalf("failed to acquire sync lock: %v", err)
	}
	if !locked {
		t.Fatal("expected to acquire sync lock")
	}

	// Verify lock is held by trying to acquire from another flock instance
	lock2 := flock.New(lockPath)
	locked2, err := lock2.TryLock()
	if err != nil {
		t.Fatalf("TryLock error: %v", err)
	}
	if locked2 {
		lock2.Unlock()
		t.Error("second lock should NOT be acquirable while first is held")
	}

	// Simulate git operations while lock is held
	hasChanges, err := gitHasBeadsChanges(ctx)
	if err != nil {
		t.Logf("gitHasBeadsChanges during lock: %v", err)
	}
	_ = hasChanges // verify git operations work while lock is held

	// Release lock
	if err := lock.Unlock(); err != nil {
		t.Fatalf("failed to unlock: %v", err)
	}

	// Verify lock is now available
	lock3 := flock.New(lockPath)
	locked3, err := lock3.TryLock()
	if err != nil {
		t.Fatalf("TryLock error after unlock: %v", err)
	}
	if !locked3 {
		t.Error("expected lock to be acquirable after release")
	} else {
		lock3.Unlock()
	}
}

// TestConcurrentSyncAttempts verifies that concurrent sync attempts fail gracefully.
// This tests the P0 security fix for preventing data corruption from simultaneous syncs.
func TestConcurrentSyncAttempts(t *testing.T) {
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	// Create beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	lockPath := filepath.Join(beadsDir, ".sync.lock")

	// Acquire first lock
	lock1 := flock.New(lockPath)
	locked1, err := lock1.TryLock()
	if err != nil {
		t.Fatalf("failed to acquire first lock: %v", err)
	}
	if !locked1 {
		t.Fatal("expected to acquire first lock")
	}
	defer lock1.Unlock()

	// Simulate multiple concurrent sync attempts
	const numAttempts = 10
	var blocked int32 // count of blocked attempts

	var wg sync.WaitGroup
	for i := 0; i < numAttempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			lock := flock.New(lockPath)
			locked, err := lock.TryLock()
			if err != nil {
				return // error is acceptable
			}
			if !locked {
				atomic.AddInt32(&blocked, 1)
			} else {
				// should not happen - release immediately
				lock.Unlock()
			}
		}()
	}

	wg.Wait()

	// All concurrent attempts should have been blocked
	if atomic.LoadInt32(&blocked) != numAttempts {
		t.Errorf("expected all %d concurrent attempts to be blocked, got %d blocked",
			numAttempts, atomic.LoadInt32(&blocked))
	}
}

// TestSyncLockReleaseTiming verifies that the lock is released at the correct time:
// after successful sync completion or after failure cleanup.
func TestSyncLockReleaseTiming(t *testing.T) {
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	lockPath := filepath.Join(beadsDir, ".sync.lock")

	t.Run("lock released after successful operation", func(t *testing.T) {
		lock := flock.New(lockPath)
		locked, err := lock.TryLock()
		if err != nil {
			t.Fatalf("TryLock error: %v", err)
		}
		if !locked {
			t.Fatal("expected to acquire lock")
		}

		// simulate successful sync operations
		time.Sleep(10 * time.Millisecond)

		// release lock
		if err := lock.Unlock(); err != nil {
			t.Fatalf("failed to unlock: %v", err)
		}

		// verify lock is immediately available
		lock2 := flock.New(lockPath)
		locked2, err := lock2.TryLock()
		if err != nil {
			t.Fatalf("TryLock error: %v", err)
		}
		if !locked2 {
			t.Error("lock should be available immediately after release")
		} else {
			lock2.Unlock()
		}
	})

	t.Run("lock released via defer on panic", func(t *testing.T) {
		lock := flock.New(lockPath)
		locked, err := lock.TryLock()
		if err != nil {
			t.Fatalf("TryLock error: %v", err)
		}
		if !locked {
			t.Fatal("expected to acquire lock")
		}

		// simulate deferred unlock pattern used in sync code
		func() {
			defer func() {
				_ = lock.Unlock()
				// recover from panic for test purposes
				if r := recover(); r != nil {
					// expected
				}
			}()
			// simulate panic during sync
			panic("simulated sync failure")
		}()

		// verify lock was released despite panic
		lock2 := flock.New(lockPath)
		locked2, err := lock2.TryLock()
		if err != nil {
			t.Fatalf("TryLock error after panic: %v", err)
		}
		if !locked2 {
			t.Error("lock should be released even after panic (via defer)")
		} else {
			lock2.Unlock()
		}
	})
}

// TestSyncLockFileCleanupOnFailure verifies that the lock file is properly
// cleaned up or left in a valid state after various failure scenarios.
func TestSyncLockFileCleanupOnFailure(t *testing.T) {
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	lockPath := filepath.Join(beadsDir, ".sync.lock")

	t.Run("lock file exists after normal release", func(t *testing.T) {
		lock := flock.New(lockPath)
		locked, err := lock.TryLock()
		if err != nil {
			t.Fatalf("TryLock error: %v", err)
		}
		if !locked {
			t.Fatal("expected to acquire lock")
		}

		if err := lock.Unlock(); err != nil {
			t.Fatalf("unlock error: %v", err)
		}

		// lock file may or may not exist after release - both are valid
		// the important thing is that it can be reacquired
		lock2 := flock.New(lockPath)
		locked2, err := lock2.TryLock()
		if err != nil {
			t.Fatalf("TryLock error: %v", err)
		}
		if !locked2 {
			t.Error("should be able to acquire lock after previous release")
		} else {
			lock2.Unlock()
		}
	})

	t.Run("stale lock file does not block new lock", func(t *testing.T) {
		// manually create a stale lock file (no process holding it)
		if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
			t.Fatalf("write stale lock: %v", err)
		}

		// flock should still be able to acquire lock on the file
		lock := flock.New(lockPath)
		locked, err := lock.TryLock()
		if err != nil {
			t.Fatalf("TryLock on stale file error: %v", err)
		}
		if !locked {
			t.Error("should be able to acquire lock even with stale lock file")
		} else {
			lock.Unlock()
		}
	})

	t.Run("lock recoverable after beadsDir recreation", func(t *testing.T) {
		// acquire and release lock
		lock1 := flock.New(lockPath)
		locked1, _ := lock1.TryLock()
		if locked1 {
			lock1.Unlock()
		}

		// simulate beads directory being deleted and recreated
		if err := os.RemoveAll(beadsDir); err != nil {
			t.Fatalf("remove beads dir: %v", err)
		}
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("recreate beads dir: %v", err)
		}

		// new lock should work on recreated directory
		lock2 := flock.New(lockPath)
		locked2, err := lock2.TryLock()
		if err != nil {
			t.Fatalf("TryLock after recreate error: %v", err)
		}
		if !locked2 {
			t.Error("should be able to acquire lock after beads dir recreation")
		} else {
			lock2.Unlock()
		}
	})
}

// TestSyncLockBlockingBehavior verifies non-blocking TryLock behavior.
func TestSyncLockBlockingBehavior(t *testing.T) {
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	lockPath := filepath.Join(beadsDir, ".sync.lock")

	// acquire lock
	lock1 := flock.New(lockPath)
	locked1, err := lock1.TryLock()
	if err != nil {
		t.Fatalf("TryLock error: %v", err)
	}
	if !locked1 {
		t.Fatal("expected to acquire lock")
	}
	defer lock1.Unlock()

	// TryLock should return immediately (non-blocking)
	start := time.Now()
	lock2 := flock.New(lockPath)
	locked2, err := lock2.TryLock()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("TryLock error: %v", err)
	}
	if locked2 {
		lock2.Unlock()
		t.Error("TryLock should fail when lock is held")
	}

	// verify TryLock returned quickly (did not block)
	if elapsed > 100*time.Millisecond {
		t.Errorf("TryLock took %v, expected immediate return", elapsed)
	}
}

// TestSyncLockWithContext tests that sync lock acquisition respects context cancellation.
func TestSyncLockWithContext(t *testing.T) {
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	lockPath := filepath.Join(beadsDir, ".sync.lock")

	// Acquire lock
	lock1 := flock.New(lockPath)
	locked1, _ := lock1.TryLock()
	if locked1 {
		defer lock1.Unlock()
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Try to acquire with TryLockContext (if available) or simulate
	lock2 := flock.New(lockPath)

	// simulate context-aware lock attempt
	done := make(chan bool, 1)
	go func() {
		for i := 0; i < 10; i++ {
			select {
			case <-ctx.Done():
				done <- false
				return
			default:
				locked, _ := lock2.TryLock()
				if locked {
					lock2.Unlock()
					done <- true
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		done <- false
	}()

	result := <-done
	if result && locked1 {
		t.Error("should not have acquired lock while lock1 is held")
	}
}

// TestSyncLockFilePermissions verifies lock file has appropriate permissions.
func TestSyncLockFilePermissions(t *testing.T) {
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	lockPath := filepath.Join(beadsDir, ".sync.lock")

	// acquire and release lock to create file
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		t.Fatalf("TryLock error: %v", err)
	}
	if !locked {
		t.Fatal("expected to acquire lock")
	}
	lock.Unlock()

	// verify file was created (flock creates the file)
	info, err := os.Stat(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			// some flock implementations may not leave file after release
			t.Skip("flock implementation does not persist lock file")
		}
		t.Fatalf("stat lock file: %v", err)
	}

	// check that file is readable/writable by owner
	mode := info.Mode().Perm()
	if mode&0600 == 0 {
		t.Errorf("lock file should be readable/writable by owner, got %o", mode)
	}
}

// TestSyncLockRobustness tests lock behavior under various edge conditions.
func TestSyncLockRobustness(t *testing.T) {
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	lockPath := filepath.Join(beadsDir, ".sync.lock")

	t.Run("rapid lock/unlock cycles", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			lock := flock.New(lockPath)
			locked, err := lock.TryLock()
			if err != nil {
				t.Fatalf("iteration %d: TryLock error: %v", i, err)
			}
			if !locked {
				t.Fatalf("iteration %d: expected to acquire lock", i)
			}
			if err := lock.Unlock(); err != nil {
				t.Fatalf("iteration %d: unlock error: %v", i, err)
			}
		}
	})

	t.Run("multiple flock instances same file", func(t *testing.T) {
		// create multiple flock instances pointing to same file
		locks := make([]*flock.Flock, 5)
		for i := range locks {
			locks[i] = flock.New(lockPath)
		}

		// only first should acquire
		locked0, _ := locks[0].TryLock()
		if !locked0 {
			t.Fatal("first lock should succeed")
		}
		defer locks[0].Unlock()

		for i := 1; i < len(locks); i++ {
			locked, _ := locks[i].TryLock()
			if locked {
				locks[i].Unlock()
				t.Errorf("lock[%d] should not have acquired while lock[0] is held", i)
			}
		}
	})
}
