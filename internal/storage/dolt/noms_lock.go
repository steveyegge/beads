package dolt

import (
	"fmt"
	"os"
	"path/filepath"
)

// CleanStaleNomsLocks removes stale Dolt noms LOCK files from all databases
// in the given dolt directory (typically .beads/dolt/).
//
// Dolt's noms storage layer creates a file-based LOCK at
// <db>/.dolt/noms/LOCK when opening a database. If the process is killed
// uncleanly (SIGKILL, OOM, etc.), this LOCK file persists and prevents the
// Dolt server from opening the database on restart — causing either a SIGSEGV
// (nil pointer dereference in DoltDB.SetCrashOnFatalError) or a "database is
// locked" error.
//
// This function is safe to call before starting or connecting to a Dolt server
// because the server is not yet using the databases. It scans all subdirectories
// of doltDir for <db>/.dolt/noms/LOCK and removes them.
//
// Returns the number of lock files removed. Errors removing individual files
// are collected but do not abort the scan.
func CleanStaleNomsLocks(doltDir string) (removed int, errs []error) {
	entries, err := os.ReadDir(doltDir)
	if err != nil {
		// Directory doesn't exist or can't be read — nothing to clean.
		return 0, nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lockPath := filepath.Join(doltDir, entry.Name(), ".dolt", "noms", "LOCK")
		if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
			continue
		}
		if rmErr := os.Remove(lockPath); rmErr != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", lockPath, rmErr))
		} else {
			removed++
		}
	}

	return removed, errs
}
