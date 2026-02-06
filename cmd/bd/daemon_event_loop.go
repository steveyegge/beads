package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

// DefaultRemoteSyncInterval is the default interval for periodic remote sync.
// Can be overridden via BEADS_REMOTE_SYNC_INTERVAL environment variable.
const DefaultRemoteSyncInterval = 30 * time.Second

// runEventDrivenLoop implements event-driven daemon architecture.
// Replaces polling ticker with reactive event handlers for:
// - File system changes (JSONL modifications)
// - RPC mutations (create, update, delete)
// - Git operations (via hooks, optional)
// - Parent process monitoring (exit if parent dies)
// - Periodic remote sync (to pull updates from other clones)
//
// The remoteSyncInterval parameter controls how often the daemon pulls from
// remote to check for updates from other clones. Use DefaultRemoteSyncInterval
// or configure via BEADS_REMOTE_SYNC_INTERVAL environment variable.
func runEventDrivenLoop(
	ctx context.Context,
	cancel context.CancelFunc,
	server *rpc.Server,
	serverErrChan chan error,
	store storage.Storage,
	jsonlPath string,
	doExport func(),
	doAutoImport func(),
	autoPull bool,
	parentPID int,
	log daemonLogger,
) {
	// Startup: flush stale dirty_issues entries to reduce query overhead (bd-13lq)
	flushStaleDirtyIssues(ctx, store, log)

	// Startup: purge old tombstones to reduce table sizes (bd-t8b0)
	gcDeadIssues(ctx, store, log)

	// Startup: rebuild blocked_issues_cache for Dolt backend (bd-b2ts)
	rebuildBlockedCacheIfDolt(ctx, store, log)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, daemonSignals...)
	defer signal.Stop(sigChan)

	// Debounced sync actions
	exportDebouncer := NewDebouncer(500*time.Millisecond, func() {
		log.log("Export triggered by mutation events")
		doExport()
	})
	defer exportDebouncer.Cancel()

	importDebouncer := NewDebouncer(500*time.Millisecond, func() {
		log.log("Import triggered by file change")
		doAutoImport()
	})
	defer importDebouncer.Cancel()

	// Debounced blocked cache rebuild (bd-b2ts optimization)
	// Coalesces rapid mutations into a single rebuild instead of running the
	// expensive recursive CTE on every write. 1s window handles burst mutations
	// (e.g., molecule creation with 10+ child issues).
	cacheRebuildDebouncer := NewDebouncer(1*time.Second, func() {
		rebuildBlockedCacheIfDolt(ctx, store, log)
	})
	defer cacheRebuildDebouncer.Cancel()

	// Start file watcher for JSONL changes
	watcher, err := NewFileWatcher(jsonlPath, func() {
		importDebouncer.Trigger()
	})
	var fallbackTicker *time.Ticker
	if err != nil {
		log.log("WARNING: File watcher unavailable (%v), using 60s polling fallback", err)
		watcher = nil
		// Fallback ticker to check for remote changes when watcher unavailable
		fallbackTicker = time.NewTicker(60 * time.Second)
		defer fallbackTicker.Stop()
	} else {
		watcher.Start(ctx, log)
		defer func() { _ = watcher.Close() }()
	}

	// Handle mutation events from RPC server
	mutationChan := server.MutationChan()
	go func() {
		for {
			select {
			case event, ok := <-mutationChan:
				if !ok {
					// Channel closed (should never happen, but handle defensively)
					log.log("Mutation channel closed; exiting listener")
					return
				}
				log.log("Mutation detected: %s %s", event.Type, event.IssueID)
				exportDebouncer.Trigger()
				// Debounced blocked cache rebuild (bd-b2ts)
				// Coalesces rapid mutations - rebuilds once after 1s quiet period
				cacheRebuildDebouncer.Trigger()

			case <-ctx.Done():
				return
			}
		}
	}()

	// Periodic health check
	healthTicker := time.NewTicker(60 * time.Second)
	defer healthTicker.Stop()

	// Periodic stats logging (every 5 minutes)
	// Configurable via BEADS_STATS_LOG_INTERVAL env var
	statsInterval := 5 * time.Minute
	if env := os.Getenv("BEADS_STATS_LOG_INTERVAL"); env != "" {
		if interval, err := time.ParseDuration(env); err == nil && interval > 0 {
			statsInterval = interval
		}
	}
	statsTicker := time.NewTicker(statsInterval)
	defer statsTicker.Stop()

	// Periodic remote sync to pull updates from other clones
	// This is essential for multi-clone workflows where the file watcher only
	// sees local changes but remote may have updates from other clones.
	// Default is 30 seconds; configurable via BEADS_REMOTE_SYNC_INTERVAL.
	// Only enabled when autoPull is true (default when sync.branch is configured).
	var remoteSyncTicker *time.Ticker
	if autoPull {
		remoteSyncInterval := getRemoteSyncInterval(log)
		if remoteSyncInterval > 0 {
			remoteSyncTicker = time.NewTicker(remoteSyncInterval)
			defer remoteSyncTicker.Stop()
			log.log("Auto-pull enabled: checking remote every %v", remoteSyncInterval)
		} else {
			log.log("Auto-pull disabled: remote-sync-interval is 0")
		}
	} else {
		log.log("Auto-pull disabled: use 'git pull' manually to sync remote changes")
	}

	// Parent process check (every 10 seconds)
	parentCheckTicker := time.NewTicker(10 * time.Second)
	defer parentCheckTicker.Stop()

	// Dropped events safety net (faster recovery than health check)
	droppedEventsTicker := time.NewTicker(1 * time.Second)
	defer droppedEventsTicker.Stop()

	for {
		select {
		case <-droppedEventsTicker.C:
			// Check for dropped mutation events every second
			dropped := server.ResetDroppedEventsCount()
			if dropped > 0 {
				log.log("WARNING: %d mutation events were dropped, triggering export", dropped)
				exportDebouncer.Trigger()
			}

		case <-healthTicker.C:
			// Periodic health validation (not sync)
			checkDaemonHealth(ctx, store, log)

		case <-statsTicker.C:
			// Periodic stats logging
			log.log(server.PeriodicStatsSummary())

		case <-func() <-chan time.Time {
			if remoteSyncTicker != nil {
				return remoteSyncTicker.C
			}
			// Never fire if auto-pull is disabled
			return make(chan time.Time)
		}():
			// Periodic remote sync to pull updates from other clones
			// This ensures the daemon sees changes pushed by other clones
			// even when the local file watcher doesn't trigger
			log.log("Periodic remote sync: checking for updates")
			doAutoImport()

		case <-parentCheckTicker.C:
			// Check if parent process is still alive
			if !checkParentProcessAlive(parentPID) {
				log.log("Parent process (PID %d) died, shutting down daemon", parentPID)
				cancel()
				if err := server.Stop(); err != nil {
					log.log("Error stopping server: %v", err)
				}
				return
			}

		case <-func() <-chan time.Time {
			if fallbackTicker != nil {
				return fallbackTicker.C
			}
			// Never fire if watcher is available
			return make(chan time.Time)
		}():
			log.log("Fallback ticker: checking for remote changes")
			importDebouncer.Trigger()

		case sig := <-sigChan:
			if isReloadSignal(sig) {
				log.log("Received reload signal, ignoring")
				continue
			}
			log.log("Received signal %v, shutting down...", sig)
			cancel()
			if err := server.Stop(); err != nil {
				log.log("Error stopping server: %v", err)
			}
			return

		case <-ctx.Done():
		log.log("Context canceled, shutting down")
		if watcher != nil {
		_ = watcher.Close()
		}
			if err := server.Stop(); err != nil {
				log.log("Error stopping server: %v", err)
			}
			return

		case err := <-serverErrChan:
		log.log("RPC server failed: %v", err)
		cancel()
		if watcher != nil {
		_ = watcher.Close()
		}
		if stopErr := server.Stop(); stopErr != nil {
			log.log("Error stopping server: %v", stopErr)
		}
		return
		}
	}
}

