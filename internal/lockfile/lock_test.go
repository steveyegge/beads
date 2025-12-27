package lockfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadLockInfo(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("JSON format", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "daemon.lock")
		lockInfo := &LockInfo{
			PID:       12345,
			ParentPID: 1,
			Database:  "/path/to/db",
			Version:   "1.0.0",
			StartedAt: time.Now(),
		}

		data, err := json.Marshal(lockInfo)
		if err != nil {
			t.Fatalf("failed to marshal lock info: %v", err)
		}

		if err := os.WriteFile(lockPath, data, 0644); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}

		result, err := ReadLockInfo(tmpDir)
		if err != nil {
			t.Fatalf("ReadLockInfo failed: %v", err)
		}

		if result.PID != lockInfo.PID {
			t.Errorf("PID mismatch: got %d, want %d", result.PID, lockInfo.PID)
		}

		if result.Database != lockInfo.Database {
			t.Errorf("Database mismatch: got %s, want %s", result.Database, lockInfo.Database)
		}
	})

	t.Run("old format (plain PID)", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "daemon.lock")
		if err := os.WriteFile(lockPath, []byte("98765"), 0644); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}

		result, err := ReadLockInfo(tmpDir)
		if err != nil {
			t.Fatalf("ReadLockInfo failed: %v", err)
		}

		if result.PID != 98765 {
			t.Errorf("PID mismatch: got %d, want %d", result.PID, 98765)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		nonExistentDir := filepath.Join(tmpDir, "nonexistent")
		_, err := ReadLockInfo(nonExistentDir)
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "daemon.lock")
		if err := os.WriteFile(lockPath, []byte("invalid json"), 0644); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}

		_, err := ReadLockInfo(tmpDir)
		if err == nil {
			t.Error("expected error for invalid format")
		}
	})
}

func TestCheckPIDFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("file not found", func(t *testing.T) {
		running, pid := checkPIDFile(tmpDir)
		if running {
			t.Error("expected running=false when PID file doesn't exist")
		}
		if pid != 0 {
			t.Errorf("expected pid=0, got %d", pid)
		}
	})

	t.Run("invalid PID", func(t *testing.T) {
		pidFile := filepath.Join(tmpDir, "daemon.pid")
		if err := os.WriteFile(pidFile, []byte("not-a-number"), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		running, pid := checkPIDFile(tmpDir)
		if running {
			t.Error("expected running=false for invalid PID")
		}
		if pid != 0 {
			t.Errorf("expected pid=0, got %d", pid)
		}
	})

	t.Run("process not running", func(t *testing.T) {
		pidFile := filepath.Join(tmpDir, "daemon.pid")
		// Use PID 99999 which is unlikely to be running
		if err := os.WriteFile(pidFile, []byte("99999"), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		running, pid := checkPIDFile(tmpDir)
		if running {
			t.Error("expected running=false for non-existent process")
		}
		if pid != 0 {
			t.Errorf("expected pid=0 for non-running process, got %d", pid)
		}
	})

	t.Run("current process is running", func(t *testing.T) {
		pidFile := filepath.Join(tmpDir, "daemon.pid")
		currentPID := os.Getpid()
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", currentPID)), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		running, pid := checkPIDFile(tmpDir)
		if !running {
			t.Error("expected running=true for current process")
		}
		if pid != currentPID {
			t.Errorf("expected pid=%d, got %d", currentPID, pid)
		}
	})
}

func TestTryDaemonLock(t *testing.T) {
	t.Run("no lock file exists", func(t *testing.T) {
		tmpDir := t.TempDir()

		running, pid := TryDaemonLock(tmpDir)
		if running {
			t.Error("expected running=false when no lock file exists")
		}
		if pid != 0 {
			t.Errorf("expected pid=0, got %d", pid)
		}
	})

	t.Run("lock file exists but not locked - daemon not running", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "daemon.lock")

		lockInfo := LockInfo{
			PID:       12345,
			Database:  "/path/to/db",
			Version:   "1.0.0",
			StartedAt: time.Now(),
		}
		data, _ := json.Marshal(lockInfo)
		if err := os.WriteFile(lockPath, data, 0644); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}

		running, _ := TryDaemonLock(tmpDir)
		if running {
			t.Error("expected running=false when lock file exists but is not locked")
		}
	})

	t.Run("lock file held by another process - daemon running", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "daemon.lock")

		lockInfo := LockInfo{
			PID:       os.Getpid(),
			Database:  "/path/to/db",
			Version:   "1.0.0",
			StartedAt: time.Now(),
		}
		data, _ := json.Marshal(lockInfo)
		if err := os.WriteFile(lockPath, data, 0644); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}

		f, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("failed to open lock file: %v", err)
		}
		defer f.Close()

		if err := FlockExclusiveBlocking(f); err != nil {
			t.Fatalf("failed to acquire lock: %v", err)
		}
		defer FlockUnlock(f)

		running, pid := TryDaemonLock(tmpDir)
		if !running {
			t.Error("expected running=true when lock is held")
		}
		if pid != os.Getpid() {
			t.Errorf("expected pid=%d, got %d", os.Getpid(), pid)
		}
	})

	t.Run("lock file with old format (plain PID)", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "daemon.lock")

		currentPID := os.Getpid()
		if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", currentPID)), 0644); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}

		f, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("failed to open lock file: %v", err)
		}
		defer f.Close()

		if err := FlockExclusiveBlocking(f); err != nil {
			t.Fatalf("failed to acquire lock: %v", err)
		}
		defer FlockUnlock(f)

		running, pid := TryDaemonLock(tmpDir)
		if !running {
			t.Error("expected running=true when lock is held")
		}
		if pid != currentPID {
			t.Errorf("expected pid=%d, got %d", currentPID, pid)
		}
	})

	t.Run("lock file with invalid content falls back to PID file", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "daemon.lock")
		pidFile := filepath.Join(tmpDir, "daemon.pid")

		if err := os.WriteFile(lockPath, []byte("invalid content"), 0644); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}

		currentPID := os.Getpid()
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", currentPID)), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		f, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("failed to open lock file: %v", err)
		}
		defer f.Close()

		if err := FlockExclusiveBlocking(f); err != nil {
			t.Fatalf("failed to acquire lock: %v", err)
		}
		defer FlockUnlock(f)

		running, pid := TryDaemonLock(tmpDir)
		if !running {
			t.Error("expected running=true when lock is held")
		}
		if pid != currentPID {
			t.Errorf("expected pid=%d from PID file fallback, got %d", currentPID, pid)
		}
	})

	t.Run("falls back to PID file when no lock file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidFile := filepath.Join(tmpDir, "daemon.pid")

		currentPID := os.Getpid()
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", currentPID)), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		running, pid := TryDaemonLock(tmpDir)
		if !running {
			t.Error("expected running=true when PID file has running process")
		}
		if pid != currentPID {
			t.Errorf("expected pid=%d, got %d", currentPID, pid)
		}
	})
}

