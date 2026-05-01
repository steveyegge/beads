//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/storage/versioncontrolops"
)

// withDBConn opens a database connection configured for the store's native
// doltlite branch and passes it to fn without starting an explicit SQL
// transaction. Version-control functions manage their own transaction boundary.
func (s *DoltliteStore) withDBConn(ctx context.Context, fn func(db versioncontrolops.DBConn) error) (err error) {
	if s.closed.Load() {
		return errClosed
	}

	var db *sql.DB
	var cleanup func() error
	db, cleanup, err = s.activeDB(ctx)
	if err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, cleanup())
		s.cleanGitRemoteCacheGarbage()
	}()

	return fn(db)
}

func (s *DoltliteStore) withDBWrite(ctx context.Context, fn func(db versioncontrolops.DBConn) error) error {
	return s.withExclusiveLock(ctx, func() error {
		return s.withRetry(ctx, func() error {
			return s.withDBConn(ctx, fn)
		})
	})
}

// commitAuthor returns the author string for native doltlite commits.
const commitAuthor = commitName + " <" + commitEmail + ">"

func commitAllNative(ctx context.Context, db versioncontrolops.DBConn, message string) error {
	if message == "" {
		message = "doltlite: snapshot"
	}
	_, err := db.ExecContext(ctx, "SELECT dolt_commit('-A', '-m', ?, '--author', ?)", message, commitAuthor)
	if err != nil && !issueops.IsNothingToCommitError(err) {
		return fmt.Errorf("doltlite commit: %w", err)
	}
	return nil
}

func (s *DoltliteStore) Commit(ctx context.Context, message string) error {
	return s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
		return commitAllNative(ctx, db, message)
	})
}

// CommitWithConfig commits all working set changes including config.
func (s *DoltliteStore) CommitWithConfig(ctx context.Context, message string) error {
	return s.Commit(ctx, message)
}

func (s *DoltliteStore) AddRemote(ctx context.Context, name, url string) error {
	return s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
		var existing string
		err := db.QueryRowContext(ctx, "SELECT url FROM dolt_remotes WHERE name = ?", name).Scan(&existing)
		switch {
		case err == nil && existing == url:
			return nil
		case err == nil:
			if _, rmErr := db.ExecContext(ctx, "SELECT dolt_remote('remove', ?)", name); rmErr != nil {
				return fmt.Errorf("remove existing remote %s: %w", name, rmErr)
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return fmt.Errorf("lookup remote %s: %w", name, err)
		}

		if _, err := db.ExecContext(ctx, "SELECT dolt_remote('add', ?, ?)", name, url); err != nil {
			return fmt.Errorf("add remote %s: %w", name, err)
		}
		return nil
	})
}

func (s *DoltliteStore) HasRemote(ctx context.Context, name string) (bool, error) {
	var count int
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		return db.QueryRowContext(ctx, "SELECT count(*) FROM dolt_remotes WHERE name = ?", name).Scan(&count)
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
	return s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
		if _, err := db.ExecContext(ctx, "SELECT dolt_branch(?)", name); err != nil {
			return fmt.Errorf("create branch %s: %w", name, err)
		}
		return nil
	})
}

func (s *DoltliteStore) Checkout(ctx context.Context, branch string) error {
	if err := s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
		if _, err := db.ExecContext(ctx, "SELECT dolt_checkout(?)", branch); err != nil {
			return fmt.Errorf("checkout branch %s: %w", branch, err)
		}
		return nil
	}); err != nil {
		return err
	}
	s.branch = branch
	return nil
}

func (s *DoltliteStore) CurrentBranch(ctx context.Context) (string, error) {
	var branch string
	err := s.withDBConn(ctx, func(db versioncontrolops.DBConn) error {
		var err error
		branch, err = versioncontrolops.CurrentBranch(ctx, db)
		return err
	})
	if err != nil {
		return "", err
	}
	if branch != "" {
		s.branch = branch
	}
	return branch, nil
}

func (s *DoltliteStore) DeleteBranch(ctx context.Context, branch string) error {
	current, err := s.CurrentBranch(ctx)
	if err != nil {
		return err
	}
	if branch == current {
		return fmt.Errorf("delete branch %s: cannot delete current branch", branch)
	}
	return s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
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
	query := "SELECT commit_hash, committer, email, date, message FROM dolt_log ORDER BY date DESC"
	var args []any
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
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
			var date any
			if err := rows.Scan(&c.Hash, &c.Author, &c.Email, &date, &c.Message); err != nil {
				return fmt.Errorf("scan commit: %w", err)
			}
			c.Date = parseDoltliteTimeValue(date)
			commits = append(commits, c)
		}
		return rows.Err()
	})
	return commits, err
}

func parseDoltliteTimeValue(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		return parseDoltliteTime(t)
	case []byte:
		return parseDoltliteTime(string(t))
	case int64:
		return time.Unix(t, 0).UTC()
	default:
		return time.Time{}
	}
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
	err := s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
		if _, mergeErr := db.ExecContext(ctx, "SELECT dolt_merge(?)", branch); mergeErr != nil {
			c, conflictErr := versioncontrolops.GetConflicts(ctx, db)
			if conflictErr == nil && len(c) > 0 {
				conflicts = c
				return nil
			}
			return fmt.Errorf("merge branch %s: %w", branch, mergeErr)
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
	if table == "" || !validIdentifier.MatchString(table) {
		return fmt.Errorf("invalid table name: %s", table)
	}
	var flag string
	switch strategy {
	case "ours":
		flag = "--ours"
	case "theirs":
		flag = "--theirs"
	default:
		return fmt.Errorf("unknown conflict resolution strategy: %s", strategy)
	}
	return s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
		if _, err := db.ExecContext(ctx, "SELECT dolt_conflicts_resolve(?, ?)", flag, table); err != nil {
			return fmt.Errorf("resolve conflicts: %w", err)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Remote operations
// ---------------------------------------------------------------------------

const defaultRemote = "origin"

func (s *DoltliteStore) RemoveRemote(ctx context.Context, name string) error {
	return s.withDBWrite(ctx, func(db versioncontrolops.DBConn) error {
		if _, err := db.ExecContext(ctx, "SELECT dolt_remote('remove', ?)", name); err != nil {
			return fmt.Errorf("remove remote %s: %w", name, err)
		}
		return nil
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

var errDoltliteBackupUnsupported = errors.New("doltlite backup operations unsupported")

func (s *DoltliteStore) BackupAdd(ctx context.Context, name, url string) error {
	return errDoltliteBackupUnsupported
}

func (s *DoltliteStore) BackupSync(ctx context.Context, name string) error {
	return errDoltliteBackupUnsupported
}

func (s *DoltliteStore) BackupRemove(ctx context.Context, name string) error {
	return errDoltliteBackupUnsupported
}

func (s *DoltliteStore) BackupDatabase(ctx context.Context, dir string) error {
	return errDoltliteBackupUnsupported
}

func (s *DoltliteStore) RestoreDatabase(ctx context.Context, dir string, force bool) error {
	return errDoltliteBackupUnsupported
}
