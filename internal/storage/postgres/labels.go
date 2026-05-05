package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// AddLabel attaches a label to an issue. ON CONFLICT DO NOTHING makes the
// call idempotent.
func (s *PostgresStore) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		return tx.AddLabel(ctx, issueID, label, actor)
	})
}

// addLabelRow is the in-transaction worker. table is "labels" or "wisp_labels".
func addLabelRow(ctx context.Context, c pgxConn, table, issueID, label string) error {
	table = guardTable(table)
	//nolint:gosec // table allowlisted
	stmt := fmt.Sprintf(`INSERT INTO %s (issue_id, label) VALUES ($1, $2) ON CONFLICT (issue_id, label) DO NOTHING`, table)
	if _, err := c.Exec(ctx, stmt, issueID, label); err != nil {
		return wrapErr(fmt.Sprintf("add label to %s", table), err)
	}
	return nil
}

// RemoveLabel detaches a label.
func (s *PostgresStore) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		return tx.RemoveLabel(ctx, issueID, label, actor)
	})
}

func removeLabelRow(ctx context.Context, c pgxConn, table, issueID, label string) error {
	table = guardTable(table)
	//nolint:gosec // table allowlisted
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE issue_id = $1 AND label = $2`, table)
	if _, err := c.Exec(ctx, stmt, issueID, label); err != nil {
		return wrapErr(fmt.Sprintf("remove label from %s", table), err)
	}
	return nil
}

// GetLabels returns the labels attached to an issue, looking in both the
// regular and wisp tables.
func (s *PostgresStore) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	labels, err := getLabelsFromTable(ctx, s.pool, "labels", issueID)
	if err != nil {
		return nil, err
	}
	if len(labels) == 0 {
		return getLabelsFromTable(ctx, s.pool, "wisp_labels", issueID)
	}
	return labels, nil
}

func getLabelsFromTable(ctx context.Context, c pgxConn, table, issueID string) ([]string, error) {
	table = guardTable(table)
	//nolint:gosec // table allowlisted
	q := fmt.Sprintf(`SELECT label FROM %s WHERE issue_id = $1 ORDER BY label`, table)
	rows, err := c.Query(ctx, q, issueID)
	if err != nil {
		return nil, wrapErr(fmt.Sprintf("get labels from %s", table), err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, wrapErr(fmt.Sprintf("scan labels from %s", table), err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetIssuesByLabel returns the persistent issues that carry the named label.
func (s *PostgresStore) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM issues i
		WHERE EXISTS (SELECT 1 FROM labels l WHERE l.issue_id = i.id AND l.label = $1)
		ORDER BY i.created_at DESC
	`, prefixedIssueColumns("i"))
	rows, err := s.pool.Query(ctx, q, label)
	if err != nil {
		return nil, wrapErr("get issues by label", err)
	}
	return scanIssues(rows)
}

// prefixedIssueColumns returns issueColumns with each name aliased by `<prefix>.`
// for use in joins. v1 callers always pass "i"; the parameter is kept so future
// joins can use a different alias without changing the helper signature.
func prefixedIssueColumns(prefix string) string { //nolint:unparam // see comment above
	if prefix == "" {
		return issueColumns
	}
	return rewriteColumnsWithPrefix(issueColumns, prefix)
}

func rewriteColumnsWithPrefix(cols, prefix string) string {
	var b []byte
	inWord := false
	for i := 0; i < len(cols); i++ {
		c := cols[i]
		if isIdentByte(c) {
			if !inWord {
				inWord = true
				b = append(b, prefix...)
				b = append(b, '.')
			}
			b = append(b, c)
		} else {
			inWord = false
			b = append(b, c)
		}
	}
	return string(b)
}

func isIdentByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// GetLabelsForIssues returns labels for many issues, keyed by issue ID.
func (s *PostgresStore) GetLabelsForIssues(ctx context.Context, issueIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(issueIDs))
	if len(issueIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		args[i] = id
	}
	ph := joinPlaceholders(1, len(issueIDs))
	for _, table := range []string{"labels", "wisp_labels"} {
		//nolint:gosec // table allowlisted
		q := fmt.Sprintf(`SELECT issue_id, label FROM %s WHERE issue_id IN (%s) ORDER BY issue_id, label`, table, ph)
		rows, err := s.pool.Query(ctx, q, args...)
		if err != nil {
			return nil, wrapErr(fmt.Sprintf("get labels for issues from %s", table), err)
		}
		if err := func() error {
			defer rows.Close()
			for rows.Next() {
				var id, lab string
				if err := rows.Scan(&id, &lab); err != nil {
					return err
				}
				out[id] = append(out[id], lab)
			}
			return rows.Err()
		}(); err != nil {
			return nil, wrapErr("scan labels for issues", err)
		}
	}
	return out, nil
}

// errLabelNotFound is returned when a label lookup misses; reserved for callers
// that want to distinguish "no labels" from connectivity errors.
var errLabelNotFound = errors.New("postgres: label not found")

var _ pgx.Rows = (pgx.Rows)(nil) // keep pgx import even if scan helpers move out
