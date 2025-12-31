package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/storage/sqlite"
)

// ensureDirectMode makes sure the CLI is operating in direct-storage mode.
// If the daemon is active, it is cleanly disconnected and the shared store is opened.
func ensureDirectMode(reason string) error {
	if getDaemonClient() != nil {
		if err := fallbackToDirectMode(reason); err != nil {
			return err
		}
		return nil
	}
	return ensureStoreActive()
}

// fallbackToDirectMode disables the daemon client and ensures a local store is ready.
func fallbackToDirectMode(reason string) error {
	disableDaemonForFallback(reason)
	return ensureStoreActive()
}

// disableDaemonForFallback closes the daemon client and updates status metadata.
func disableDaemonForFallback(reason string) {
	if client := getDaemonClient(); client != nil {
		_ = client.Close()
		setDaemonClient(nil)
	}

	ds := getDaemonStatus()
	ds.Mode = "direct"
	ds.Connected = false
	ds.Degraded = true
	if reason != "" {
		ds.Detail = reason
	}
	if ds.FallbackReason == FallbackNone {
		ds.FallbackReason = FallbackDaemonUnsupported
	}
	setDaemonStatus(ds)

	if reason != "" {
		debug.Logf("Debug: %s\n", reason)
	}
}

// ensureStoreActive guarantees that a local SQLite store is initialized and tracked.
func ensureStoreActive() error {
	lockStore()
	active := isStoreActive() && getStore() != nil
	unlockStore()
	if active {
		return nil
	}

	path := getDBPath()
	if path == "" {
		if found := beads.FindDatabasePath(); found != "" {
			setDBPath(found)
			path = found
		} else {
			// Check if this is a JSONL-only project
			beadsDir := beads.FindBeadsDir()
			if beadsDir != "" {
				jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
				if _, err := os.Stat(jsonlPath); err == nil {
					// JSONL exists - check if no-db mode is configured
					if isNoDbModeConfigured(beadsDir) {
						return fmt.Errorf("this project uses JSONL-only mode (no SQLite database).\n" +
							"Hint: use 'bd --no-db <command>' or set 'no-db: true' in config.yaml")
					}
					// JSONL exists but no-db not configured - fresh clone scenario
					return fmt.Errorf("found JSONL file but no database: %s\n"+
						"Hint: run 'bd init' to create the database and import issues,\n"+
						"      or use 'bd --no-db' for JSONL-only mode", jsonlPath)
				}
			}
			return fmt.Errorf("no beads database found.\n" +
				"Hint: run 'bd init' to create a database in the current directory,\n" +
				"      or use 'bd --no-db' for JSONL-only mode")
		}
	}

	sqlStore, err := sqlite.New(getRootContext(), path)
	if err != nil {
		// Check for fresh clone scenario
		if isFreshCloneError(err) {
			beadsDir := filepath.Dir(path)
			handleFreshCloneError(err, beadsDir)
			return fmt.Errorf("database not initialized")
		}
		return fmt.Errorf("failed to open database: %w", err)
	}

	lockStore()
	setStore(sqlStore)
	setStoreActive(true)
	unlockStore()

	if isAutoImportEnabled() {
		autoImportIfNewer()
	}

	return nil
}
