package dolt

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// AddLabel adds a label to an issue.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.AddLabel(ctx, issueID, label, actor)
	})
}

// RemoveLabel removes a label from an issue.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.RemoveLabel(ctx, issueID, label, actor)
	})
}

// GetLabels retrieves all labels for an issue
func (s *DoltStore) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT label FROM labels WHERE issue_id = ? ORDER BY label
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	defer rows.Close()

	var labels []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, fmt.Errorf("failed to scan label: %w", err)
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

// getLabelsForIssuesBatchSize is the maximum number of issue IDs per IN clause.
// Large IN clauses (e.g. 29k params) create queries that Dolt cannot execute efficiently.
const getLabelsForIssuesBatchSize = 500

// GetLabelsForIssues retrieves labels for multiple issues, batching the query
// into chunks to avoid oversized IN clauses that crash Dolt.
func (s *DoltStore) GetLabelsForIssues(ctx context.Context, issueIDs []string) (map[string][]string, error) {
	if len(issueIDs) == 0 {
		return make(map[string][]string), nil
	}

	result := make(map[string][]string)
	for i := 0; i < len(issueIDs); i += getLabelsForIssuesBatchSize {
		end := i + getLabelsForIssuesBatchSize
		if end > len(issueIDs) {
			end = len(issueIDs)
		}
		batch := issueIDs[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch))
		for j, id := range batch {
			placeholders[j] = "?"
			args[j] = id
		}

		// nolint:gosec // G201: placeholders contains only ? markers, actual values passed via args
		query := fmt.Sprintf(`
			SELECT issue_id, label FROM labels
			WHERE issue_id IN (%s)
			ORDER BY issue_id, label
		`, strings.Join(placeholders, ","))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to get labels for issues: %w", err)
		}

		for rows.Next() {
			var issueID, label string
			if err := rows.Scan(&issueID, &label); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan label: %w", err)
			}
			result[issueID] = append(result[issueID], label)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}

// GetAllLabels retrieves all labels for all issues.
// Used by the label cache to do a single full-table scan at startup
// instead of many IN-clause queries.
func (s *DoltStore) GetAllLabels(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT issue_id, label FROM labels ORDER BY issue_id, label
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get all labels: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var issueID, label string
		if err := rows.Scan(&issueID, &label); err != nil {
			return nil, fmt.Errorf("failed to scan label: %w", err)
		}
		result[issueID] = append(result[issueID], label)
	}
	return result, rows.Err()
}

// GetIssuesByLabel retrieves all issues with a specific label
func (s *DoltStore) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id FROM issues i
		JOIN labels l ON i.id = l.issue_id
		WHERE l.label = ?
		ORDER BY i.priority ASC, i.created_at DESC
	`, label)
	if err != nil {
		return nil, fmt.Errorf("failed to get issues by label: %w", err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan issue id: %w", err)
		}
		issue, err := s.GetIssue(ctx, id)
		if err != nil {
			return nil, err
		}
		if issue != nil {
			issues = append(issues, issue)
		}
	}
	return issues, rows.Err()
}
