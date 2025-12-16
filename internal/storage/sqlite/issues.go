package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// insertIssue inserts a single issue into the database
func insertIssue(ctx context.Context, conn *sql.Conn, issue *types.Issue) error {
	sourceRepo := issue.SourceRepo
	if sourceRepo == "" {
		sourceRepo = "." // Default to primary repo
	}

	// Format relates_to as JSON for storage
	relatesTo := formatJSONStringArray(issue.RelatesTo)
	ephemeral := 0
	if issue.Ephemeral {
		ephemeral = 1
	}

	_, err := conn.ExecContext(ctx, `
		INSERT INTO issues (
			id, content_hash, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, updated_at, closed_at, external_ref, source_repo, close_reason,
			deleted_at, deleted_by, delete_reason, original_type,
			sender, ephemeral, replies_to, relates_to, duplicate_of, superseded_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		issue.ID, issue.ContentHash, issue.Title, issue.Description, issue.Design,
		issue.AcceptanceCriteria, issue.Notes, issue.Status,
		issue.Priority, issue.IssueType, issue.Assignee,
		issue.EstimatedMinutes, issue.CreatedAt, issue.UpdatedAt,
		issue.ClosedAt, issue.ExternalRef, sourceRepo, issue.CloseReason,
		issue.DeletedAt, issue.DeletedBy, issue.DeleteReason, issue.OriginalType,
		issue.Sender, ephemeral, issue.RepliesTo, relatesTo, issue.DuplicateOf, issue.SupersededBy,
	)
	if err != nil {
		return fmt.Errorf("failed to insert issue: %w", err)
	}
	return nil
}

// insertIssues bulk inserts multiple issues using a prepared statement
func insertIssues(ctx context.Context, conn *sql.Conn, issues []*types.Issue) error {
	stmt, err := conn.PrepareContext(ctx, `
		INSERT INTO issues (
			id, content_hash, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, updated_at, closed_at, external_ref, source_repo, close_reason,
			deleted_at, deleted_by, delete_reason, original_type,
			sender, ephemeral, replies_to, relates_to, duplicate_of, superseded_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, issue := range issues {
		sourceRepo := issue.SourceRepo
		if sourceRepo == "" {
			sourceRepo = "." // Default to primary repo
		}

		// Format relates_to as JSON for storage
		relatesTo := formatJSONStringArray(issue.RelatesTo)
		ephemeral := 0
		if issue.Ephemeral {
			ephemeral = 1
		}

		_, err = stmt.ExecContext(ctx,
			issue.ID, issue.ContentHash, issue.Title, issue.Description, issue.Design,
			issue.AcceptanceCriteria, issue.Notes, issue.Status,
			issue.Priority, issue.IssueType, issue.Assignee,
			issue.EstimatedMinutes, issue.CreatedAt, issue.UpdatedAt,
			issue.ClosedAt, issue.ExternalRef, sourceRepo, issue.CloseReason,
			issue.DeletedAt, issue.DeletedBy, issue.DeleteReason, issue.OriginalType,
			issue.Sender, ephemeral, issue.RepliesTo, relatesTo, issue.DuplicateOf, issue.SupersededBy,
		)
		if err != nil {
			return fmt.Errorf("failed to insert issue %s: %w", issue.ID, err)
		}
	}
	return nil
}
