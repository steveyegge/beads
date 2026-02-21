package ephemeral

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// AddIssueComment adds a comment to an issue.
func (s *Store) AddIssueComment(ctx context.Context, issueID, author, text string) (*types.Comment, error) {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO comments (issue_id, author, text) VALUES (?, ?, ?)`,
		issueID, author, text)
	if err != nil {
		return nil, fmt.Errorf("add comment to ephemeral issue %s: %w", issueID, err)
	}
	id, _ := result.LastInsertId()
	return &types.Comment{
		ID:      id,
		IssueID: issueID,
		Author:  author,
		Text:    text,
	}, nil
}

// GetIssueComments returns all comments for an issue.
func (s *Store) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, issue_id, author, text, created_at FROM comments WHERE issue_id = ? ORDER BY created_at`,
		issueID)
	if err != nil {
		return nil, fmt.Errorf("get comments for ephemeral issue %s: %w", issueID, err)
	}
	defer rows.Close()

	var comments []*types.Comment
	for rows.Next() {
		var c types.Comment
		var createdAt sql.NullString
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &createdAt); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			c.CreatedAt = parseTime(createdAt.String)
		}
		comments = append(comments, &c)
	}
	return comments, rows.Err()
}

// GetEvents returns events for an issue.
func (s *Store) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	query := `SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM events WHERE issue_id = ? ORDER BY created_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit) // nolint:gosec // G202: limit is an int, not user string
	}

	rows, err := s.db.QueryContext(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("get events for ephemeral issue %s: %w", issueID, err)
	}
	defer rows.Close()

	var events []*types.Event
	for rows.Next() {
		var e types.Event
		var oldVal, newVal, comment, createdAt sql.NullString
		if err := rows.Scan(&e.ID, &e.IssueID, &e.EventType, &e.Actor, &oldVal, &newVal, &comment, &createdAt); err != nil {
			return nil, err
		}
		if oldVal.Valid {
			e.OldValue = &oldVal.String
		}
		if newVal.Valid {
			e.NewValue = &newVal.String
		}
		if comment.Valid {
			e.Comment = &comment.String
		}
		if createdAt.Valid {
			e.CreatedAt = parseTime(createdAt.String)
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}
