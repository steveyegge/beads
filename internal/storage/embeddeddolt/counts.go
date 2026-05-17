//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

func (s *EmbeddedDoltStore) CountIssues(ctx context.Context, query string, filter types.IssueFilter) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
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

func (s *EmbeddedDoltStore) CountDependents(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies WHERE depends_on_id = ?`, issueID).Scan(&n)
	})
	return n, err
}

func (s *EmbeddedDoltStore) CountDependencies(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies WHERE issue_id = ?`, issueID).Scan(&n)
	})
	return n, err
}

func (s *EmbeddedDoltStore) CountIssueComments(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM comments WHERE issue_id = ?`, issueID).Scan(&n)
	})
	return n, err
}

func (s *EmbeddedDoltStore) CountEvents(ctx context.Context, issueID string, limit int) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
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

func (s *EmbeddedDoltStore) CountDependentsByStatus(ctx context.Context, issueID string, status types.Status) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies d
			 JOIN issues i ON i.id = d.issue_id
			 WHERE d.depends_on_id = ? AND i.status = ?`,
			issueID, string(status)).Scan(&n)
	})
	return n, err
}
