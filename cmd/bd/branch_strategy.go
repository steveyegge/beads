package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/steveyegge/beads/internal/storage/dolt"
)

// executePostCommitStrategy runs strategy-specific logic after a Dolt commit
// on a registered branch. Called from the auto-commit and transact paths.
//
// stay-on-main:      no-op (writes go directly to Dolt main, no branch exists)
// merge-with-branch: no-op here (isolated until explicit merge via bd vc merge)
// merge-on-close:    no-op here (merge happens on issue close)
func executePostCommitStrategy(ctx context.Context, s *dolt.DoltStore) {
	if s == nil || s.IsClosed() {
		return
	}

	branch, err := s.CurrentBranch(ctx)
	if err != nil || branch == "main" {
		return // on main, nothing to do
	}

	info, err := s.GetBranchInfo(ctx, branch)
	if err != nil || info == nil {
		return // unregistered branch, no strategy
	}

	switch info.MergeStrategy {
	case "stay-on-main":
		// Writes go directly to Dolt main — no merge needed
		return
	case "merge-with-branch":
		// Fully isolated until explicit merge (bd vc merge)
		return
	case "merge-on-close":
		// No-op on regular commits — merge happens on close
		return
	}
}

// mergeOnClose performs a merge-on-close merge: merge the branch to main when
// an issue is closed. On conflict, logs a warning and skips — the merge will
// retry on the next close. This is safer than auto-resolving with --theirs
// because the close merge is a convenience, not a critical path.
func mergeOnClose(ctx context.Context, s *dolt.DoltStore, branchName string) error {
	mergeDB, err := openMergeConnection(s)
	if err != nil {
		return fmt.Errorf("failed to open merge connection: %w", err)
	}
	defer mergeDB.Close()

	if _, err := mergeDB.ExecContext(ctx, "CALL DOLT_CHECKOUT('main')"); err != nil {
		return fmt.Errorf("merge connection checkout main: %w", err)
	}

	_, err = mergeDB.ExecContext(ctx, "CALL DOLT_MERGE(?)", branchName)
	if err != nil {
		// On conflict: abort and skip. Will retry on next close.
		if strings.Contains(err.Error(), "conflict") {
			_, _ = mergeDB.ExecContext(ctx, "CALL DOLT_MERGE('--abort')")
			return fmt.Errorf("merge conflict from %s to main (will retry on next close): %w", branchName, err)
		}
		return fmt.Errorf("merge %s to main: %w", branchName, err)
	}

	commitMsg := fmt.Sprintf("merge-on-close: %s → main", branchName)
	_, err = mergeDB.ExecContext(ctx, "CALL DOLT_COMMIT('-Am', ?, '--author', ?)",
		commitMsg, s.CommitAuthor())
	if err != nil {
		if isDoltNothingToCommit(err) {
			return nil
		}
		return fmt.Errorf("commit merge on main: %w", err)
	}

	return nil
}

// maybeMergeOnClose checks if the current branch uses merge-on-close strategy
// and triggers a merge to main. Called from the close command after an issue is closed.
func maybeMergeOnClose(ctx context.Context, s *dolt.DoltStore) {
	if s == nil || s.IsClosed() {
		return
	}

	branch, err := s.CurrentBranch(ctx)
	if err != nil || branch == "main" {
		return
	}

	info, err := s.GetBranchInfo(ctx, branch)
	if err != nil || info == nil {
		return
	}

	if info.MergeStrategy != "merge-on-close" {
		return
	}

	if err := mergeOnClose(ctx, s, branch); err != nil {
		log.Printf("merge-on-close failed for branch %s: %v", branch, err)
	}
}

// openMergeConnection opens a separate database connection for merge operations.
// This avoids the MaxOpenConns(1) trap and keeps the caller's session on its branch.
func openMergeConnection(s *dolt.DoltStore) (*sql.DB, error) {
	connStr := s.ConnectionString()
	if connStr == "" {
		return nil, fmt.Errorf("no connection string available (embedded mode not supported for branch strategies)")
	}

	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}
