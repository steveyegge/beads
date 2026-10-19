package versioncontrolops

import (
	"context"
	"fmt"
)

// Compact squashes old Dolt commits into a single base commit while preserving
// recent commits via cherry-pick. The recipe:
//  1. Create temp branch at the boundary commit (last old commit)
//  2. Checkout temp branch
//  3. Soft-reset to initial commit (collapses old history into working set)
//  4. Commit as single squashed base, dated like the boundary commit
//  5. Cherry-pick each recent commit on top, as its original committer at its original date
//  6. Checkout main, hard-reset to temp branch
//  7. Delete temp branch
//
// A cherry-pick keeps the commit's author, author date and message, but records
// the session's committer and the time of the replay. Without the original
// committer dates, every timestamp stored before the compaction (a tracker's
// last_sync) predates the whole rewritten history, so a dolt_history lookup
// filtered on commit_date at that timestamp finds only the initial commit and
// every linked issue reads as changed. The committer is set through Dolt's
// session variables rather than amended afterwards: an amend rewrites the
// author too, and its --author flag rejects names containing parentheses.
//
// Callers should run PruneRemoteRefs and then DoltGC afterward to reclaim disk
// space — remote-tracking refs still anchor the pre-compact chain, and GC
// alone reclaims nothing while they exist (bd-agctw).
//
// conn must be a single database connection (not a pooled *sql.DB) since the
// stored procedures rely on session-scoped state (current branch, working set).
func Compact(ctx context.Context, conn DBConn, initialHash, boundaryHash string, oldCommits int, recentHashes []string) (retErr error) {
	meta, err := compactCommitMetadata(ctx, conn)
	if err != nil {
		return fmt.Errorf("compact step %q: %w", "read commit metadata", err)
	}
	boundary, ok := meta[boundaryHash]
	if !ok {
		return fmt.Errorf("compact: boundary commit %s not found in dolt_log", boundaryHash)
	}
	for _, hash := range recentHashes {
		if _, ok := meta[hash]; !ok {
			return fmt.Errorf("compact: recent commit %s not found in dolt_log", hash)
		}
	}

	branchCreated := false
	committerSet := false

	// The committer session variables outlive this function on the caller's
	// connection; clear them whether the replay finished or not.
	defer func() {
		if committerSet {
			_, _ = conn.ExecContext(ctx, "SET @@dolt_committer_name = '', @@dolt_committer_email = '', @@dolt_committer_date = ''")
		}
	}()

	// Best-effort cleanup: if any step fails after creating the temp branch,
	// try to return to main and delete the temp branch so future compactions
	// aren't blocked by a leftover branch.
	defer func() {
		if retErr != nil && branchCreated {
			_, _ = conn.ExecContext(ctx, "CALL DOLT_CHECKOUT('main')")
			_, _ = conn.ExecContext(ctx, "CALL DOLT_BRANCH('-D', 'compact-tmp')")
		}
	}()

	execSQL := func(name, query string, args ...interface{}) error {
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("compact step %q: %w", name, err)
		}
		return nil
	}

	if err := execSQL("create temp branch", "CALL DOLT_BRANCH('compact-tmp', ?)", boundaryHash); err != nil {
		return err
	}
	branchCreated = true

	if err := execSQL("checkout temp", "CALL DOLT_CHECKOUT('compact-tmp')"); err != nil {
		return err
	}
	if err := execSQL("soft reset to initial", "CALL DOLT_RESET('--soft', ?)", initialHash); err != nil {
		return err
	}
	msg := fmt.Sprintf("compact: squash %d commits into base snapshot", oldCommits)
	if err := execSQL("commit squashed base", "CALL DOLT_COMMIT('-Am', ?, '--date', ?)", msg, boundary.date); err != nil {
		return err
	}

	// --allow-empty: the preserved window can contain empty commits (a Dolt
	// auto-commit with no table change, or a bd create double-commit whose
	// leading member has 0 changed tables). Without this flag DOLT_CHERRY_PICK
	// aborts the entire replay at the first empty commit with Error 1105
	// ("The previous cherry-pick commit is empty. Use --allow-empty ..."),
	// leaving compaction permanently blocked on active databases. See #3815.
	for _, hash := range recentHashes {
		short := hash[:min(8, len(hash))]
		m := meta[hash]
		committerSet = true
		if err := execSQL(fmt.Sprintf("set committer %s", short),
			"SET @@dolt_committer_name = ?, @@dolt_committer_email = ?, @@dolt_committer_date = ?",
			m.committer, m.email, m.date); err != nil {
			return err
		}
		if err := execSQL(fmt.Sprintf("cherry-pick %s", short), "CALL DOLT_CHERRY_PICK('--allow-empty', ?)", hash); err != nil {
			return err
		}
	}

	if err := execSQL("checkout main", "CALL DOLT_CHECKOUT('main')"); err != nil {
		return err
	}
	if err := execSQL("reset main to compacted", "CALL DOLT_RESET('--hard', 'compact-tmp')"); err != nil {
		return err
	}
	if err := execSQL("delete temp branch", "CALL DOLT_BRANCH('-D', 'compact-tmp')"); err != nil {
		return err
	}

	return nil
}

type compactCommit struct {
	committer, email, date string
}

// compactCommitMetadata reads every commit on the current branch with its
// committer and commit date. The date is formatted the way Dolt parses
// dolt_committer_date, to the millisecond, so restoring it keeps the original
// order of commits made within the same second.
func compactCommitMetadata(ctx context.Context, conn DBConn) (map[string]compactCommit, error) {
	rows, err := conn.QueryContext(ctx,
		"SELECT commit_hash, committer, email, DATE_FORMAT(date, '%Y-%m-%dT%H:%i:%s.%fZ') FROM dolt_log")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	meta := make(map[string]compactCommit)
	for rows.Next() {
		var hash, committer, email, date string
		if err := rows.Scan(&hash, &committer, &email, &date); err != nil {
			return nil, err
		}
		meta[hash] = compactCommit{committer: committer, email: email, date: date}
	}
	return meta, rows.Err()
}
