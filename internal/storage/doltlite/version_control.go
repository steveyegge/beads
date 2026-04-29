//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/schema"
	"github.com/steveyegge/beads/internal/storage/versioncontrolops"
)

// withDBConn opens a short-lived database connection configured for the
// store's database and branch and passes it to fn. Unlike withConn, no
// transaction is started — this is required for Dolt stored procedures
// (CALL DOLT_BRANCH, CALL DOLT_MERGE, etc.) that cannot run inside
// explicit SQL transactions.
func (s *DoltliteStore) withDBConn(ctx context.Context, fn func(db versioncontrolops.DBConn) error) (err error) {
	if s.closed.Load() {
		return errClosed
	}

	var db *sql.DB
	var cleanup func() error
	db, cleanup, err = OpenSQL(ctx, s.dataDir, s.database, s.branch)
	if err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, cleanup())
		// Best-effort cleanup of orphaned tmp_pack_* files left by git
		// fetch in the Dolt git-remote-cache. Rate-limited internally.
		s.cleanGitRemoteCacheGarbage()
	}()

	return fn(db)
}

func (s *DoltliteStore) Commit(ctx context.Context, message string) error {
	return s.withExclusiveLock(ctx, func() error {
		return s.withRetry(ctx, func() error {
			return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
				if _, err := db.ExecContext(ctx, "SELECT dolt_add('-A')"); err != nil {
					return fmt.Errorf("dolt add: %w", err)
				}
				if _, err := db.ExecContext(ctx, "SELECT dolt_commit('-m', ?)", message); err != nil {
					return fmt.Errorf("dolt commit: %w", err)
				}
				return nil
			})
		})
	})
}

// CommitWithConfig commits all working set changes including config.
// DoltliteStore.Commit already includes config via DOLT_ADD('-A'),
// so this is just an alias to satisfy the VersionControl interface (GH#3216).
func (s *DoltliteStore) CommitWithConfig(ctx context.Context, message string) error {
	return s.Commit(ctx, message)
}

func (s *DoltliteStore) AddRemote(ctx context.Context, name, url string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_remote('add', ?, ?)", name, url)
		return err
	})
}

func (s *DoltliteStore) HasRemote(ctx context.Context, name string) (bool, error) {
	var count int
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT count(*) FROM dolt_remotes WHERE name = ?", name).Scan(&count)
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ---------------------------------------------------------------------------
// Branch operations
// ---------------------------------------------------------------------------

func (s *DoltliteStore) Branch(ctx context.Context, name string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		if _, err := db.ExecContext(ctx, "SELECT dolt_branch(?)", name); err != nil {
			return fmt.Errorf("create branch %s: %w", name, err)
		}
		return schema.CreateIgnoredTablesSQLite(ctx, db)
	})
}

func (s *DoltliteStore) Checkout(ctx context.Context, branch string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		if _, err := db.ExecContext(ctx, "SELECT dolt_checkout(?)", branch); err != nil {
			return fmt.Errorf("checkout branch %s: %w", branch, err)
		}
		return schema.CreateIgnoredTablesSQLite(ctx, db)
	})
}

func (s *DoltliteStore) CurrentBranch(ctx context.Context) (string, error) {
	var branch string
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		branch, err = versioncontrolops.CurrentBranch(ctx, db)
		return err
	})
	return branch, err
}

func (s *DoltliteStore) DeleteBranch(ctx context.Context, branch string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		if _, err := db.ExecContext(ctx, "SELECT dolt_branch('-D', ?)", branch); err != nil {
			return fmt.Errorf("delete branch %s: %w", branch, err)
		}
		return nil
	})
}

func (s *DoltliteStore) ListBranches(ctx context.Context) ([]string, error) {
	var branches []string
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		branches, err = versioncontrolops.ListBranches(ctx, db)
		return err
	})
	return branches, err
}

// ---------------------------------------------------------------------------
// Version control operations
// ---------------------------------------------------------------------------

