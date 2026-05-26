// Package dolt — iter_issues.go
//
// Streaming iterator over the issues table. Holds a DEDICATED *sql.Conn
// for the cursor's lifetime so per-row label hydration can run on a SECOND
// pool connection without deadlocking against the cursor connection
// (be-jaavsb §4.1, §4.3).
//
// This iterator queries only the `issues` table — wisp routing happens in
// the slice-returning SearchIssues which merges wisps and issues. The
// streaming path matches PG semantics: callers that need wisps stream
// IterWisps separately.
package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// doltIssueIter is a streaming iterator over the issues table.
//
// It holds a dedicated *sql.Conn for its lifetime: the cursor is busy
// holding *sql.Rows so per-row label hydration must run on a SECOND
// connection from the *sql.DB pool. Forgetting this is the classic
// deadlock — the iterator is locked, the hydration query waits for the
// same conn, and the pool blocks forever. The dedicated-conn pattern is
// pinned by the parity test (concurrent iterators).
type doltIssueIter struct {
	s      *DoltStore
	conn   *sql.Conn
	rows   *sql.Rows
	cur    *types.Issue
	err    error
	closed bool
}

// IterIssues streams issues matching the filter from the `issues` table.
//
// The streaming path queries only the issues table (wisps are streamed
// separately via IterWisps). The slice path SearchIssues merges both for
// backward compatibility — that merge defeats streaming because the wisp
// pass needs a seen-set keyed by ID across the full issues result set.
func (s *DoltStore) IterIssues(ctx context.Context, query string, filter types.IssueFilter) (storage.Iter[types.Issue], error) {
	if s.closed.Load() {
		return nil, ErrStoreClosed
	}
	whereClauses, args, err := issueops.BuildIssueFilterClauses(query, filter, issueops.IssuesFilterTables)
	if err != nil {
		return nil, fmt.Errorf("iter issues: build filter: %w", err)
	}
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}
	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	//nolint:gosec // G201: whereSQL contains column comparisons with ?, limitSQL is a safe integer
	q := fmt.Sprintf(`SELECT %s FROM issues %s ORDER BY priority ASC, created_at DESC, id ASC%s`,
		issueops.IssueSelectColumns, whereSQL, limitSQL)

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("iter issues: acquire conn: %w", err)
	}
	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("iter issues: query: %w", err)
	}
	return &doltIssueIter{s: s, conn: conn, rows: rows}, nil
}

func (it *doltIssueIter) Next(ctx context.Context) bool {
	if it.err != nil || it.closed {
		return false
	}
	if err := ctx.Err(); err != nil {
		it.err = err
		return false
	}
	if !it.rows.Next() {
		it.err = it.rows.Err()
		return false
	}
	iss, err := issueops.ScanIssueFrom(it.rows)
	if err != nil {
		it.err = fmt.Errorf("iter issues: scan: %w", err)
		return false
	}
	// Hydrate labels on a SEPARATE pool connection (NOT it.conn — the
	// cursor conn is busy holding it.rows). s.db.QueryContext acquires a
	// fresh conn from the pool for the duration of the label query.
	labels, err := iterFetchLabels(ctx, it.s.db, "labels", iss.ID)
	if err != nil {
		it.err = fmt.Errorf("iter issues: hydrate labels: %w", err)
		return false
	}
	iss.Labels = labels
	it.cur = iss
	return true
}

func (it *doltIssueIter) Value() *types.Issue { return it.cur }
func (it *doltIssueIter) Err() error          { return it.err }

func (it *doltIssueIter) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	if it.rows != nil {
		_ = it.rows.Close()
	}
	if it.conn != nil {
		_ = it.conn.Close()
	}
	return nil
}

// iterFetchLabels reads labels for a single issue ID from the named label
// table on a fresh pool connection. Used by streaming iterators where the
// cursor connection is busy holding *sql.Rows.
//
//nolint:gosec // G201: labelTable is a hardcoded constant from caller ("labels" or "wisp_labels")
func iterFetchLabels(ctx context.Context, db *sql.DB, labelTable, issueID string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		fmt.Sprintf(`SELECT label FROM %s WHERE issue_id = ? ORDER BY label`, labelTable),
		issueID)
	if err != nil {
		return nil, fmt.Errorf("fetch labels from %s: %w", labelTable, err)
	}
	defer rows.Close()

	var labels []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, fmt.Errorf("fetch labels: scan: %w", err)
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}
