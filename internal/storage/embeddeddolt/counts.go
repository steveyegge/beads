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

// Each split dep table has exactly one typed target column under the new
// schema; count queries read that column directly.

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

// CountIssuesByGroup returns per-group issue counts. groupBy is one of:
// status, priority, type, assignee, label.
func (s *EmbeddedDoltStore) CountIssuesByGroup(ctx context.Context, filter types.IssueFilter, groupBy string) (map[string]int, error) {
	var result map[string]int
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.CountIssuesByGroupInTx(ctx, tx, filter, groupBy)
		return err
	})
	return result, err
}

// CountDependents counts edges across all six split dep tables so the total
// matches GetDependentsWithMetadata. Counted in separate top-level queries
// per table and summed in Go.
func (s *EmbeddedDoltStore) CountDependents(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		for _, t := range issueops.AllDepTables() {
			col := issueops.DepTargetColumnForTable(t)
			var c int64
			//nolint:gosec // G201: t and col are hardcoded constants.
			if err := tx.QueryRowContext(ctx,
				fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s = ?`, t, col), issueID).Scan(&c); err != nil {
				return err
			}
			n += c
		}
		return nil
	})
	return n, err
}

// CountDependencies counts both dependency tables so the total matches
// GetDependenciesWithMetadata: a wisp's outgoing edges live in
// `wisp_dependencies`, a permanent issue's in `dependencies`. Counted as two
// separate queries summed in Go (see CountDependents for why a single combined
// query is avoided).
func (s *EmbeddedDoltStore) CountDependencies(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		for _, t := range issueops.AllDepTables() {
			var c int64
			//nolint:gosec // G201: t is a hardcoded constant.
			if err := tx.QueryRowContext(ctx,
				fmt.Sprintf(`SELECT count(*) FROM %s WHERE source_id = ?`, t), issueID).Scan(&c); err != nil {
				return err
			}
			n += c
		}
		return nil
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

// CountDependentsByStatus counts both dependency tables, joining each to its
// home issue table (dependencies→issues, wisp_dependencies→wisps), so wisp
// dependents are included the same way GetDependentsWithMetadata includes them.
// Counted as two separate queries summed in Go (see CountDependents for why a
// single combined query is avoided).
func (s *EmbeddedDoltStore) CountDependentsByStatus(ctx context.Context, issueID string, status types.Status) (int64, error) {
	var n int64
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		pairs := []struct{ depTable, issueTable string }{
			{"issue_issue_dependencies", "issues"},
			{"issue_wisp_dependencies", "issues"},
			{"issue_external_dependencies", "issues"},
			{"wisp_issue_dependencies", "wisps"},
			{"wisp_wisp_dependencies", "wisps"},
			{"wisp_external_dependencies", "wisps"},
		}
		for _, p := range pairs {
			col := issueops.DepTargetColumnForTable(p.depTable)
			var c int64
			//nolint:gosec // G201: p.depTable, p.issueTable, col are hardcoded constants.
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(
				`SELECT count(*) FROM %s d JOIN %s s ON s.id = d.source_id WHERE d.%s = ? AND s.status = ?`,
				p.depTable, p.issueTable, col), issueID, string(status)).Scan(&c); err != nil {
				return err
			}
			n += c
		}
		return nil
	})
	return n, err
}
