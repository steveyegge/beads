package versioncontrolops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// Compact squashes old Dolt commits into a single base commit while preserving
// recent commits via cherry-pick. The recipe:
//  1. Create temp branch at the boundary commit (last old commit)
//  2. Checkout temp branch
//  3. Soft-reset to initial commit (collapses old history into working set)
//  4. Commit as single squashed base
//  5. Cherry-pick each recent commit on top
//  6. Checkout main, soft-reset to temp branch, and commit any rows main
//     held outside the plan
//  7. Delete temp branch
//
// The plan (boundaryHash, recentHashes) is read from main's log before
// Compact runs. Compact refuses before step 1 when main's head is not the
// head the plan was built from, and checks again just before the reset in
// step 6: a commit that reached main after the plan was read is in no
// cherry-pick, so resetting main past it would delete a write that had
// already reported success. On a refusal main is left untouched and the temp
// branch removed.
//
// Compact takes the planned head to be the newest commit in the plan, which
// is main's head unless the log's date order disagrees with its parent
// order; then Compact refuses although nothing moved. A caller that can read
// main's head (MainHead) BEFORE it reads the log should call CompactAtHead
// with that head instead, so each refusal names the true head and its cause.
//
// Callers should run PruneRemoteRefs and then DoltGC afterward to reclaim disk
// space — remote-tracking refs still anchor the pre-compact chain, and GC
// alone reclaims nothing while they exist (bd-agctw).
//
// conn must be a single database connection (not a pooled *sql.DB) since the
// stored procedures rely on session-scoped state (current branch, working set).
func Compact(ctx context.Context, conn DBConn, initialHash, boundaryHash string, oldCommits int, recentHashes []string) error {
	_, err := CompactAtHead(ctx, conn, planNewest(boundaryHash, recentHashes), initialHash, boundaryHash, oldCommits, recentHashes)
	return err
}

// CompactResult says what CompactAtHead left on main.
type CompactResult struct {
	// SquashedHead is the head of the squashed branch: the planned history
	// rewritten as one base commit plus the replayed recent commits. Its data
	// equals the planned head's when the squash and replay changed nothing.
	SquashedHead string
	// KeptRowsCommit is the commit holding the rows main had outside the plan
	// (a commit that reached main after the last head check, or rows a writer
	// left uncommitted), made on top of SquashedHead. It is empty when main
	// held no such rows, and then main's head is SquashedHead.
	KeptRowsCommit string
}

// CompactAtHead is Compact with the head of main the plan was built from read
// by the caller. Read it with MainHead before reading the log: a commit that
// lands between the two reads is then in the plan but not at plannedHead, so
// CompactAtHead refuses rather than resetting main past it. It also refuses
// when main is still at plannedHead but the plan's newest commit is another
// commit, because a plan built from a log whose date order disagrees with
// its parent order does not cover main.
func CompactAtHead(ctx context.Context, conn DBConn, plannedHead, initialHash, boundaryHash string, oldCommits int, recentHashes []string) (res CompactResult, retErr error) {
	branchCreated := false

	if err := requireMainAt(ctx, conn, plannedHead, "before building the squashed branch"); err != nil {
		return res, err
	}
	if newest := planNewest(boundaryHash, recentHashes); newest != plannedHead {
		return res, fmt.Errorf("%w: main is at %s, but the newest commit in the plan is %s; "+
			"the log's date order disagrees with its parent order, so main was left unchanged",
			ErrPlanNotAtHead, plannedHead, newest)
	}

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
		return res, err
	}
	branchCreated = true

	if err := execSQL("checkout temp", "CALL DOLT_CHECKOUT('compact-tmp')"); err != nil {
		return res, err
	}
	if err := execSQL("soft reset to initial", "CALL DOLT_RESET('--soft', ?)", initialHash); err != nil {
		return res, err
	}
	msg := fmt.Sprintf("compact: squash %d commits into base snapshot", oldCommits)
	if err := execSQL("commit squashed base", "CALL DOLT_COMMIT('-Am', ?)", msg); err != nil {
		return res, err
	}

	// --allow-empty: the preserved window can contain empty commits (a Dolt
	// auto-commit with no table change, or a bd create double-commit whose
	// leading member has 0 changed tables). Without this flag DOLT_CHERRY_PICK
	// aborts the entire replay at the first empty commit with Error 1105
	// ("The previous cherry-pick commit is empty. Use --allow-empty ..."),
	// leaving compaction permanently blocked on active databases. See #3815.
	for _, hash := range recentHashes {
		if err := execSQL(fmt.Sprintf("cherry-pick %s", hash[:min(8, len(hash))]), "CALL DOLT_CHERRY_PICK('--allow-empty', ?)", hash); err != nil {
			return res, err
		}
	}

	if err := execSQL("checkout main", "CALL DOLT_CHECKOUT('main')"); err != nil {
		return res, err
	}
	if err := requireMainAt(ctx, conn, plannedHead, "before resetting main"); err != nil {
		return res, err
	}
	// --soft: main's working set keeps every row it holds, including a
	// commit that reached main after the check above and rows a writer left
	// uncommitted. The squashed tree equals the planned head's, so whatever
	// differs now is exactly those rows, and they are committed here so the
	// compaction never leaves main dirty.
	if err := execSQL("reset main to compacted", "CALL DOLT_RESET('--soft', 'compact-tmp')"); err != nil {
		return res, err
	}
	// Read from compact-tmp, which no writer touches, not from main, which a
	// writer may move at any moment.
	if err := conn.QueryRowContext(ctx, "SELECT DOLT_HASHOF('compact-tmp')").Scan(&res.SquashedHead); err != nil {
		return res, fmt.Errorf("compact step %q: %w", "read the squashed head", err)
	}
	var dirty int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dolt_status").Scan(&dirty); err != nil {
		return res, fmt.Errorf("compact step %q: %w", "read status after reset", err)
	}
	if dirty > 0 {
		// DOLT_COMMIT answers with the hash of the commit it made.
		if err := conn.QueryRowContext(ctx, "CALL DOLT_COMMIT('-Am', 'compact: commit rows main held outside the plan')").Scan(&res.KeptRowsCommit); err != nil {
			return res, fmt.Errorf("compact step %q: %w", "commit rows kept by the reset", err)
		}
	}
	if err := execSQL("delete temp branch", "CALL DOLT_BRANCH('-D', 'compact-tmp')"); err != nil {
		return res, err
	}

	return res, nil
}

