package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// CountIssues returns the number of issues matching query and filter.
// Filter.Limit and Filter.Offset are ignored; all other fields apply.
func (s *DoltStore) CountIssues(ctx context.Context, query string, filter types.IssueFilter) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		whereClauses, args, err := issueops.BuildIssueFilterClauses(query, filter, issueops.IssuesFilterTables)
		if err != nil {
			return err
		}
		where := ""
		if len(whereClauses) > 0 {
			where = " WHERE " + strings.Join(whereClauses, " AND ")
		}
		//nolint:gosec // table name is a static constant; placeholders are bound
		q := fmt.Sprintf(`SELECT count(*) FROM issues%s`, where)
		return tx.QueryRowContext(ctx, q, args...).Scan(&n)
	})
	return n, err
}

// CountDependents returns the number of issues that depend on issueID.
func (s *DoltStore) CountDependents(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies WHERE depends_on_id = ?`, issueID).Scan(&n)
	})
	return n, err
}

// CountDependencies returns the number of issues that issueID depends on.
func (s *DoltStore) CountDependencies(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies WHERE issue_id = ?`, issueID).Scan(&n)
	})
	return n, err
}

// CountIssueComments returns the number of comments on an issue.
func (s *DoltStore) CountIssueComments(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM comments WHERE issue_id = ?`, issueID).Scan(&n)
	})
	return n, err
}

// CountEvents returns the number of audit events for an issue, capped at limit
// (or unbounded if limit == 0).
func (s *DoltStore) CountEvents(ctx context.Context, issueID string, limit int) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM events WHERE issue_id = ?`, issueID).Scan(&n)
	})
	if err != nil {
		return 0, err
	}
	if limit > 0 && n > int64(limit) {
		n = int64(limit)
	}
	return n, nil
}

// CountDependentsByStatus returns the number of issues that depend on issueID
// and are in the given status.
func (s *DoltStore) CountDependentsByStatus(ctx context.Context, issueID string, status types.Status) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies d
			 JOIN issues i ON i.id = d.issue_id
			 WHERE d.depends_on_id = ? AND i.status = ?`,
			issueID, string(status)).Scan(&n)
	})
	return n, err
}
