package ephemeral

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// AddDependency adds a dependency between two ephemeral issues.
func (s *Store) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO dependencies (issue_id, depends_on_id, type, created_by, metadata, thread_id)
		 VALUES (?, ?, ?, ?, '{}', ?)`,
		dep.IssueID, dep.DependsOnID, dep.Type, actor, dep.ThreadID)
	if err != nil {
		return fmt.Errorf("add ephemeral dependency: %w", err)
	}
	return nil
}

// RemoveDependency removes a dependency.
func (s *Store) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM dependencies WHERE issue_id = ? AND depends_on_id = ?`,
		issueID, dependsOnID)
	if err != nil {
		return fmt.Errorf("remove ephemeral dependency: %w", err)
	}
	return nil
}

// GetDependencies returns issues that this issue depends on.
func (s *Store) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+issueSelectColumns+` FROM issues
		 WHERE id IN (SELECT depends_on_id FROM dependencies WHERE issue_id = ?)`, issueID)
	if err != nil {
		return nil, fmt.Errorf("get ephemeral dependencies: %w", err)
	}
	defer rows.Close()
	return scanIssues(rows)
}

// GetDependents returns issues that depend on this issue.
func (s *Store) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+issueSelectColumns+` FROM issues
		 WHERE id IN (SELECT issue_id FROM dependencies WHERE depends_on_id = ?)`, issueID)
	if err != nil {
		return nil, fmt.Errorf("get ephemeral dependents: %w", err)
	}
	defer rows.Close()
	return scanIssues(rows)
}

// GetDependencyRecords returns raw dependency records for an issue.
func (s *Store) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT issue_id, depends_on_id, type, created_at, created_by, thread_id
		 FROM dependencies WHERE issue_id = ?`, issueID)
	if err != nil {
		return nil, fmt.Errorf("get ephemeral dependency records: %w", err)
	}
	defer rows.Close()

	var deps []*types.Dependency
	for rows.Next() {
		var d types.Dependency
		var createdAt, threadID sql.NullString
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &d.Type, &createdAt, &d.CreatedBy, &threadID); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			d.CreatedAt = parseTime(createdAt.String)
		}
		if threadID.Valid {
			d.ThreadID = threadID.String
		}
		deps = append(deps, &d)
	}
	return deps, rows.Err()
}

// GetDependencyRecordsForIssues returns dependency records for multiple issues.
func (s *Store) GetDependencyRecordsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Dependency, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(issueIDs))
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT issue_id, depends_on_id, type, created_at, created_by, thread_id
		 FROM dependencies WHERE issue_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("get ephemeral dependency records for issues: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]*types.Dependency)
	for rows.Next() {
		var d types.Dependency
		var createdAt, threadID sql.NullString
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &d.Type, &createdAt, &d.CreatedBy, &threadID); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			d.CreatedAt = parseTime(createdAt.String)
		}
		if threadID.Valid {
			d.ThreadID = threadID.String
		}
		result[d.IssueID] = append(result[d.IssueID], &d)
	}
	return result, rows.Err()
}

// GetBlockingInfoForIssues returns blocking info for a set of issues.
func (s *Store) GetBlockingInfoForIssues(ctx context.Context, issueIDs []string) (blockedByMap map[string][]string, blocksMap map[string][]string, err error) {
	blockedByMap = make(map[string][]string)
	blocksMap = make(map[string][]string)

	if len(issueIDs) == 0 {
		return
	}

	placeholders := make([]string, len(issueIDs))
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	in := strings.Join(placeholders, ",")

	// Issues that block us (where we are the issue_id)
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.issue_id, d.depends_on_id FROM dependencies d
		 JOIN issues i ON i.id = d.depends_on_id
		 WHERE d.issue_id IN (`+in+`)
		   AND d.type = 'blocks'
		   AND i.status IN ('open', 'in_progress', 'blocked', 'deferred', 'hooked')`,
		args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var issueID, depID string
		if err := rows.Scan(&issueID, &depID); err != nil {
			return nil, nil, err
		}
		blockedByMap[issueID] = append(blockedByMap[issueID], depID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Issues we block (where we are the depends_on_id)
	rows2, err := s.db.QueryContext(ctx,
		`SELECT d.depends_on_id, d.issue_id FROM dependencies d
		 WHERE d.depends_on_id IN (`+in+`)
		   AND d.type = 'blocks'`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var depID, issueID string
		if err := rows2.Scan(&depID, &issueID); err != nil {
			return nil, nil, err
		}
		blocksMap[depID] = append(blocksMap[depID], issueID)
	}
	return blockedByMap, blocksMap, rows2.Err()
}

// scanIssues scans multiple issues from rows.
func scanIssues(rows *sql.Rows) ([]*types.Issue, error) {
	var issues []*types.Issue
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}
