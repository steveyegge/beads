package fix

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
)

// DatabaseIntegrity attempts to recover from database corruption by:
//  1. Backing up the corrupt database (and WAL/SHM if present)
//  2. Re-initializing the database from the working tree JSONL export
//
// This is intentionally conservative: it will not delete JSONL, and it preserves the
// original DB as a backup for forensic recovery.
func DatabaseIntegrity(path string) error {
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	beadsDir := filepath.Join(absPath, ".beads")

	// Resolve database path (respects metadata.json database override).
	var dbPath string
	if cfg, err := configfile.Load(beadsDir); err == nil && cfg != nil && cfg.Database != "" {
		dbPath = cfg.DatabasePath(beadsDir)
	} else {
		dbPath = filepath.Join(beadsDir, beads.CanonicalDatabaseName)
	}

	// Find JSONL source of truth.
	jsonlPath := ""
	if cfg, err := configfile.Load(beadsDir); err == nil && cfg != nil {
		candidate := cfg.JSONLPath(beadsDir)
		if _, err := os.Stat(candidate); err == nil {
			jsonlPath = candidate
		}
	}
	if jsonlPath == "" {
		for _, name := range []string{"issues.jsonl", "beads.jsonl"} {
			candidate := filepath.Join(beadsDir, name)
			if _, err := os.Stat(candidate); err == nil {
				jsonlPath = candidate
				break
			}
		}
	}
	if jsonlPath == "" {
		return fmt.Errorf("cannot auto-recover: no JSONL export found in %s", beadsDir)
	}

	// Back up corrupt DB and its sidecar files.
	ts := time.Now().UTC().Format("20060102T150405Z")
	backupDB := dbPath + "." + ts + ".corrupt.backup.db"
	if err := os.Rename(dbPath, backupDB); err != nil {
		return fmt.Errorf("failed to back up database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sidecar := dbPath + suffix
		if _, err := os.Stat(sidecar); err == nil {
			_ = os.Rename(sidecar, backupDB+suffix) // best effort
		}
	}

	// Rebuild by importing from the working tree JSONL into a fresh database.
	bdBinary, err := getBdBinary()
	if err != nil {
		return err
	}

	// Use import (not init) so we always hydrate from the working tree JSONL, not git-tracked blobs.
	args := []string{"--db", dbPath, "import", "-i", jsonlPath, "--force", "--no-git-history"}
	cmd := newBdCmd(bdBinary, args...)
	cmd.Dir = absPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Best-effort rollback: attempt to restore the backup, preserving any partial init output.
		failedTS := time.Now().UTC().Format("20060102T150405Z")
		if _, statErr := os.Stat(dbPath); statErr == nil {
			failedDB := dbPath + "." + failedTS + ".failed.init.db"
			_ = os.Rename(dbPath, failedDB)
			for _, suffix := range []string{"-wal", "-shm", "-journal"} {
				_ = os.Rename(dbPath+suffix, failedDB+suffix)
			}
		}
		_ = os.Rename(backupDB, dbPath)
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			_ = os.Rename(backupDB+suffix, dbPath+suffix)
		}
		return fmt.Errorf("failed to rebuild database from JSONL: %w (backup: %s)", err, backupDB)
	}

	return nil
}
