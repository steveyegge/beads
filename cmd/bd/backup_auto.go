package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
)

// isBackupAutoEnabled returns whether backup should run.
// If user explicitly configured backup.enabled, use that.
// Otherwise, auto-enable when a git remote exists.
func isBackupAutoEnabled() bool {
	if config.GetValueSource("backup.enabled") != config.SourceDefault {
		return config.GetBool("backup.enabled")
	}
	return primeHasGitRemote()
}

// maybeAutoBackup runs a Dolt-native backup if enabled and the throttle interval has passed.
// Called from PersistentPostRun after auto-commit.
func maybeAutoBackup(ctx context.Context) {
	// Skip backup entirely when running as a git hook (post-checkout, post-merge, etc.).
	// Git hooks call 'bd hooks run' which goes through PersistentPostRun — without this
	// guard, every git checkout/merge/rebase triggers a backup on the current branch.
	if os.Getenv("BD_GIT_HOOK") == "1" {
		debug.Logf("backup: skipping — running as git hook\n")
		return
	}

	if !isBackupAutoEnabled() {
		return
	}
	if store == nil {
		return
	}
	if lm, ok := storage.UnwrapStore(store).(storage.LifecycleManager); ok && lm.IsClosed() {
		return
	}

	dir, err := backupDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-backup skipped: %v\n", err)
		return
	}

	state, err := loadBackupState(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-backup skipped: %v\n", err)
		return
	}

	// Throttle: skip if we backed up recently
	interval := config.GetDuration("backup.interval")
	if interval == 0 {
		interval = 15 * time.Minute
	}
	if !state.Timestamp.IsZero() && time.Since(state.Timestamp) < interval {
		debug.Logf("backup: throttled (last backup %s ago, interval %s)\n",
			time.Since(state.Timestamp).Round(time.Second), interval)
		return
	}

	// Serialize concurrent auto-backups across bd CLI forks. In server mode
	// multiple bd processes share one Dolt sql-server; without this lock,
	// PersistentPostRun fires per fork and all forks race past the throttle
	// window into BackupDatabase's rm-then-add, producing duplicate
	// CALL DOLT_BACKUP traffic. Non-blocking: if another fork holds the lock,
	// this one skips — the holder's run already covers our changes.
	lock, err := util.TryLock(filepath.Join(dir, ".backup.lock"))
	if err != nil {
		if lockfile.IsLocked(err) {
			debug.Logf("backup: another process is backing up, skipping\n")
			return
		}
		fmt.Fprintf(os.Stderr, "Warning: auto-backup skipped: %v\n", err)
		return
	}
	defer lock.Unlock()

	// Change detection: skip if nothing changed
	currentCommit, err := store.GetCurrentCommit(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-backup skipped: failed to get current commit: %v\n", err)
		return
	}
	if currentCommit == state.LastDoltCommit && state.LastDoltCommit != "" {
		debug.Logf("backup: no changes since last backup\n")
		return
	}

	// Run the backup (force=true since we already checked change detection above)
	if _, err := runBackupExport(ctx, true); err != nil {
		if !isQuiet() && !jsonOutput {
			fmt.Fprintf(os.Stderr, "Warning: auto-backup failed: %v\n", err)
		}
		debug.Logf("backup: error: %v\n", err)
		return
	}

	debug.Logf("backup: completed successfully\n")
}
