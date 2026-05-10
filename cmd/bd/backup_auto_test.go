//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/lockfile"
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
