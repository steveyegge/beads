//go:build unix

package daemon

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
)

const (
	sigTermTimeout = 10 * time.Second
	sigKillWait    = 2 * time.Second
	pollInterval   = 200 * time.Millisecond
)

// Kill stops the bdd daemon in beadsDir, if one is running.
// It reads bdd.pid, sends SIGTERM, waits up to 10s, then SIGKILLs if needed.
// Returns nil if no daemon is running or after a clean stop.
// bdd.log is never removed.
func Kill(beadsDir string) error {
	pf, err := pidfile.Read(beadsDir, "bdd.pid")
	if err != nil {
		return err
	}
	if pf == nil {
		return nil // no pid file — nothing to kill
	}

	proc, err := os.FindProcess(pf.Pid)
	if err != nil {
		// Process not found — clean up stale files.
		return removeFiles(beadsDir)
	}

	// Check if the process is actually alive.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Stale pid file — process is gone.
		return removeFiles(beadsDir)
	}

	// Send SIGTERM and poll for exit.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return removeFiles(beadsDir)
	}

	deadline := time.Now().Add(sigTermTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		if proc.Signal(syscall.Signal(0)) != nil {
			return removeFiles(beadsDir)
		}
	}

	// Still alive — force kill.
	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(sigKillWait)

	return removeFiles(beadsDir)
}

func removeFiles(beadsDir string) error {
	if err := pidfile.Remove(beadsDir, "bdd.pid"); err != nil {
		return err
	}
	sock := filepath.Join(beadsDir, "bdd.sock")
	if err := os.Remove(sock); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