// commitAuthor returns the author string for merge commits.
const commitAuthor = commitName + " <" + commitEmail + ">"

func (s *DoltliteStore) CommitExists(ctx context.Context, commitHash string) (bool, error) {
	var exists bool
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		exists, err = versioncontrolops.CommitExists(ctx, db, commitHash)
		return err
	})
	return exists, err
}

func (s *DoltliteStore) Status(ctx context.Context) (*storage.Status, error) {
	var status *storage.Status
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		status, err = versioncontrolops.Status(ctx, db)
		return err
	})
	return status, err
}

func (s *DoltliteStore) Log(ctx context.Context, limit int) ([]storage.CommitInfo, error) {
	var query string
	var args []any
	if limit > 0 {
		query = "SELECT commit_hash, committer, email, date, message FROM dolt_log ORDER BY date DESC LIMIT ?"
		args = []any{limit}
	} else {
		query = "SELECT commit_hash, committer, email, date, message FROM dolt_log ORDER BY date DESC"
	}
	var commits []storage.CommitInfo
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("get log: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c storage.CommitInfo
			var date string
			if err := rows.Scan(&c.Hash, &c.Author, &c.Email, &date, &c.Message); err != nil {
				return fmt.Errorf("scan commit: %w", err)
			}
			c.Date = parseDoltliteTime(date)
			commits = append(commits, c)
		}
		return rows.Err()
	})
	return commits, err
}

func parseDoltliteTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *DoltliteStore) Merge(ctx context.Context, branch string) ([]storage.Conflict, error) {
	var conflicts []storage.Conflict
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_merge(?)", branch)
		if err != nil {
			c, conflictErr := versioncontrolops.GetConflicts(ctx, db)
			if conflictErr == nil && len(c) > 0 {
				conflicts = c
				return nil
			}
			return fmt.Errorf("merge branch %s: %w", branch, err)
		}
		return nil
	})
	return conflicts, err
}

func (s *DoltliteStore) GetConflicts(ctx context.Context) ([]storage.Conflict, error) {
	var conflicts []storage.Conflict
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		conflicts, err = versioncontrolops.GetConflicts(ctx, db)
		return err
	})
	return conflicts, err
}

func (s *DoltliteStore) ResolveConflicts(ctx context.Context, table string, strategy string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		switch strategy {
		case "ours":
			_, err := db.ExecContext(ctx, "SELECT dolt_conflicts_resolve('--ours', ?)", table)
			return err
		case "theirs":
			_, err := db.ExecContext(ctx, "SELECT dolt_conflicts_resolve('--theirs', ?)", table)
			return err
		default:
			return fmt.Errorf("unknown conflict resolution strategy: %s", strategy)
		}
	})
}

// ---------------------------------------------------------------------------
// Remote operations
// ---------------------------------------------------------------------------

const defaultRemote = "origin"

func (s *DoltliteStore) RemoveRemote(ctx context.Context, name string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_remote('remove', ?)", name)
		return err
	})
}

func (s *DoltliteStore) ListRemotes(ctx context.Context) ([]storage.RemoteInfo, error) {
	var remotes []storage.RemoteInfo
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		remotes, err = versioncontrolops.ListRemotes(ctx, db)
		return err
	})
	return remotes, err
}

func (s *DoltliteStore) Push(ctx context.Context) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_push(?, ?)", defaultRemote, s.branch)
		return err
	})
}

func (s *DoltliteStore) Pull(ctx context.Context) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_pull(?, ?)", defaultRemote, s.branch)
		return err
	})
}

func (s *DoltliteStore) ForcePush(ctx context.Context) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_push(?, ?, '--force')", defaultRemote, s.branch)
		return err
	})
}

func (s *DoltliteStore) PushRemote(ctx context.Context, remote string, force bool) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		if force {
			_, err := db.ExecContext(ctx, "SELECT dolt_push(?, ?, '--force')", remote, s.branch)
			return err
		}
		_, err := db.ExecContext(ctx, "SELECT dolt_push(?, ?)", remote, s.branch)
		return err
	})
}

