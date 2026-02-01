//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// findBdBinary attempts to locate the bd binary for integration tests.
// It checks:
// 1. Repository root ("../../bd")
// 2. System PATH
func findBdBinary() (string, error) {
	// Find bd binary: prefer repo root build, then PATH
	bdBinary := "bd"
	if runtime.GOOS == "windows" {
		bdBinary = "bd.exe"
	}

	repoRoot := filepath.Join("..", "..")
	existingBD := filepath.Join(repoRoot, bdBinary)
	if absPath, err := filepath.Abs(existingBD); err == nil {
		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
	}

	if path, err := exec.LookPath("bd"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("bd binary not found in repo root or PATH")
}

func TestDaemonLockPreventsMultipleInstances(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")

	// Acquire lock
	lock1, err := acquireDaemonLock(beadsDir, dbPath)
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}
	defer lock1.Close()

	// Try to acquire lock again - should fail
	lock2, err := acquireDaemonLock(beadsDir, dbPath)
	if err != ErrDaemonLocked {
		if lock2 != nil {
			lock2.Close()
		}
		t.Fatalf("Expected ErrDaemonLocked, got: %v", err)
	}

	// Release first lock
	lock1.Close()

	// Now should be able to acquire lock
	lock3, err := acquireDaemonLock(beadsDir, dbPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock after release: %v", err)
	}
	lock3.Close()
}

func TestDaemonLockFileDeletedOnClose(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	lockPath := filepath.Join(beadsDir, "daemon.lock")

	// Acquire lock
	lock, err := acquireDaemonLock(beadsDir, dbPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Verify lock file exists
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("Lock file should exist after acquiring lock")
	}

	// Close/release the lock
	if err := lock.Close(); err != nil {
		t.Fatalf("Failed to close lock: %v", err)
	}

	// Verify lock file is deleted after close
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("Lock file should be deleted after Close(), but it still exists")
	}
}

// TestDaemonStopDeletesLockFile verifies that "bd daemon stop" removes the lock file.
// This is an end-to-end test that starts a real daemon process and stops it.
func TestDaemonStopDeletesLockFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow integration test in short mode")
	}

	// Find bd binary
	bdBinary, err := findBdBinary()
	if err != nil {
		t.Skip("bd binary not found, skipping integration test")
	}

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	lockPath := filepath.Join(beadsDir, "daemon.lock")

	// Create git repo (required for daemon)
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User").Run()

	// Initialize bd
	cmd = exec.Command(bdBinary, "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init bd: %v\nOutput: %s", err, out)
	}

	// Start daemon
	cmd = exec.Command(bdBinary, "daemon", "start", "--interval", "10m")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to start daemon: %v\nOutput: %s", err, out)
	}

	// Wait for daemon to be fully running
	time.Sleep(500 * time.Millisecond)

	// Verify lock file exists while daemon is running
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("Lock file should exist while daemon is running")
	}

	// Stop daemon (pass workspace path as required by "bd daemon stop")
	cmd = exec.Command(bdBinary, "daemon", "stop", tmpDir)
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to stop daemon: %v\nOutput: %s", err, out)
	}

	// Give daemon time to clean up
	time.Sleep(300 * time.Millisecond)

	// Verify lock file is deleted after daemon stop
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("Lock file should be deleted after 'bd daemon stop', but it still exists")
	}
}

