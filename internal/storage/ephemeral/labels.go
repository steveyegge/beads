package ephemeral

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// AddLabel adds a label to an issue.
func (s *Store) AddLabel(ctx context.Context, issueID, label, actor string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO labels (issue_id, label) VALUES (?, ?)`,
		issueID, label)
	if err != nil {
		return fmt.Errorf("add label to ephemeral issue %s: %w", issueID, err)
	}
	return nil
}

// RemoveLabel removes a label from an issue.
func (s *Store) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM labels WHERE issue_id = ? AND label = ?`,
		issueID, label)
	if err != nil {
		return fmt.Errorf("remove label from ephemeral issue %s: %w", issueID, err)
	}
	return nil
}

// GetLabels returns all labels for an issue.
func (s *Store) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	return s.getLabels(ctx, s.db, issueID)
}

// GetIssuesByLabel returns all issues with a given label.
func (s *Store) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+issueSelectColumns+` FROM issues
		 WHERE id IN (SELECT issue_id FROM labels WHERE label = ?)`, label)
	if err != nil {
		return nil, fmt.Errorf("get ephemeral issues by label: %w", err)
	}
	defer rows.Close()
	return scanIssues(rows)
}
