package daemonrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrDaemonLocked = errors.New("daemon lock already held by another process")

// DaemonLockInfo represents the metadata stored in the daemon.lock file
type DaemonLockInfo struct {
	PID       int       `json:"pid"`
	Database  string    `json:"database"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
}

// DaemonLock represents a held lock on the daemon.lock file
type DaemonLock struct {
	file *os.File
	path string
}

// Close releases the daemon lock
func (l *DaemonLock) Close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (d *Daemon) setupLock() (io.Closer, error) {
	lock, err := acquireDaemonLock(d.cfg.BeadsDir, d.cfg.DBPath, d.Version)
	if err != nil {
		if err == ErrDaemonLocked {
			d.log.log("Daemon already running (lock held), exiting")
		} else {
			d.log.log("Error acquiring daemon lock: %v", err)
		}
		return nil, err
	}

	// Ensure PID file matches our PID
	myPID := os.Getpid()
	pidFile := d.cfg.PIDFile
	// #nosec G304 - controlled path from config
	if data, err := os.ReadFile(pidFile); err == nil {
		var filePID int
		if _, err := fmt.Sscanf(string(data), "%d", &filePID); err == nil && filePID != myPID {
			d.log.log("PID file has wrong PID (expected %d, got %d), overwriting", myPID, filePID)
			_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", myPID)), 0600)
		}
	} else {
		d.log.log("PID file missing after lock acquisition, creating")
		_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", myPID)), 0600)
	}

	return lock, nil
}

// acquireDaemonLock attempts to acquire an exclusive lock on daemon.lock
func acquireDaemonLock(beadsDir string, dbPath string, version string) (*DaemonLock, error) {
	lockPath := filepath.Join(beadsDir, "daemon.lock")

	// Open or create the lock file
	// #nosec G304 - controlled path from config
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file: %w", err)
	}

	// Try to acquire exclusive non-blocking lock
	if err := flockExclusive(f); err != nil {
		_ = f.Close()
		if err == ErrDaemonLocked {
			return nil, ErrDaemonLocked
		}
		return nil, fmt.Errorf("cannot lock file: %w", err)
	}

	// Write JSON metadata to the lock file
	lockInfo := DaemonLockInfo{
		PID:       os.Getpid(),
		Database:  dbPath,
		Version:   version,
		StartedAt: time.Now().UTC(),
	}

	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(lockInfo)
	_ = f.Sync()

	// Also write PID file for Windows compatibility
	pidFile := filepath.Join(beadsDir, "daemon.pid")
	_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0600)

	return &DaemonLock{file: f, path: lockPath}, nil
}
