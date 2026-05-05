package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// AddIssueComment writes a comment row and returns the persisted record. Used
// by `bd comment add`.
func (s *PostgresStore) AddIssueComment(ctx context.Context, issueID, author, text string) (*types.Comment, error) {
	c := &types.Comment{
		IssueID:   issueID,
		Author:    author,
		Text:      text,
		CreatedAt: time.Now().UTC(),
	}
	q := `INSERT INTO comments (issue_id, author, text, created_at) VALUES ($1, $2, $3, $4) RETURNING id`
	if err := s.pool.QueryRow(ctx, q, issueID, author, text, c.CreatedAt).Scan(&c.ID); err != nil {
		return nil, wrapErr("add issue comment", err)
	}
	return c, nil
}

// AddComment is the AnnotationStore variant — same behavior, no return value.
func (s *PostgresStore) AddComment(ctx context.Context, issueID, actor, comment string) error {
	_, err := s.AddIssueComment(ctx, issueID, actor, comment)
	return err
}

// ImportIssueComment is used by import paths that carry a known created_at.
func (s *PostgresStore) ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error) {
	c := &types.Comment{
		IssueID:   issueID,
		Author:    author,
		Text:      text,
		CreatedAt: createdAt,
	}
	q := `INSERT INTO comments (issue_id, author, text, created_at) VALUES ($1, $2, $3, $4) RETURNING id`
	if err := s.pool.QueryRow(ctx, q, issueID, author, text, createdAt).Scan(&c.ID); err != nil {
		return nil, wrapErr("import issue comment", err)
	}
	return c, nil
}

// GetIssueComments returns comments for one issue, oldest-first.
func (s *PostgresStore) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	q := `SELECT id::text, issue_id, author, text, created_at FROM comments WHERE issue_id = $1 ORDER BY created_at ASC, id ASC`
	rows, err := s.pool.Query(ctx, q, issueID)
	if err != nil {
		return nil, wrapErr("get issue comments", err)
	}
	defer rows.Close()
	var out []*types.Comment
	for rows.Next() {
		c := &types.Comment{}
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt); err != nil {
			return nil, wrapErr("scan issue comments", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCommentCounts returns a map of issue_id → comment count for the given
// IDs. Missing IDs map to zero (or simply absent in the map).
func (s *PostgresStore) GetCommentCounts(ctx context.Context, issueIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(issueIDs))
	if len(issueIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		args[i] = id
	}
	ph := joinPlaceholders(1, len(issueIDs))
	//nolint:gosec // placeholders bound, identifiers static
	q := fmt.Sprintf(`SELECT issue_id, COUNT(*)::int FROM comments WHERE issue_id IN (%s) GROUP BY issue_id`, ph)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("get comment counts", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, wrapErr("scan comment counts", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// GetCommentsForIssues returns all comments grouped by issue_id.
func (s *PostgresStore) GetCommentsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error) {
	out := make(map[string][]*types.Comment, len(issueIDs))
	if len(issueIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		args[i] = id
	}
	ph := joinPlaceholders(1, len(issueIDs))
	//nolint:gosec // placeholders bound
	q := fmt.Sprintf(`
		SELECT id::text, issue_id, author, text, created_at
		FROM comments
		WHERE issue_id IN (%s)
		ORDER BY issue_id, created_at ASC
	`, ph)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("get comments for issues", err)
	}
	defer rows.Close()
	for rows.Next() {
		c := &types.Comment{}
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt); err != nil {
			return nil, wrapErr("scan comments for issues", err)
		}
		out[c.IssueID] = append(out[c.IssueID], c)
	}
	return out, rows.Err()
}