func TestFlockFunctions(t *testing.T) {
	t.Run("FlockExclusiveBlocking and FlockUnlock", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		if err := os.WriteFile(lockPath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create lock file: %v", err)
		}

		f, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("failed to open lock file: %v", err)
		}
		defer f.Close()

		if err := FlockExclusiveBlocking(f); err != nil {
			t.Errorf("FlockExclusiveBlocking failed: %v", err)
		}

		if err := FlockUnlock(f); err != nil {
			t.Errorf("FlockUnlock failed: %v", err)
		}
	})

	t.Run("flockExclusive non-blocking succeeds on unlocked file", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		if err := os.WriteFile(lockPath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create lock file: %v", err)
		}

		f, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("failed to open lock file: %v", err)
		}
		defer f.Close()

		if err := flockExclusive(f); err != nil {
			t.Errorf("flockExclusive should succeed on unlocked file: %v", err)
		}

		FlockUnlock(f)
	})

	t.Run("flockExclusive returns errDaemonLocked when already locked", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		if err := os.WriteFile(lockPath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create lock file: %v", err)
		}

		f1, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("failed to open lock file: %v", err)
		}
		defer f1.Close()

		if err := FlockExclusiveBlocking(f1); err != nil {
			t.Fatalf("failed to acquire first lock: %v", err)
		}
		defer FlockUnlock(f1)

		f2, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("failed to open second lock file handle: %v", err)
		}
		defer f2.Close()

		err = flockExclusive(f2)
		if err != errDaemonLocked {
			t.Errorf("expected errDaemonLocked, got %v", err)
		}
	})
}

func TestIsProcessRunning(t *testing.T) {
	t.Run("current process is running", func(t *testing.T) {
		if !isProcessRunning(os.Getpid()) {
			t.Error("expected current process to be running")
		}
	})

	t.Run("non-existent process is not running", func(t *testing.T) {
		if isProcessRunning(99999) {
			t.Error("expected non-existent process to not be running")
		}
	})

	t.Run("parent process is running", func(t *testing.T) {
		ppid := os.Getppid()
		if ppid > 0 && !isProcessRunning(ppid) {
			t.Error("expected parent process to be running")
		}
	})
}
