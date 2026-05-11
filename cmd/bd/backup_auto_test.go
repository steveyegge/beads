//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/git"
	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
)

func TestIsBackupAutoEnabled(t *testing.T) {
	// Cannot be parallel: modifies global primeHasGitRemote and env vars.

	tests := []struct {
		name       string
		envVal     string // "\x00" = not set, "" = set to empty, "true"/"false"/"0" = explicit
		hasRemote  bool
		wantResult bool
	}{
		{
			name:       "default + git remote → enabled",
			envVal:     "\x00",
			hasRemote:  true,
			wantResult: true,
		},
		{
			name:       "default + no git remote → disabled",
			envVal:     "\x00",
			hasRemote:  false,
			wantResult: false,
		},
		{
			name:       "explicit true + no remote → enabled",
			envVal:     "true",
			hasRemote:  false,
			wantResult: true,
		},
		{
			name:       "explicit false + remote → disabled",
			envVal:     "false",
			hasRemote:  true,
			wantResult: false,
		},
		{
			name:       "explicit 0 + remote → disabled",
			envVal:     "0",
			hasRemote:  true,
			wantResult: false,
		},
		{
			name:       "empty string + remote → disabled (env set = explicit)",
			envVal:     "",
			hasRemote:  true,
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Stub primeHasGitRemote
			orig := primeHasGitRemote
			primeHasGitRemote = func() bool { return tt.hasRemote }
			t.Cleanup(func() { primeHasGitRemote = orig })

			// Set env var: "\x00" = unset, anything else = set to that value
			if tt.envVal == "\x00" {
				os.Unsetenv("BD_BACKUP_ENABLED")
				t.Cleanup(func() { os.Unsetenv("BD_BACKUP_ENABLED") })
			} else {
				t.Setenv("BD_BACKUP_ENABLED", tt.envVal)
			}

			config.ResetForTesting()
			t.Cleanup(func() { config.ResetForTesting() })
			if err := config.Initialize(); err != nil {
				t.Fatalf("config.Initialize: %v", err)
			}

			got := isBackupAutoEnabled()
			if got != tt.wantResult {
				t.Errorf("isBackupAutoEnabled() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

// TestAutoBackupLockSerializesForks pins the contract that maybeAutoBackup
// relies on: when one bd CLI fork holds the auto-backup flock at
// <backup-dir>/.backup.lock, a second TryLock on the same path returns an
// error that lockfile.IsLocked recognizes. The skip-on-contention branch in
// maybeAutoBackup depends on that recognition; if a refactor of util.TryLock
// or lockfile.IsLocked breaks the recognition, concurrent forks would race
// past the lock and back into the noisy DOLT_BACKUP rm/add path this test
// guards against.
func TestAutoBackupLockSerializesForks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".backup.lock")

	held, err := util.TryLock(lockPath)
	if err != nil {
		t.Fatalf("first TryLock on uncontended path: %v", err)
	}
	defer held.Unlock()

	_, err = util.TryLock(lockPath)
	if err == nil {
		t.Fatalf("second TryLock should fail while the lock is held")
	}
	if !lockfile.IsLocked(err) {
		t.Fatalf("second TryLock error should be IsLocked-recognized; got: %v", err)
	}
}

// recordingStore satisfies storage.DoltStorage just enough for
// maybeAutoBackup to traverse the pre-lock guards. It records whether
// GetCurrentCommit — the first store call past the .backup.lock guard —
// was invoked. The embedded DoltStorage is intentionally nil; any
// method call on it would nil-panic, which is exactly the assertion we
// want for any post-lock store reference.
type recordingStore struct {
	storage.DoltStorage
	getCurrentCommitCalled atomic.Bool
}

func (r *recordingStore) GetCurrentCommit(ctx context.Context) (string, error) {
	r.getCurrentCommitCalled.Store(true)
	return "", nil
}

// TestMaybeAutoBackupSkipsOnLockContention drives maybeAutoBackup directly and
// asserts it short-circuits at the .backup.lock guard when another holder is
// alive — pinning the production caller's behavior, not just the TryLock
// primitive it depends on. TestAutoBackupLockSerializesForks above would still
// pass if a refactor dropped the TryLock call from maybeAutoBackup entirely;
// this test would not.
//
// The function has two subtests so the contended-lock assertion is anchored to
// a control: in the uncontended case maybeAutoBackup must reach the store
// (proving the test setup gets past every pre-lock guard), and in the
// contended case it must not.
func TestMaybeAutoBackupSkipsOnLockContention(t *testing.T) {
	// Cannot be parallel: subtests assign to global store and global config.

	setup := func(t *testing.T) (ctx context.Context, repoDir string, rec *recordingStore) {
		t.Helper()
		ctx = context.Background()

		// Use the backup.git-repo path so backupDir() resolves without any
		// FindBeadsDir / cwd walk-up dance — point it at a minimal
		// git-init'd temp dir.
		repoDir = t.TempDir()
		if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		t.Setenv("BD_BACKUP_GIT_REPO", repoDir)

		beads.ResetCaches()
		git.ResetCaches()
		t.Cleanup(func() {
			beads.ResetCaches()
			git.ResetCaches()
		})

		// Force-enable auto-backup independent of the git-remote heuristic.
		t.Setenv("BD_BACKUP_ENABLED", "true")
		config.ResetForTesting()
		t.Cleanup(func() { config.ResetForTesting() })
		if err := config.Initialize(); err != nil {
			t.Fatalf("config.Initialize: %v", err)
		}

		originalStore := store
		t.Cleanup(func() { store = originalStore })
		rec = &recordingStore{}
		store = rec

		if err := os.MkdirAll(filepath.Join(repoDir, "backup"), 0o700); err != nil {
			t.Fatalf("mkdir backup dir: %v", err)
		}
		return ctx, repoDir, rec
	}

	t.Run("lock held → skip", func(t *testing.T) {
		ctx, repoDir, rec := setup(t)

		// Pre-acquire the auto-backup lock from this goroutine to simulate
		// another bd CLI fork already holding it.
		lockPath := filepath.Join(repoDir, "backup", ".backup.lock")
		held, err := util.TryLock(lockPath)
		if err != nil {
			t.Fatalf("pre-acquire TryLock: %v", err)
		}
		defer held.Unlock()

		maybeAutoBackup(ctx)

		if rec.getCurrentCommitCalled.Load() {
			t.Fatalf("maybeAutoBackup reached store.GetCurrentCommit despite contended lock — lock-skip path did not run")
		}

		statePath := filepath.Join(repoDir, "backup", "backup_state.json")
		if _, err := os.Stat(statePath); err == nil {
			t.Fatalf("maybeAutoBackup wrote %s despite contended lock", statePath)
		} else if !os.IsNotExist(err) {
			t.Fatalf("unexpected error stat'ing %s: %v", statePath, err)
		}
	})

	t.Run("lock free → control: store is consulted", func(t *testing.T) {
		ctx, _, rec := setup(t)

		// No pre-acquired lock. maybeAutoBackup should pass the lock guard
		// and reach store.GetCurrentCommit. The recordingStore returns
		// ("", nil), so the downstream runBackupExport will fail at the
		// BackupStore type assertion — that's expected and produces a
		// "Warning: auto-backup failed: storage backend does not support
		// backup operations" line on stderr. We only care that the store
		// was consulted; if it wasn't, the "skip" subtest above would
		// pass for the wrong reason.
		maybeAutoBackup(ctx)

		if !rec.getCurrentCommitCalled.Load() {
			t.Fatalf("maybeAutoBackup did not consult store.GetCurrentCommit even with an uncontended lock — the 'skip' subtest is passing for the wrong reason")
		}
	})
}
