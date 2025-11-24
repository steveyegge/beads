// Package sqlite implements the storage interface using SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// Verify sqliteTxStorage implements storage.Transaction at compile time
var _ storage.Transaction = (*sqliteTxStorage)(nil)

// sqliteTxStorage implements the storage.Transaction interface for SQLite.
// It wraps a dedicated database connection with an active transaction.
type sqliteTxStorage struct {
	conn   *sql.Conn      // Dedicated connection for the transaction
	parent *SQLiteStorage // Parent storage for accessing shared state
}

// RunInTransaction executes a function within a database transaction.
//
// The transaction uses BEGIN IMMEDIATE to acquire a write lock early,
// preventing deadlocks when multiple goroutines compete for the same lock.
//
// Transaction lifecycle:
//  1. Acquire dedicated connection from pool
//  2. Begin IMMEDIATE transaction with retry on SQLITE_BUSY
//  3. Execute user function with Transaction interface
//  4. On success: COMMIT
//  5. On error or panic: ROLLBACK
//
// Panic safety: If the callback panics, the transaction is rolled back
// and the panic is re-raised to the caller.
func (s *SQLiteStorage) RunInTransaction(ctx context.Context, fn func(tx storage.Transaction) error) error {
	// Acquire a dedicated connection for the transaction.
	// This ensures all operations in the transaction use the same connection.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for transaction: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Start IMMEDIATE transaction to acquire write lock early.
	// Use retry logic with exponential backoff to handle SQLITE_BUSY (bd-ola6)
	if err := beginImmediateWithRetry(ctx, conn, 5, 10*time.Millisecond); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Track commit state for cleanup
	committed := false
	defer func() {
		if !committed {
			// Use background context to ensure rollback completes even if ctx is cancelled
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	// Handle panics: rollback and re-raise
	defer func() {
		if r := recover(); r != nil {
			// Rollback will happen via the committed=false check above
			panic(r) // Re-raise the panic
		}
	}()

	// Create transaction wrapper
	txStorage := &sqliteTxStorage{
		conn:   conn,
		parent: s,
	}

	// Execute user function
	if err := fn(txStorage); err != nil {
		return err // Rollback happens in defer
	}

	// Commit the transaction
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return nil
}

// CreateIssue creates a new issue within the transaction.
func (t *sqliteTxStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Validate issue before creating
	if err := issue.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Set timestamps
	now := time.Now()
	issue.CreatedAt = now
	issue.UpdatedAt = now

	// Compute content hash (bd-95)
	if issue.ContentHash == "" {
		issue.ContentHash = issue.ComputeContentHash()
	}

	// Get prefix from config (needed for both ID generation and validation)
	var prefix string
	err := t.conn.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, "issue_prefix").Scan(&prefix)
	if err == sql.ErrNoRows || prefix == "" {
		// CRITICAL: Reject operation if issue_prefix config is missing (bd-166)
		return fmt.Errorf("database not initialized: issue_prefix config is missing (run 'bd init --prefix <prefix>' first)")
	} else if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Generate or validate ID
	if issue.ID == "" {
		// Generate hash-based ID with adaptive length based on database size (bd-ea2a13)
		generatedID, err := GenerateIssueID(ctx, t.conn, prefix, issue, actor)
		if err != nil {
			return wrapDBError("generate issue ID", err)
		}
		issue.ID = generatedID
	} else {
		// Validate that explicitly provided ID matches the configured prefix (bd-177)
		if err := ValidateIssueIDPrefix(issue.ID, prefix); err != nil {
			return wrapDBError("validate issue ID prefix", err)
		}

		// For hierarchical IDs (bd-a3f8e9.1), ensure parent exists
		if strings.Contains(issue.ID, ".") {
			// Try to resurrect entire parent chain if any parents are missing
			resurrected, err := t.parent.tryResurrectParentChainWithConn(ctx, t.conn, issue.ID)
			if err != nil {
				return fmt.Errorf("failed to resurrect parent chain for %s: %w", issue.ID, err)
			}
			if !resurrected {
				// Parent(s) not found in JSONL history - cannot proceed
				lastDot := strings.LastIndex(issue.ID, ".")
				parentID := issue.ID[:lastDot]
				return fmt.Errorf("parent issue %s does not exist and could not be resurrected from JSONL history", parentID)
			}
		}
	}

	// Insert issue
	if err := insertIssue(ctx, t.conn, issue); err != nil {
		return wrapDBError("insert issue", err)
	}

	// Record creation event
	if err := recordCreatedEvent(ctx, t.conn, issue, actor); err != nil {
		return wrapDBError("record creation event", err)
	}

	// Mark issue as dirty for incremental export
	if err := markDirty(ctx, t.conn, issue.ID); err != nil {
		return wrapDBError("mark issue dirty", err)
	}

	return nil
}

// CreateIssues creates multiple issues within the transaction.
func (t *sqliteTxStorage) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	if len(issues) == 0 {
		return nil
	}

	// Validate and prepare all issues first
	now := time.Now()
	for _, issue := range issues {
		if err := issue.Validate(); err != nil {
			return fmt.Errorf("validation failed for issue: %w", err)
		}
		issue.CreatedAt = now
		issue.UpdatedAt = now
		if issue.ContentHash == "" {
			issue.ContentHash = issue.ComputeContentHash()
		}
	}

	// Get prefix from config
	var prefix string
	err := t.conn.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, "issue_prefix").Scan(&prefix)
	if err == sql.ErrNoRows || prefix == "" {
		return fmt.Errorf("database not initialized: issue_prefix config is missing")
	} else if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Generate IDs for issues that don't have them
	for _, issue := range issues {
		if issue.ID == "" {
			generatedID, err := GenerateIssueID(ctx, t.conn, prefix, issue, actor)
			if err != nil {
				return wrapDBError("generate issue ID", err)
			}
			issue.ID = generatedID
		} else {
			if err := ValidateIssueIDPrefix(issue.ID, prefix); err != nil {
				return wrapDBError("validate issue ID prefix", err)
			}
		}
	}

	// Check for duplicate IDs within the batch
	seenIDs := make(map[string]bool)
	for _, issue := range issues {
		if seenIDs[issue.ID] {
			return fmt.Errorf("duplicate issue ID within batch: %s", issue.ID)
		}
		seenIDs[issue.ID] = true
	}

	// Insert all issues
	if err := insertIssues(ctx, t.conn, issues); err != nil {
		return wrapDBError("insert issues", err)
	}

	// Record creation events
	if err := recordCreatedEvents(ctx, t.conn, issues, actor); err != nil {
		return wrapDBError("record creation events", err)
	}

	// Mark all issues as dirty
	if err := markDirtyBatch(ctx, t.conn, issues); err != nil {
		return wrapDBError("mark issues dirty", err)
	}

	return nil
}

