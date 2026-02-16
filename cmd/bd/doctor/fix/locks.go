package fix

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/lockfile"
)

// StaleLockFiles removes stale lock files from the .beads directory.
// This is safe because:
// - Bootstrap/sync/startup locks use flock, which is released on process exit
// - If the flock is released but the file remains, the file is just clutter
// - Daemon lock staleness is verified via flock probe (not just file age)
func StaleLockFiles(path string) error {
	beadsDir := filepath.Join(path, ".beads")
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		return nil
	}

	var removed []string
	var errors []string

	// Remove stale bootstrap lock
	bootstrapLockPath := filepath.Join(beadsDir, "dolt.bootstrap.lock")
	if info, err := os.Stat(bootstrapLockPath); err == nil {
		age := time.Since(info.ModTime())
		if age > 5*time.Minute {
			if err := os.Remove(bootstrapLockPath); err != nil {
				errors = append(errors, fmt.Sprintf("dolt.bootstrap.lock: %v", err))
			} else {
				removed = append(removed, "dolt.bootstrap.lock")
			}
		}
	}

	// Remove stale sync lock
	syncLockPath := filepath.Join(beadsDir, ".sync.lock")
	if info, err := os.Stat(syncLockPath); err == nil {
		age := time.Since(info.ModTime())
		if age > 1*time.Hour {
			if err := os.Remove(syncLockPath); err != nil {
				errors = append(errors, fmt.Sprintf(".sync.lock: %v", err))
			} else {
				removed = append(removed, ".sync.lock")
			}
		}
	}

	// Remove stale daemon lock (only if no process holds the flock)
	daemonLockPath := filepath.Join(beadsDir, "daemon.lock")
	if _, err := os.Stat(daemonLockPath); err == nil {
		running, _ := lockfile.TryDaemonLock(beadsDir)
		if !running {
			if err := os.Remove(daemonLockPath); err != nil {
				errors = append(errors, fmt.Sprintf("daemon.lock: %v", err))
			} else {
				removed = append(removed, "daemon.lock")
			}
		}
	}

	// Remove stale dolt-access.lock (embedded dolt advisory flock).
	// This lock uses flock which is released on process exit, but the file
	// persists and can confuse diagnostics or cause issues if flock behavior
	// varies across platforms.
	accessLockPath := filepath.Join(beadsDir, "dolt-access.lock")
	if info, err := os.Stat(accessLockPath); err == nil {
		age := time.Since(info.ModTime())
		if age > 5*time.Minute {
			if err := os.Remove(accessLockPath); err != nil {
				errors = append(errors, fmt.Sprintf("dolt-access.lock: %v", err))
			} else {
				removed = append(removed, "dolt-access.lock")
			}
		}
	}

	// Remove stale Dolt internal LOCK files (noms layer filesystem lock).
	// These live at .beads/dolt/<database>/.dolt/noms/LOCK and are created
	// by the embedded Dolt engine. If a process crashes without closing the
	// embedded connector, the LOCK file persists and blocks all future opens
	// with "the database is locked by another dolt process".
	doltDir := filepath.Join(beadsDir, "dolt")
	if dbEntries, err := os.ReadDir(doltDir); err == nil {
		for _, dbEntry := range dbEntries {
			if !dbEntry.IsDir() {
				continue
			}
			nomsLock := filepath.Join(doltDir, dbEntry.Name(), ".dolt", "noms", "LOCK")
			if info, err := os.Stat(nomsLock); err == nil {
				age := time.Since(info.ModTime())
				if age > 5*time.Minute {
					lockName := fmt.Sprintf("dolt/%s/.dolt/noms/LOCK", dbEntry.Name())
					if err := os.Remove(nomsLock); err != nil {
						errors = append(errors, fmt.Sprintf("%s: %v", lockName, err))
					} else {
						removed = append(removed, lockName)
					}
				}
			}
		}
	}

	// Remove stale startup locks
	entries, err := os.ReadDir(beadsDir)
	if err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".startlock") {
				info, err := entry.Info()
				if err != nil {
					continue
				}
				age := time.Since(info.ModTime())
				if age > 30*time.Second {
					lockPath := filepath.Join(beadsDir, entry.Name())
					if err := os.Remove(lockPath); err != nil {
						errors = append(errors, fmt.Sprintf("%s: %v", entry.Name(), err))
					} else {
						removed = append(removed, entry.Name())
					}
				}
			}
		}
	}

	if len(removed) > 0 {
		fmt.Printf("  Removed stale lock files: %s\n", strings.Join(removed, ", "))
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to remove some lock files: %s", strings.Join(errors, "; "))
	}

	return nil
}
