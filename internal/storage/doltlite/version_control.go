//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/steveyegge/beads/internal/storage"
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
	db, cleanup, err = s.activeDB(ctx)
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
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		return s.recordSyntheticCommit(ctx, tx, message)
	})
}

// CommitWithConfig commits all working set changes including config.
// DoltliteStore.Commit already includes config via DOLT_ADD('-A'),
// so this is just an alias to satisfy the VersionControl interface (GH#3216).
func (s *DoltliteStore) CommitWithConfig(ctx context.Context, message string) error {
	return s.Commit(ctx, message)
}

func (s *DoltliteStore) AddRemote(ctx context.Context, name, url string) error {
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dolt_remotes (name, url) VALUES (?, ?)
			ON CONFLICT(name) DO UPDATE SET url = excluded.url
		`, name, url)
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
	return s.withExclusiveLock(ctx, func() error {
		srcPath, err := s.branchDBPath(s.branch)
		if err != nil {
			return err
		}
		dstPath, err := s.branchDBPath(name)
		if err != nil {
			return err
		}
		if srcPath == dstPath {
			return fmt.Errorf("create branch %s: branch already exists", name)
		}
		if err := s.closePersistentDB(); err != nil {
			return err
		}
		defer func() { _ = s.openPersistentDB(ctx) }()
		if _, err := os.Stat(dstPath); err == nil {
			return fmt.Errorf("create branch %s: branch already exists", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("create branch %s: %w", name, err)
		}
		db, cleanup, err := OpenSQL(ctx, s.dataDir, s.database, name)
		if err != nil {
			return err
		}
		defer func() { _ = cleanup() }()
		var head string
		_ = db.QueryRowContext(ctx, "SELECT head_hash FROM doltlite_refs WHERE branch = ?", s.branch).Scan(&head)
		if _, err := db.ExecContext(ctx, `
			INSERT INTO doltlite_refs (branch, head_hash) VALUES (?, ?)
			ON CONFLICT(branch) DO UPDATE SET head_hash = excluded.head_hash
		`, name, head); err != nil {
			return err
		}
		return nil
	})
}

func (s *DoltliteStore) Checkout(ctx context.Context, branch string) error {
	_, err := os.Stat(func() string {
		path, _ := s.branchDBPath(branch)
		return path
	}())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checkout branch %s: branch not found", branch)
		}
		return err
	}
	s.branch = branch
	if err := s.resetPersistentDB(ctx); err != nil {
		return err
	}
	return s.withConn(ctx, false, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO doltlite_refs (branch, head_hash)
			SELECT ?, COALESCE((SELECT head_hash FROM doltlite_refs WHERE branch = ?), '')
			WHERE NOT EXISTS (SELECT 1 FROM doltlite_refs WHERE branch = ?)
		`, branch, branch, branch); err != nil {
			return err
		}
		return nil
	})
}

func (s *DoltliteStore) CurrentBranch(ctx context.Context) (string, error) {
	return s.branch, nil
}

func (s *DoltliteStore) DeleteBranch(ctx context.Context, branch string) error {
	if branch == s.branch {
		return fmt.Errorf("delete branch %s: cannot delete current branch", branch)
	}
	return s.withExclusiveLock(ctx, func() error {
		path, err := s.branchDBPath(branch)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete branch %s: branch not found", branch)
			}
			return fmt.Errorf("delete branch %s: %w", branch, err)
		}
		return nil
	})
}

func (s *DoltliteStore) ListBranches(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, err
	}
	branches := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if branch, ok := branchFromDBFilename(s.database, entry.Name()); ok {
			branches = append(branches, branch)
		}
	}
	if !slices.Contains(branches, s.branch) {
		branches = append(branches, s.branch)
	}
	slices.Sort(branches)
	return branches, nil
}

// ---------------------------------------------------------------------------
// Version control operations
// ---------------------------------------------------------------------------

// commitAuthor returns the author string for merge commits.
const commitAuthor = commitName + " <" + commitEmail + ">"

func (s *DoltliteStore) CommitExists(ctx context.Context, commitHash string) (bool, error) {
	var count int
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM doltlite_commits
			WHERE branch = ? AND (hash = ? OR hash LIKE ?)
		`, s.branch, commitHash, commitHash+"%").Scan(&count)
	})
	return count > 0, err
}

func (s *DoltliteStore) Status(ctx context.Context) (*storage.Status, error) {
	return &storage.Status{
		Staged:   []storage.StatusEntry{},
		Unstaged: []storage.StatusEntry{},
	}, nil
}

func (s *DoltliteStore) Log(ctx context.Context, limit int) ([]storage.CommitInfo, error) {
	var query string
	var args []any
	if limit > 0 {
		query = `
			SELECT hash, committer, email, date, message
			FROM doltlite_commits
			WHERE branch = ?
			ORDER BY date DESC
			LIMIT ?
		`
		args = []any{s.branch, limit}
	} else {
		query = `
			SELECT hash, committer, email, date, message
			FROM doltlite_commits
			WHERE branch = ?
			ORDER BY date DESC
		`
		args = []any{s.branch}
	}
	var commits []storage.CommitInfo
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
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
	return nil, fmt.Errorf("doltlite merge unsupported for sqlite backend")
}

func (s *DoltliteStore) GetConflicts(ctx context.Context) ([]storage.Conflict, error) {
	return nil, nil
}

func (s *DoltliteStore) ResolveConflicts(ctx context.Context, table string, strategy string) error {
	return fmt.Errorf("doltlite conflict resolution unsupported for sqlite backend")
}

// ---------------------------------------------------------------------------
// Remote operations
// ---------------------------------------------------------------------------

const defaultRemote = "origin"

func (s *DoltliteStore) RemoveRemote(ctx context.Context, name string) error {
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM dolt_remotes WHERE name = ?", name)
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