func (s *DoltliteStore) PullRemote(ctx context.Context, remote string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_pull(?, ?)", remote, s.branch)
		return err
	})
}

func (s *DoltliteStore) Fetch(ctx context.Context, peer string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_fetch(?)", peer)
		return err
	})
}

func (s *DoltliteStore) PushTo(ctx context.Context, peer string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		_, err := db.ExecContext(ctx, "SELECT dolt_push(?, ?)", peer, s.branch)
		return err
	})
}

func (s *DoltliteStore) PullFrom(ctx context.Context, peer string) ([]storage.Conflict, error) {
	// Auto-commit pending changes before pull to prevent
	// "cannot merge with uncommitted changes" errors.
	if _, err := s.CommitPending(ctx, "beads"); err != nil {
		return nil, fmt.Errorf("commit pending before pull: %w", err)
	}

	var conflicts []storage.Conflict
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		if _, pullErr := db.ExecContext(ctx, "SELECT dolt_pull(?, ?)", peer, s.branch); pullErr != nil {
			c, conflictErr := versioncontrolops.GetConflicts(ctx, db)
			if conflictErr == nil && len(c) > 0 {
				conflicts = c
				return nil
			}
			return fmt.Errorf("pull from %s: %w", peer, pullErr)
		}
		return nil
	})
	return conflicts, err
}

// ---------------------------------------------------------------------------
// Backup operations
// ---------------------------------------------------------------------------

func (s *DoltliteStore) BackupAdd(ctx context.Context, name, url string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		return versioncontrolops.BackupAdd(ctx, db, name, url)
	})
}

func (s *DoltliteStore) BackupSync(ctx context.Context, name string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		return versioncontrolops.BackupSync(ctx, db, name)
	})
}

func (s *DoltliteStore) BackupRemove(ctx context.Context, name string) error {
	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		return versioncontrolops.BackupRemove(ctx, db, name)
	})
}

// BackupDatabase registers dir as a file:// Dolt backup remote and syncs
// the database to it. The dir must exist locally. This preserves full Dolt
// commit history.
func (s *DoltliteStore) BackupDatabase(ctx context.Context, dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("backup destination does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backup destination is not a directory: %s", dir)
	}

	backupURL, err := versioncontrolops.DirToFileURL(dir)
	if err != nil {
		return err
	}
	backupName := "backup_export"

	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		// Register as a backup remote (idempotent — remove first if exists).
		_ = versioncontrolops.BackupRemove(ctx, db, backupName)
		if err := versioncontrolops.BackupAdd(ctx, db, backupName, backupURL); err != nil {
			// Another backup (e.g. "default" registered by `bd backup init`) may
			// already point to this URL. In that case, sync using the existing
			// remote name rather than failing.
			if conflict := versioncontrolops.ExtractAddressConflictName(err); conflict != "" {
				if syncErr := versioncontrolops.BackupSync(ctx, db, conflict); syncErr != nil {
					return fmt.Errorf("sync to backup: %w", syncErr)
				}
				return nil
			}
			return fmt.Errorf("register backup remote: %w", err)
		}
		if err := versioncontrolops.BackupSync(ctx, db, backupName); err != nil {
			return fmt.Errorf("sync to backup: %w", err)
		}
		return nil
	})
}

// RestoreDatabase restores the database from a Dolt backup at dir.
// The dir must exist locally and contain a valid Dolt backup.
// When force is true, an existing database is overwritten.
func (s *DoltliteStore) RestoreDatabase(ctx context.Context, dir string, force bool) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("backup source does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backup source is not a directory: %s", dir)
	}

	backupURL, err := versioncontrolops.DirToFileURL(dir)
	if err != nil {
		return err
	}

	return s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		return versioncontrolops.BackupRestore(ctx, db, backupURL, s.database, force)
	})
}