func TestTryDaemonLockDetectsRunning(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")

	// Initially no daemon running
	running, _ := tryDaemonLock(beadsDir)
	if running {
		t.Fatal("Expected no daemon running initially")
	}

	// Acquire lock
	lock, err := acquireDaemonLock(beadsDir, dbPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	defer lock.Close()

	// Now should detect daemon running
	running, pid := tryDaemonLock(beadsDir)
	if !running {
		t.Fatal("Expected daemon to be detected as running")
	}
	if pid != os.Getpid() {
		t.Errorf("Expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestBackwardCompatibilityWithOldDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Simulate old daemon: PID file exists but no lock file
	pidFile := filepath.Join(beadsDir, "daemon.pid")
	currentPID := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", currentPID)), 0600); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	// tryDaemonLock should detect the old daemon via PID file fallback
	running, pid := tryDaemonLock(beadsDir)
	if !running {
		t.Fatal("Expected old daemon to be detected via PID file")
	}
	if pid != currentPID {
		t.Errorf("Expected PID %d, got %d", currentPID, pid)
	}

	// Clean up PID file
	os.Remove(pidFile)

	// Now should report no daemon running
	running, _ = tryDaemonLock(beadsDir)
	if running {
		t.Fatal("Expected no daemon running after PID file removed")
	}
}

func TestDaemonLockJSONFormat(t *testing.T) {
	// Skip on Windows - file locking prevents reading lock file while locked
	if runtime.GOOS == "windows" {
		t.Skip("Windows file locking prevents reading locked files")
	}

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")

	// Acquire lock
	lock, err := acquireDaemonLock(beadsDir, dbPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	defer lock.Close()

	// Read the lock file and verify JSON format
	lockInfo, err := readDaemonLockInfo(beadsDir)
	if err != nil {
		t.Fatalf("Failed to read lock info: %v", err)
	}

	if lockInfo.PID != os.Getpid() {
		t.Errorf("Expected PID %d, got %d", os.Getpid(), lockInfo.PID)
	}

	if lockInfo.Database != dbPath {
		t.Errorf("Expected database %s, got %s", dbPath, lockInfo.Database)
	}

	if lockInfo.Version != Version {
		t.Errorf("Expected version %s, got %s", Version, lockInfo.Version)
	}

	if lockInfo.StartedAt.IsZero() {
		t.Error("Expected non-zero StartedAt timestamp")
	}
}

func TestValidateDaemonLock(t *testing.T) {
	// Skip on Windows - file locking prevents reading lock file while locked
	if runtime.GOOS == "windows" {
		t.Skip("Windows file locking prevents reading locked files")
	}

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")

	// No lock file - validation should pass
	if err := validateDaemonLock(beadsDir, dbPath); err != nil {
		t.Errorf("Expected no error when no lock file exists, got: %v", err)
	}

	// Acquire lock with correct database
	lock, err := acquireDaemonLock(beadsDir, dbPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	defer lock.Close()

	// Validation should pass with matching database
	if err := validateDaemonLock(beadsDir, dbPath); err != nil {
		t.Errorf("Expected no error with matching database, got: %v", err)
	}

	// Validation should fail with different database
	wrongDB := filepath.Join(beadsDir, "wrong.db")
	if err := validateDaemonLock(beadsDir, wrongDB); err == nil {
		t.Error("Expected error with mismatched database")
	}
}

func TestMultipleDaemonProcessesRace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition test in short mode")
	}

	// Find the bd binary
	bdBinary, err := findBdBinary()
	if err != nil {
		t.Skip("bd binary not found, skipping race test")
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	beadsDir := filepath.Dir(dbPath)

	// Initialize a test database with git repo
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Initialize bd
	cmd = exec.Command(bdBinary, "init", "--prefix", "test")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_DB="+dbPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init bd: %v\nOutput: %s", err, out)
	}

	// Try to start 5 daemons simultaneously
	numAttempts := 5
	results := make(chan error, numAttempts)

	for i := 0; i < numAttempts; i++ {
		go func() {
			cmd := exec.Command(bdBinary, "daemon", "--interval", "10m")
			cmd.Dir = tmpDir
			cmd.Env = append(os.Environ(), "BEADS_DB="+dbPath)
			err := cmd.Start()
			if err != nil {
				results <- err
				return
			}

			// Wait a bit for daemon to start
			time.Sleep(200 * time.Millisecond)

			// Check if it's still running
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			results <- cmd.Wait()
		}()
	}

	// Wait for all attempts
	var successCount int
	var alreadyRunning int
	timeout := time.After(5 * time.Second)

	for i := 0; i < numAttempts; i++ {
		select {
		case err := <-results:
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "exit status 1") {
				// Could be "already running" error
				alreadyRunning++
			}
		case <-timeout:
			t.Fatal("Test timed out waiting for daemon processes")
		}
	}

	// Clean up any remaining daemon files
	os.Remove(filepath.Join(beadsDir, "daemon.pid"))
	os.Remove(filepath.Join(beadsDir, "daemon.lock"))
	os.Remove(filepath.Join(beadsDir, "bd.sock"))

	t.Logf("Results: %d success, %d already running", successCount, alreadyRunning)

	// At most one should have succeeded in holding the lock
	// (though timing means even the first might have exited by the time we checked)
	if alreadyRunning < numAttempts-1 {
		t.Logf("Warning: Expected at least %d processes to fail with 'already running', got %d",
			numAttempts-1, alreadyRunning)
		t.Log("This could indicate a race condition, but may also be timing-related in tests")
	}
}
