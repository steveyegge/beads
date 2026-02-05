package dolt

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// UpdateIssueID updates an issue ID and all its references
func (s *DoltStore) UpdateIssueID(ctx context.Context, oldID, newID string, issue *types.Issue, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Disable foreign key checks to allow renaming the primary key
	// without violating FK constraints on child tables (bd-wj80.1)
	_, err = tx.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 0`)
	if err != nil {
		return fmt.Errorf("failed to disable foreign key checks: %w", err)
	}

	// Update the issue itself
	result, err := tx.ExecContext(ctx, `
		UPDATE issues
		SET id = ?, title = ?, description = ?, design = ?, acceptance_criteria = ?, notes = ?, updated_at = ?
		WHERE id = ?
	`, newID, issue.Title, issue.Description, issue.Design, issue.AcceptanceCriteria, issue.Notes, time.Now().UTC(), oldID)
	if err != nil {
		return fmt.Errorf("failed to update issue ID: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("issue not found: %s", oldID)
	}

	// Update references in dependencies
	_, err = tx.ExecContext(ctx, `UPDATE dependencies SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update issue_id in dependencies: %w", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE dependencies SET depends_on_id = ? WHERE depends_on_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update depends_on_id in dependencies: %w", err)
	}

	// Update references in events
	_, err = tx.ExecContext(ctx, `UPDATE events SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update events: %w", err)
	}

	// Update references in labels
	_, err = tx.ExecContext(ctx, `UPDATE labels SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update labels: %w", err)
	}

	// Update references in comments
	_, err = tx.ExecContext(ctx, `UPDATE comments SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update comments: %w", err)
	}

	// Update dirty_issues
	_, err = tx.ExecContext(ctx, `UPDATE dirty_issues SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update dirty_issues: %w", err)
	}

	// Update export_hashes
	_, err = tx.ExecContext(ctx, `UPDATE export_hashes SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update export_hashes: %w", err)
	}

	// Update child_counters
	_, err = tx.ExecContext(ctx, `UPDATE child_counters SET parent_id = ? WHERE parent_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update child_counters: %w", err)
	}

	// Update issue_snapshots
	_, err = tx.ExecContext(ctx, `UPDATE issue_snapshots SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update issue_snapshots: %w", err)
	}

	// Update compaction_snapshots
	_, err = tx.ExecContext(ctx, `UPDATE compaction_snapshots SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update compaction_snapshots: %w", err)
	}

	// Update decision_points
	_, err = tx.ExecContext(ctx, `UPDATE decision_points SET issue_id = ? WHERE issue_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update decision_points issue_id: %w", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE decision_points SET prior_id = ? WHERE prior_id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("failed to update decision_points prior_id: %w", err)
	}

	// Mark new ID as dirty for incremental export
	_, err = tx.ExecContext(ctx, `
		INSERT INTO dirty_issues (issue_id, marked_at)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE marked_at = VALUES(marked_at)
	`, newID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	// Record rename event
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, old_value, new_value)
		VALUES (?, 'renamed', ?, ?, ?)
	`, newID, actor, oldID, newID)
	if err != nil {
		return fmt.Errorf("failed to record rename event: %w", err)
	}

	// Re-enable foreign key checks before commit
	_, err = tx.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 1`)
	if err != nil {
		return fmt.Errorf("failed to re-enable foreign key checks: %w", err)
	}

	return tx.Commit()
}

// RenameDependencyPrefix updates the prefix in all dependency records
func (s *DoltStore) RenameDependencyPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Update issue_id column
	_, err = tx.ExecContext(ctx, `
		UPDATE dependencies
		SET issue_id = CONCAT(?, SUBSTRING(issue_id, LENGTH(?) + 1))
		WHERE issue_id LIKE CONCAT(?, '%')
	`, newPrefix, oldPrefix, oldPrefix)
	if err != nil {
		return fmt.Errorf("failed to update issue_id in dependencies: %w", err)
	}

	// Update depends_on_id column
	_, err = tx.ExecContext(ctx, `
		UPDATE dependencies
		SET depends_on_id = CONCAT(?, SUBSTRING(depends_on_id, LENGTH(?) + 1))
		WHERE depends_on_id LIKE CONCAT(?, '%')
	`, newPrefix, oldPrefix, oldPrefix)
	if err != nil {
		return fmt.Errorf("failed to update depends_on_id in dependencies: %w", err)
	}

	return tx.Commit()
}

// RenameCounterPrefix is a no-op with hash-based IDs
func (s *DoltStore) RenameCounterPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	// Hash-based IDs don't use counters
	return nil
}