// checkDaemonHealth performs periodic health validation.
// Separate from sync operations - just validates state.
func checkDaemonHealth(ctx context.Context, store storage.Storage, log daemonLogger) {
	// Health check 1: Verify metadata is accessible
	// This helps detect if external operations (like bd import --force) have modified metadata
	// Without this, daemon may continue operating with stale metadata cache
	// Try new key first, fall back to old for migration
	if _, err := store.GetMetadata(ctx, "jsonl_content_hash"); err != nil {
		if _, err := store.GetMetadata(ctx, "last_import_hash"); err != nil {
			log.log("Health check: metadata read failed: %v", err)
			// Non-fatal: daemon continues but logs the issue
			// This helps diagnose stuck states in sandboxed environments
		}
	}

	// Health check 2: Database integrity check
	// Verify the database is accessible and structurally sound
	if db := store.UnderlyingDB(); db != nil {
		if store.BackendName() == "sqlite" {
			// SQLite: use PRAGMA quick_check for integrity validation
			var result string
			if err := db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&result); err != nil {
				log.log("Health check: database integrity check failed: %v", err)
			} else if result != "ok" {
				log.log("Health check: database integrity issue: %s", result)
			}
		} else {
			// Dolt/MySQL: use simple connectivity check (PRAGMA is invalid SQL)
			var one int
			if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
				log.log("Health check: database connectivity check failed: %v", err)
			}
		}
	}

	// Health check 3: Disk space check (platform-specific)
	// Uses checkDiskSpace helper which is implemented per-platform
	dbPath := store.Path()
	if dbPath != "" {
		if availableMB, ok := checkDiskSpace(dbPath); ok {
			// Warn if less than 100MB available
			if availableMB < 100 {
				log.log("Health check: low disk space warning: %dMB available", availableMB)
			}
		}
	}

	// Health check 4: Memory usage check
	// Log warning if memory usage is unusually high
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	heapMB := memStats.HeapAlloc / (1024 * 1024)

	// Warn if heap exceeds 500MB (daemon should be lightweight)
	if heapMB > 500 {
		log.log("Health check: high memory usage warning: %dMB heap allocated", heapMB)
	}
}

