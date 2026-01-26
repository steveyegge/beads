package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// GetEpicsEligibleForClosure returns all epics with their completion status
func (s *SQLiteStorage) GetEpicsEligibleForClosure(ctx context.Context) ([]*types.EpicStatus, error) {
	query := `
		WITH epic_children AS (
			SELECT 
				d.depends_on_id AS epic_id,
				i.id AS child_id,
				i.status AS child_status
			FROM dependencies d
			JOIN issues i ON i.id = d.issue_id
			WHERE d.type = 'parent-child'
		),
		epic_stats AS (
			SELECT 
				epic_id,
				COUNT(*) AS total_children,
				SUM(CASE WHEN child_status = 'closed' THEN 1 ELSE 0 END) AS closed_children
			FROM epic_children
			GROUP BY epic_id
		)
		SELECT 
			i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
			i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
			i.created_at, i.updated_at, i.closed_at, i.external_ref,
			COALESCE(es.total_children, 0) AS total_children,
			COALESCE(es.closed_children, 0) AS closed_children
		FROM issues i
		LEFT JOIN epic_stats es ON es.epic_id = i.id
		WHERE i.issue_type = 'epic'
		  AND i.status != 'closed'
		ORDER BY i.priority ASC, i.created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []*types.EpicStatus
	for rows.Next() {
		var epic types.Issue
		var totalChildren, closedChildren int
		var assignee sql.NullString

		err := rows.Scan(
			&epic.ID, &epic.Title, &epic.Description, &epic.Design,
			&epic.AcceptanceCriteria, &epic.Notes, &epic.Status,
			&epic.Priority, &epic.IssueType, &assignee,
			&epic.EstimatedMinutes, &epic.CreatedAt, &epic.UpdatedAt,
			&epic.ClosedAt, &epic.ExternalRef,
			&totalChildren, &closedChildren,
		)
		if err != nil {
			return nil, err
		}

		// Convert sql.NullString to string
		if assignee.Valid {
			epic.Assignee = assignee.String
		}

		eligibleForClose := false
		if totalChildren > 0 && closedChildren == totalChildren {
			eligibleForClose = true
		}

		results = append(results, &types.EpicStatus{
			Epic:             &epic,
			TotalChildren:    totalChildren,
			ClosedChildren:   closedChildren,
			EligibleForClose: eligibleForClose,
		})
	}

	return results, rows.Err()
}

// GetEpicProgress returns progress (total/closed children) for a list of epic IDs
// Returns a map from epic ID to progress. Epics not found or with no children have 0/0.
func (s *SQLiteStorage) GetEpicProgress(ctx context.Context, epicIDs []string) (map[string]*types.EpicProgress, error) {
	if len(epicIDs) == 0 {
		return make(map[string]*types.EpicProgress), nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(epicIDs))
	args := make([]interface{}, len(epicIDs))
	for i, id := range epicIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	//nolint:gosec // SQL uses ? placeholders for values, string concat is for placeholder count only
	query := `
		WITH epic_children AS (
			SELECT
				d.depends_on_id AS epic_id,
				i.status AS child_status
			FROM dependencies d
			JOIN issues i ON i.id = d.issue_id
			WHERE d.type = 'parent-child'
			  AND d.depends_on_id IN (` + strings.Join(placeholders, ",") + `)
		)
		SELECT
			epic_id,
			COUNT(*) AS total_children,
			SUM(CASE WHEN child_status = 'closed' THEN 1 ELSE 0 END) AS closed_children
		FROM epic_children
		GROUP BY epic_id
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]*types.EpicProgress)
	for rows.Next() {
		var epicID string
		var total, closed int
		if err := rows.Scan(&epicID, &total, &closed); err != nil {
			return nil, err
		}
		result[epicID] = &types.EpicProgress{
			TotalChildren:  total,
			ClosedChildren: closed,
		}
	}

	return result, rows.Err()
}
