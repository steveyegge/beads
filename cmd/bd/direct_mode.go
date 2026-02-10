package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/syncbranch"
)

// ensureDirectMode makes sure the CLI is operating in direct-storage mode.
// If the daemon is active, it is cleanly disconnected and the shared store is opened.
// When BD_DAEMON_HOST is set (remote daemon), direct storage access is blocked,
// so this returns an error explaining the limitation (bd-ma0s.1).
func ensureDirectMode(reason string) error {
	// When BD_DAEMON_HOST is set, direct storage is blocked by the factory guard.
	// Don't attempt fallback - the remote daemon should handle operations.
	if rpc.GetDaemonHost() != "" {
		return fmt.Errorf("this operation requires direct database access, which is not available when BD_DAEMON_HOST is set (%s)",
			reason)
	}
	if getDaemonClient() != nil {
		if err := fallbackToDirectMode(reason); err != nil {
			return err
		}
		return nil
	}
	return ensureStoreActive()
}

// fallbackToDirectMode disables the daemon client and ensures a local store is ready.
// When BD_DAEMON_HOST is set (remote daemon), falling back to local storage is not
// allowed — the user explicitly requested a remote daemon connection (bd-lkks).
func fallbackToDirectMode(reason string) error {
	if rpc.GetDaemonHost() != "" {
		return fmt.Errorf("cannot fall back to local storage: BD_DAEMON_HOST is set (%s).\n"+
			"The remote daemon at %s should handle this operation.\n"+
			"Hint: check that the daemon is running and BD_DAEMON_TOKEN is correct",
			reason, rpc.GetDaemonHost())
	}
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

// ensureStoreActive guarantees that a storage backend is initialized and tracked.
// Uses the factory to respect metadata.json backend configuration (SQLite, Dolt embedded, or Dolt server).
//
// When a daemon is connected, the store is not needed (daemon handles operations via RPC),
// so this returns nil without opening direct storage. This prevents "direct database access
// blocked" errors when BD_DAEMON_HOST is set (bd-ma0s.1).
func ensureStoreActive() error {
	// If daemon is connected, commands should use daemon RPC, not direct storage.
	// Return nil to allow callers to proceed to their daemon code paths.
	if getDaemonClient() != nil {
		return nil
	}

	lockStore()
	active := isStoreActive() && getStore() != nil
	unlockStore()
	if active {
		return nil
	}

	// Find the .beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if rpc.GetDaemonHost() != "" {
			return fmt.Errorf("no local database found, but BD_DAEMON_HOST is set (%s).\n"+
				"Hint: this command should use the remote daemon. Check daemon connectivity with 'bd doctor'",
				rpc.GetDaemonHost())
		}
		return fmt.Errorf("no beads database found.\n" +
			"Hint: run 'bd connect --url <DAEMON_URL> --token <TOKEN>' to connect to a remote daemon,\n" +
			"      or run 'bd init' to create a local database")
	}

	// When BD_DAEMON_HOST is set, refuse to open a local database even if one exists.
	// Local writes would not persist — the remote daemon is the source of truth. (bd-lkks)
	if rpc.GetDaemonHost() != "" {
		return fmt.Errorf("local database found at %s, but BD_DAEMON_HOST is set (%s).\n"+
			"Writing to a local database will NOT persist — the remote daemon is the source of truth.\n"+
			"Hint: check daemon connectivity with 'bd doctor', or unset BD_DAEMON_HOST for local-only use",
			beadsDir, rpc.GetDaemonHost())
	}

	// GH#1349: Ensure sync branch worktree exists if configured.
	// This must happen before any JSONL operations to fix fresh clone scenario
	// where findJSONLPath would otherwise fall back to main's stale JSONL.
	if _, err := syncbranch.EnsureWorktree(context.Background()); err != nil {
		// Log warning but don't fail - operations can still work with main's JSONL
		// This allows graceful degradation if worktree creation fails
		debug.Logf("Warning: could not ensure sync worktree: %v", err)
	}

	// Check if this is a JSONL-only project
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if _, err := os.Stat(jsonlPath); err == nil {
		// JSONL exists - check if no-db mode is configured
		if isNoDbModeConfigured(beadsDir) {
			return fmt.Errorf("this project uses JSONL-only mode (no SQLite database).\n" +
				"Hint: use 'bd --no-db <command>' or set 'no-db: true' in config.yaml")
		}
	}

	// Use factory to create the appropriate backend (SQLite, Dolt embedded, or Dolt server)
	// based on metadata.json configuration
	store, err := factory.NewFromConfig(getRootContext(), beadsDir)
	if err != nil {
		// Check for fresh clone scenario
		if isFreshCloneError(err) {
			handleFreshCloneError(err, beadsDir)
			return fmt.Errorf("database not initialized")
		}
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Update the database path for compatibility with code that expects it
	if dbPath := beads.FindDatabasePath(); dbPath != "" {
		setDBPath(dbPath)
	}

	lockStore()
	setStore(store)
	setStoreActive(true)
	unlockStore()

	if isAutoImportEnabled() {
		autoImportIfNewer()
	}

	return nil
}