// rebuildBlockedCacheIfDolt rebuilds the blocked_issues_cache table for Dolt backends.
// Delegates to DoltStore.RebuildBlockedCache() to avoid duplicating the recursive CTE.
// Called at startup and via debounced trigger after mutations. (bd-b2ts)
func rebuildBlockedCacheIfDolt(ctx context.Context, store storage.Storage, log daemonLogger) {
	if store.BackendName() != "dolt" {
		return
	}

	if ds, ok := store.(*dolt.DoltStore); ok {
		if err := ds.RebuildBlockedCache(ctx); err != nil {
			log.log("Blocked cache rebuild failed: %v", err)
		}
	}
}

// gcDeadIssues purges old tombstone issues and cleans up stale data.
// Tombstoned issues older than 7 days are hard-deleted since they've been
// soft-deleted and exported already. This reduces the active dataset size
// and speeds up joins. (bd-t8b0)
func gcDeadIssues(ctx context.Context, store storage.Storage, log daemonLogger) {
	db := store.UnderlyingDB()
	if db == nil {
		return
	}

	backend := store.BackendName()

	// Step 1: Hard-delete tombstoned issues older than 7 days
	// FK CASCADE will clean up dependencies, events, labels, dirty_issues, etc.
	var purgeQuery string
	if backend == "sqlite" {
		purgeQuery = `DELETE FROM issues WHERE status = 'tombstone' AND deleted_at < datetime('now', '-7 days')`
	} else {
		// MySQL/Dolt
		purgeQuery = `DELETE FROM issues WHERE status = 'tombstone' AND deleted_at < DATE_SUB(NOW(), INTERVAL 7 DAY)`
	}
	result, err := db.ExecContext(ctx, purgeQuery)
	if err != nil {
		log.log("GC: failed to purge old tombstones: %v", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		log.log("GC: purged %d tombstoned issues older than 7 days", n)
	}

	// Step 2: Clean up orphaned dependencies (both sides deleted)
	// Dependencies where either issue_id or depends_on_id no longer exists
	orphanDepQuery := `
		DELETE FROM dependencies WHERE
			issue_id NOT IN (SELECT id FROM issues)
			OR depends_on_id NOT IN (SELECT id FROM issues)
	`
	result, err = db.ExecContext(ctx, orphanDepQuery)
	if err != nil {
		log.log("GC: failed to clean orphaned dependencies: %v", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		log.log("GC: removed %d orphaned dependency records", n)
	}
}

// flushStaleDirtyIssues removes dirty_issues entries that are stale:
// 1) Entries referencing issues that no longer exist (orphaned)
// 2) Entries for issues already exported with matching content hash
// This is called once at daemon startup to prevent large dirty_issues tables
// from inflating query scan times. (bd-13lq)
func flushStaleDirtyIssues(ctx context.Context, store storage.Storage, log daemonLogger) {
	db := store.UnderlyingDB()
	if db == nil {
		return
	}

	backend := store.BackendName()

	// Step 1: Remove orphaned dirty entries (issue no longer exists)
	orphanQuery := `DELETE FROM dirty_issues WHERE issue_id NOT IN (SELECT id FROM issues)`
	result, err := db.ExecContext(ctx, orphanQuery)
	if err != nil {
		log.log("Dirty flush: failed to remove orphaned entries: %v", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		log.log("Dirty flush: removed %d orphaned dirty_issues entries", n)
	}

	// Step 2: Remove dirty entries for issues already exported with current content
	// Only applies when export_hashes table is populated
	var exportFlushQuery string
	if backend == "sqlite" {
		exportFlushQuery = `
			DELETE FROM dirty_issues WHERE issue_id IN (
				SELECT d.issue_id FROM dirty_issues d
				JOIN issues i ON d.issue_id = i.id
				JOIN export_hashes e ON e.issue_id = i.id
				WHERE i.content_hash = e.content_hash
			)
		`
	} else {
		// MySQL/Dolt: can't reference target table in subquery DELETE
		exportFlushQuery = `
			DELETE d FROM dirty_issues d
			JOIN issues i ON d.issue_id = i.id
			JOIN export_hashes e ON e.issue_id = i.id
			WHERE i.content_hash = e.content_hash
		`
	}
	result, err = db.ExecContext(ctx, exportFlushQuery)
	if err != nil {
		log.log("Dirty flush: failed to remove already-exported entries: %v", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		log.log("Dirty flush: removed %d already-exported dirty_issues entries", n)
	}
}

// getRemoteSyncInterval returns the interval for periodic remote sync.
// Configuration sources (in order of precedence):
//  1. BEADS_REMOTE_SYNC_INTERVAL environment variable
//  2. remote-sync-interval in .beads/config.yaml
//  3. DefaultRemoteSyncInterval (30s)
//
// Accepts Go duration strings like:
// - "30s" (30 seconds)
// - "1m" (1 minute)
// - "5m" (5 minutes)
// - "0" or "0s" (disables periodic sync - use with caution)
//
// Minimum allowed value is 5 seconds to prevent excessive load.
func getRemoteSyncInterval(log daemonLogger) time.Duration {
	// config.GetDuration handles both config.yaml and env var (env takes precedence)
	duration := config.GetDuration("remote-sync-interval")
	
	// If config returns 0, it could mean:
	// 1. User explicitly set "0" to disable
	// 2. Config not found (use default)
	// Check if there's an explicit value set
	if duration == 0 {
		// Check if user explicitly set it to 0 via env var
		if envVal := os.Getenv("BEADS_REMOTE_SYNC_INTERVAL"); envVal == "0" || envVal == "0s" {
			log.log("Warning: remote-sync-interval is 0, periodic remote sync disabled")
			return 24 * time.Hour * 365
		}
		// Otherwise use default
		return DefaultRemoteSyncInterval
	}

	// Minimum 5 seconds to prevent excessive load
	if duration > 0 && duration < 5*time.Second {
		log.log("Warning: remote-sync-interval too low (%v), using minimum 5s", duration)
		return 5 * time.Second
	}

	// Log if using non-default value
	if duration != DefaultRemoteSyncInterval {
		log.log("Using custom remote sync interval: %v", duration)
	}
	return duration
}