// GetIssue retrieves an issue within the transaction.
// This enables read-your-writes semantics within the transaction.
func (t *sqliteTxStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	var issue types.Issue
	var closedAt sql.NullTime
	var estimatedMinutes sql.NullInt64
	var assignee sql.NullString
	var externalRef sql.NullString
	var compactedAt sql.NullTime
	var originalSize sql.NullInt64
	var sourceRepo sql.NullString
	var contentHash sql.NullString
	var compactedAtCommit sql.NullString

	err := t.conn.QueryRowContext(ctx, `
		SELECT id, content_hash, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, updated_at, closed_at, external_ref,
		       compaction_level, compacted_at, compacted_at_commit, original_size, source_repo
		FROM issues
		WHERE id = ?
	`, id).Scan(
		&issue.ID, &contentHash, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&issue.CreatedAt, &issue.UpdatedAt, &closedAt, &externalRef,
		&issue.CompactionLevel, &compactedAt, &compactedAtCommit, &originalSize, &sourceRepo,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	if contentHash.Valid {
		issue.ContentHash = contentHash.String
	}
	if closedAt.Valid {
		issue.ClosedAt = &closedAt.Time
	}
	if estimatedMinutes.Valid {
		mins := int(estimatedMinutes.Int64)
		issue.EstimatedMinutes = &mins
	}
	if assignee.Valid {
		issue.Assignee = assignee.String
	}
	if externalRef.Valid {
		issue.ExternalRef = &externalRef.String
	}
	if compactedAt.Valid {
		issue.CompactedAt = &compactedAt.Time
	}
	if compactedAtCommit.Valid {
		issue.CompactedAtCommit = &compactedAtCommit.String
	}
	if originalSize.Valid {
		issue.OriginalSize = int(originalSize.Int64)
	}
	if sourceRepo.Valid {
		issue.SourceRepo = sourceRepo.String
	}

	// Fetch labels for this issue using the transaction connection
	labels, err := t.getLabels(ctx, issue.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	issue.Labels = labels

	return &issue, nil
}

// getLabels retrieves labels using the transaction's connection
func (t *sqliteTxStorage) getLabels(ctx context.Context, issueID string) ([]string, error) {
	rows, err := t.conn.QueryContext(ctx, `
		SELECT label FROM labels WHERE issue_id = ? ORDER BY label
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var labels []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}

	return labels, nil
}

// UpdateIssue updates an issue within the transaction.
func (t *sqliteTxStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Get old issue for event
	oldIssue, err := t.GetIssue(ctx, id)
	if err != nil {
		return wrapDBError("get issue for update", err)
	}
	if oldIssue == nil {
		return fmt.Errorf("issue %s not found", id)
	}

	// Build update query with validated field names
	setClauses := []string{"updated_at = ?"}
	args := []interface{}{time.Now()}

	for key, value := range updates {
		// Prevent SQL injection by validating field names
		if !allowedUpdateFields[key] {
			return fmt.Errorf("invalid field for update: %s", key)
		}

		// Validate field values
		if err := validateFieldUpdate(key, value); err != nil {
			return wrapDBError("validate field update", err)
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = ?", key))
		args = append(args, value)
	}

	// Auto-manage closed_at when status changes
	setClauses, args = manageClosedAt(oldIssue, updates, setClauses, args)

	// Recompute content_hash if any content fields changed (bd-95)
	contentChanged := false
	contentFields := []string{"title", "description", "design", "acceptance_criteria", "notes", "status", "priority", "issue_type", "assignee", "external_ref"}
	for _, field := range contentFields {
		if _, exists := updates[field]; exists {
			contentChanged = true
			break
		}
	}
	if contentChanged {
		updatedIssue := *oldIssue
		applyUpdatesToIssue(&updatedIssue, updates)
		newHash := updatedIssue.ComputeContentHash()
		setClauses = append(setClauses, "content_hash = ?")
		args = append(args, newHash)
	}

	args = append(args, id)

	// Update issue
	query := fmt.Sprintf("UPDATE issues SET %s WHERE id = ?", strings.Join(setClauses, ", ")) // #nosec G201 - safe SQL with controlled column names
	_, err = t.conn.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Record event
	oldData, err := json.Marshal(oldIssue)
	if err != nil {
		oldData = []byte(fmt.Sprintf(`{"id":"%s"}`, id))
	}
	newData, err := json.Marshal(updates)
	if err != nil {
		newData = []byte(`{}`)
	}

	eventType := determineEventType(oldIssue, updates)

	_, err = t.conn.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)
	`, id, eventType, actor, string(oldData), string(newData))
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	// Mark issue as dirty
	if err := markDirty(ctx, t.conn, id); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	return nil
}

// applyUpdatesToIssue applies update map to issue for content hash recomputation
func applyUpdatesToIssue(issue *types.Issue, updates map[string]interface{}) {
	for key, value := range updates {
		switch key {
		case "title":
			issue.Title = value.(string)
		case "description":
			issue.Description = value.(string)
		case "design":
			issue.Design = value.(string)
		case "acceptance_criteria":
			issue.AcceptanceCriteria = value.(string)
		case "notes":
			issue.Notes = value.(string)
		case "status":
			if s, ok := value.(types.Status); ok {
				issue.Status = s
			} else {
				issue.Status = types.Status(value.(string))
			}
		case "priority":
			issue.Priority = value.(int)
		case "issue_type":
			if t, ok := value.(types.IssueType); ok {
				issue.IssueType = t
			} else {
				issue.IssueType = types.IssueType(value.(string))
			}
		case "assignee":
			if value == nil {
				issue.Assignee = ""
			} else {
				issue.Assignee = value.(string)
			}
		case "external_ref":
			if value == nil {
				issue.ExternalRef = nil
			} else {
				switch v := value.(type) {
				case string:
					issue.ExternalRef = &v
				case *string:
					issue.ExternalRef = v
				}
			}
		}
	}
}

// CloseIssue closes an issue within the transaction.
func (t *sqliteTxStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	now := time.Now()

	result, err := t.conn.ExecContext(ctx, `
		UPDATE issues SET status = ?, closed_at = ?, updated_at = ?
		WHERE id = ?
	`, types.StatusClosed, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to close issue: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("issue not found: %s", id)
	}

	_, err = t.conn.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, id, types.EventClosed, actor, reason)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	// Mark issue as dirty
	if err := markDirty(ctx, t.conn, id); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	return nil
}

// DeleteIssue deletes an issue within the transaction.
func (t *sqliteTxStorage) DeleteIssue(ctx context.Context, id string) error {
	// Delete dependencies (both directions)
	_, err := t.conn.ExecContext(ctx, `DELETE FROM dependencies WHERE issue_id = ? OR depends_on_id = ?`, id, id)
	if err != nil {
		return fmt.Errorf("failed to delete dependencies: %w", err)
	}

	// Delete events
	_, err = t.conn.ExecContext(ctx, `DELETE FROM events WHERE issue_id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete events: %w", err)
	}

	// Delete from dirty_issues
	_, err = t.conn.ExecContext(ctx, `DELETE FROM dirty_issues WHERE issue_id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete dirty marker: %w", err)
	}

	// Delete the issue itself
	result, err := t.conn.ExecContext(ctx, `DELETE FROM issues WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete issue: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("issue not found: %s", id)
	}

	return nil
}

// AddDependency adds a dependency between issues within the transaction.
func (t *sqliteTxStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	// Validate dependency type
	if !dep.Type.IsValid() {
		return fmt.Errorf("invalid dependency type: %s (must be blocks, related, parent-child, or discovered-from)", dep.Type)
	}

	// Validate that both issues exist
	issueExists, err := t.GetIssue(ctx, dep.IssueID)
	if err != nil {
		return fmt.Errorf("failed to check issue %s: %w", dep.IssueID, err)
	}
	if issueExists == nil {
		return fmt.Errorf("issue %s not found", dep.IssueID)
	}

	dependsOnExists, err := t.GetIssue(ctx, dep.DependsOnID)
	if err != nil {
		return fmt.Errorf("failed to check dependency %s: %w", dep.DependsOnID, err)
	}
	if dependsOnExists == nil {
		return fmt.Errorf("dependency target %s not found", dep.DependsOnID)
	}

	// Prevent self-dependency
	if dep.IssueID == dep.DependsOnID {
		return fmt.Errorf("issue cannot depend on itself")
	}

	// Validate parent-child dependency direction
	if dep.Type == types.DepParentChild {
		if issueExists.IssueType == types.TypeEpic && dependsOnExists.IssueType != types.TypeEpic {
			return fmt.Errorf("invalid parent-child dependency: parent (%s) cannot depend on child (%s). Use: bd dep add %s %s --type parent-child",
				dep.IssueID, dep.DependsOnID, dep.DependsOnID, dep.IssueID)
		}
	}

	if dep.CreatedAt.IsZero() {
		dep.CreatedAt = time.Now()
	}
	if dep.CreatedBy == "" {
		dep.CreatedBy = actor
	}

	// Cycle detection
	var cycleExists bool
	err = t.conn.QueryRowContext(ctx, `
		WITH RECURSIVE paths AS (
			SELECT
				issue_id,
				depends_on_id,
				1 as depth
			FROM dependencies
			WHERE issue_id = ?

			UNION ALL

			SELECT
				d.issue_id,
				d.depends_on_id,
				p.depth + 1
			FROM dependencies d
			JOIN paths p ON d.issue_id = p.depends_on_id
			WHERE p.depth < 100
		)
		SELECT EXISTS(
			SELECT 1 FROM paths
			WHERE depends_on_id = ?
		)
	`, dep.DependsOnID, dep.IssueID).Scan(&cycleExists)

	if err != nil {
		return fmt.Errorf("failed to check for cycles: %w", err)
	}

	if cycleExists {
		return fmt.Errorf("cannot add dependency: would create a cycle (%s → %s → ... → %s)",
			dep.IssueID, dep.DependsOnID, dep.IssueID)
	}

	// Insert dependency
	_, err = t.conn.ExecContext(ctx, `
		INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by)
		VALUES (?, ?, ?, ?, ?)
	`, dep.IssueID, dep.DependsOnID, dep.Type, dep.CreatedAt, dep.CreatedBy)
	if err != nil {
		return fmt.Errorf("failed to add dependency: %w", err)
	}

	// Record event
	_, err = t.conn.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, dep.IssueID, types.EventDependencyAdded, actor,
		fmt.Sprintf("Added dependency: %s %s %s", dep.IssueID, dep.Type, dep.DependsOnID))
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	// Mark both issues as dirty
	if err := markDirty(ctx, t.conn, dep.IssueID); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}
	if err := markDirty(ctx, t.conn, dep.DependsOnID); err != nil {
		return fmt.Errorf("failed to mark depends-on issue dirty: %w", err)
	}

	return nil
}

// RemoveDependency removes a dependency within the transaction.
func (t *sqliteTxStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	result, err := t.conn.ExecContext(ctx, `
		DELETE FROM dependencies WHERE issue_id = ? AND depends_on_id = ?
	`, issueID, dependsOnID)
	if err != nil {
		return fmt.Errorf("failed to remove dependency: %w", err)
	}

	// Check if dependency existed
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("dependency from %s to %s does not exist", issueID, dependsOnID)
	}

	_, err = t.conn.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, issueID, types.EventDependencyRemoved, actor,
		fmt.Sprintf("Removed dependency on %s", dependsOnID))
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	// Mark both issues as dirty
	if err := markDirty(ctx, t.conn, issueID); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}
	if err := markDirty(ctx, t.conn, dependsOnID); err != nil {
		return fmt.Errorf("failed to mark depends-on issue dirty: %w", err)
	}

	return nil
}

// AddLabel adds a label to an issue within the transaction.
func (t *sqliteTxStorage) AddLabel(ctx context.Context, issueID, label, actor string) error {
	result, err := t.conn.ExecContext(ctx, `
		INSERT OR IGNORE INTO labels (issue_id, label) VALUES (?, ?)
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to add label: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		// Label already existed, no change made
		return nil
	}

	// Record event
	_, err = t.conn.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, issueID, types.EventLabelAdded, actor, fmt.Sprintf("Added label: %s", label))
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	// Mark issue as dirty
	if err := markDirty(ctx, t.conn, issueID); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	return nil
}

// RemoveLabel removes a label from an issue within the transaction.
func (t *sqliteTxStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	result, err := t.conn.ExecContext(ctx, `
		DELETE FROM labels WHERE issue_id = ? AND label = ?
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		// Label didn't exist, no change made
		return nil
	}

	// Record event
	_, err = t.conn.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, issueID, types.EventLabelRemoved, actor, fmt.Sprintf("Removed label: %s", label))
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	// Mark issue as dirty
	if err := markDirty(ctx, t.conn, issueID); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	return nil
}