// ErrMainMoved reports that main's head is no longer the commit a compaction
// plan was built from, so the compaction stopped without changing main.
var ErrMainMoved = errors.New("main moved during compaction")

// ErrPlanNotAtHead reports that main is at the head read before the plan,
// but the plan's newest commit is a different commit, so the compaction
// stopped without changing main.
var ErrPlanNotAtHead = errors.New("compaction plan does not end at main's head")

// planNewest is the newest commit a plan names: the last commit it replays
// or, when it replays none, the boundary commit itself.
func planNewest(boundaryHash string, recentHashes []string) string {
	if len(recentHashes) > 0 {
		return recentHashes[len(recentHashes)-1]
	}
	return boundaryHash
}

// MainHead returns the commit at the head of main.
func MainHead(ctx context.Context, conn DBConn) (string, error) {
	var head string
	if err := conn.QueryRowContext(ctx, "SELECT DOLT_HASHOF('main')").Scan(&head); err != nil {
		return "", err
	}
	return head, nil
}

// TablesChangedBetween names the tables whose rows or schema differ between
// the commits from and to, read from DOLT_DIFF_SUMMARY. Called with the head a
// compaction was planned from and the squashed head (CompactResult), it
// answers whether the squash and replay changed any data: an empty answer
// means they did not. Rows the compaction kept are in KeptRowsCommit, above
// the squashed head, so they are not in that answer.
// Call it before GC, which may remove the planned head's chain. On success the
// result is never nil, so a JSON encoder writes [] rather than null.
func TablesChangedBetween(ctx context.Context, conn DBConn, from, to string) ([]string, error) {
	rows, err := conn.QueryContext(ctx,
		"SELECT from_table_name, to_table_name FROM DOLT_DIFF_SUMMARY(?, ?)", from, to)
	if err != nil {
		return nil, fmt.Errorf("diff summary %s..%s: %w", from, to, err)
	}
	defer rows.Close()
	changed := []string{}
	for rows.Next() {
		var fromName, toName sql.NullString
		if err := rows.Scan(&fromName, &toName); err != nil {
			return nil, fmt.Errorf("diff summary %s..%s: %w", from, to, err)
		}
		name := toName.String
		if name == "" {
			name = fromName.String
		}
		changed = append(changed, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("diff summary %s..%s: %w", from, to, err)
	}
	sort.Strings(changed)
	return changed, nil
}

// requireMainAt returns ErrMainMoved, in words, unless main's head is want.
func requireMainAt(ctx context.Context, conn DBConn, want, when string) error {
	head, err := MainHead(ctx, conn)
	if err != nil {
		return fmt.Errorf("compact: reading the head of main %s: %w", when, err)
	}
	if head != want {
		return fmt.Errorf("%w: the plan was built from main at %s, but main is at %s (checked %s), "+
			"so main was left unchanged; run compact again",
			ErrMainMoved, want, head, when)
	}
	return nil
}
