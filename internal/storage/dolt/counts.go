package dolt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// depTargetExpr resolves a dependency row's target id from the split physical
// columns instead of the STORED generated `depends_on_id` column. Both
// `dependencies` and `wisp_dependencies` define depends_on_id as
// GENERATED ALWAYS AS (COALESCE(depends_on_issue_id, depends_on_wisp_id,
// depends_on_external)). Queries must filter on the base columns when the
// pure-Go GMS analyzer can prune the columns the generated column derives
// from (a count(*) projects no real columns and used to fail with "column
// depends_on_id could not be found in any table in scope"); the slice path
// projects real columns, so it can use depends_on_id directly. This matches
// issueops.DepTargetExpr on main. The dependents iterator
// (iter_dependents.go) is the remaining user.
const depTargetExpr = "COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)"

// CountIssues returns the number of issues matching query and filter.
// Filter.Limit and Filter.Offset are ignored; all other fields apply.
// Wisps-merge semantics follow SearchIssues: SkipWisps=true counts the
// durable issues table only, otherwise the wisps tier is merged in (GH#4387).
func (s *DoltStore) CountIssues(ctx context.Context, query string, filter types.IssueFilter) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		count, err := issueops.CountIssuesInTx(ctx, tx, query, filter)
		if err != nil {
			return err
		}
		n = int64(count)
		return nil
	})
	return n, err
}

// CountIssuesByGroup returns per-group issue counts. groupBy is one of:
// status, priority, type, assignee, label.
func (s *DoltStore) CountIssuesByGroup(ctx context.Context, filter types.IssueFilter, groupBy string) (map[string]int, error) {
	var result map[string]int
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.CountIssuesByGroupInTx(ctx, tx, filter, groupBy)
		return err
	})
	return result, err
}

// CountDependents returns the number of issues that depend on issueID.
// Counts both dependency planes through issueops.CountDependencyEdgesInTx so
// the total matches GetDependentsWithMetadata's listing rule: each far end
// once (a mirror row in both tables is one dependent, whose durable copy
// wins), and a missing wisp_dependencies is an empty plane, not a failure
// (GH#3445).
func (s *DoltStore) CountDependents(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		count, err := issueops.CountDependencyEdgesInTx(ctx, tx, issueID, domain.DepDirectionIn, nil)
		if err != nil {
			return err
		}
		n = count
		return nil
	})
	return n, err
}

// CountDependencies returns the number of issues that issueID depends on.
// Counts both dependency planes through issueops.CountDependencyEdgesInTx so
// the total matches GetDependenciesWithMetadata's listing rule (see
// CountDependents).
func (s *DoltStore) CountDependencies(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		count, err := issueops.CountDependencyEdgesInTx(ctx, tx, issueID, domain.DepDirectionOut, nil)
		if err != nil {
			return err
		}
		n = count
		return nil
	})
	return n, err
}

// CountIssueComments returns the number of comments on an issue.
func (s *DoltStore) CountIssueComments(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		// Route by wisp status exactly as GetIssueComments does, so the count
		// and the rows it summarizes read the same table (GH#5565).
		table := "comments"
		if issueops.IsActiveWispInTx(ctx, tx, issueID) {
			table = "wisp_comments"
		}
		//nolint:gosec // G201: table is hardcoded
		return tx.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE issue_id = ?`, table), issueID).Scan(&n)
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
// and are in the given status. Counts both dependency tables, joining each to
// its home issue table (dependencies→issues, wisp_dependencies→wisps), so wisp
// dependents are included the same way GetDependentsWithMetadata includes them.
// Counted as two separate queries summed in Go (see CountDependents for why a
// single combined query is avoided).
func (s *DoltStore) CountDependentsByStatus(ctx context.Context, issueID string, status types.Status) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var perm, wisp int64
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies d
			 JOIN issues i ON i.id = d.issue_id
			 WHERE COALESCE(d.depends_on_issue_id, d.depends_on_wisp_id, d.depends_on_external) = ? AND i.status = ?`,
			issueID, string(status)).Scan(&perm); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM wisp_dependencies d
			 JOIN wisps w ON w.id = d.issue_id
			 WHERE COALESCE(d.depends_on_issue_id, d.depends_on_wisp_id, d.depends_on_external) = ? AND w.status = ?`,
			issueID, string(status)).Scan(&wisp); err != nil {
			return err
		}
		n = perm + wisp
		return nil
	})
	return n, err
}
