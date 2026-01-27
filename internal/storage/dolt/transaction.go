package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

const (
	// maxTransactionRetries is the maximum number of retry attempts for
	// transaction commit failures due to serialization conflicts
	maxTransactionRetries = 5
	// initialRetryDelay is the initial delay before retrying a failed transaction
	initialRetryDelay = 50 * time.Millisecond
)

// doltTransaction implements storage.Transaction for Dolt
type doltTransaction struct {
	tx    *sql.Tx
	store *DoltStore
}

// CreateIssueImport is the import-friendly issue creation hook.
// Dolt does not enforce prefix validation at the storage layer, so this delegates to CreateIssue.
func (t *doltTransaction) CreateIssueImport(ctx context.Context, issue *types.Issue, actor string, skipPrefixValidation bool) error {
	return t.CreateIssue(ctx, issue, actor)
}

// RunInTransaction executes a function within a database transaction.
// If the transaction fails due to a serialization conflict (Error 1213, 1105),
// it will be automatically retried with exponential backoff.
func (s *DoltStore) RunInTransaction(ctx context.Context, fn func(tx storage.Transaction) error) error {
	var lastErr error
	retryDelay := initialRetryDelay

	for attempt := 0; attempt <= maxTransactionRetries; attempt++ {
		if attempt > 0 {
			// Log retry for debugging
			fmt.Fprintf(os.Stderr, "Dolt transaction retry (attempt %d/%d) after serialization conflict, waiting %v...\n",
				attempt, maxTransactionRetries, retryDelay)
			time.Sleep(retryDelay)
			// Exponential backoff with jitter
			retryDelay = retryDelay * 2
			if retryDelay > 2*time.Second {
				retryDelay = 2 * time.Second
			}
		}

		lastErr = s.runTransactionOnce(ctx, fn)
		if lastErr == nil {
			return nil
		}

		// Check if this is a retryable error
		if !isSerializationError(lastErr) {
			return lastErr
		}
		// Continue to retry
	}

	return fmt.Errorf("transaction failed after %d retries: %w", maxTransactionRetries, lastErr)
}

// runTransactionOnce executes a single transaction attempt
func (s *DoltStore) runTransactionOnce(ctx context.Context, fn func(tx storage.Transaction) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	tx := &doltTransaction{tx: sqlTx, store: s}

	defer func() {
		if r := recover(); r != nil {
			_ = sqlTx.Rollback()
			panic(r)
		}
	}()

	if err := fn(tx); err != nil {
		_ = sqlTx.Rollback()
		return err
	}

	return sqlTx.Commit()
}

// CreateIssue creates an issue within the transaction
func (t *doltTransaction) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	now := time.Now().UTC()
	if issue.CreatedAt.IsZero() {
		issue.CreatedAt = now
	}
	if issue.UpdatedAt.IsZero() {
		issue.UpdatedAt = now
	}
	if issue.ContentHash == "" {
		issue.ContentHash = issue.ComputeContentHash()
	}

	// Generate ID if not set (hq-8af330.10: fix duplicate primary key error)
	if issue.ID == "" {
		// Get configured prefix
		var prefix string
		err := t.tx.QueryRowContext(ctx, `SELECT value FROM config WHERE key = 'issue_prefix'`).Scan(&prefix)
		if err != nil || prefix == "" {
			prefix = "hq-" // fallback default
		}

		// Support PrefixOverride if set (from upstream)
		if issue.PrefixOverride != "" {
			prefix = issue.PrefixOverride
		} else if issue.IDPrefix != "" {
			// Combine with IDPrefix if set (e.g., "hq" + "wisp" → "hq-wisp")
			prefix = strings.TrimSuffix(prefix, "-") + "-" + issue.IDPrefix + "-"
		}

		// Generate hash-based ID with collision avoidance
		generated, err := generateIssueIDInTx(ctx, t.tx, prefix, issue, actor)
		if err != nil {
			return fmt.Errorf("failed to generate issue ID: %w", err)
		}
		issue.ID = generated
	}

	return insertIssueTx(ctx, t.tx, issue)
}

// generateIssueIDInTx generates a unique hash-based ID within a transaction
func generateIssueIDInTx(ctx context.Context, tx *sql.Tx, prefix string, issue *types.Issue, actor string) (string, error) {
	// Use adaptive length starting at 6, up to 8
	for length := 6; length <= 8; length++ {
		// Try up to 10 nonces at each length
		for nonce := 0; nonce < 10; nonce++ {
			candidate := idgen.GenerateHashID(prefix, issue.Title, issue.Description, actor, issue.CreatedAt, length, nonce)

			// Check if this ID already exists
			var exists bool
			err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issues WHERE id = ?)`, candidate).Scan(&exists)
			if err != nil {
				return "", fmt.Errorf("failed to check for ID collision: %w", err)
			}

			if !exists {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("failed to generate unique ID after trying lengths 6-8 with 10 nonces each")
}

// CreateIssues creates multiple issues within the transaction
func (t *doltTransaction) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	for _, issue := range issues {
		if err := t.CreateIssue(ctx, issue, actor); err != nil {
			return err
		}
	}
	return nil
}

// GetIssue retrieves an issue within the transaction
func (t *doltTransaction) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return scanIssueTx(ctx, t.tx, id)
}

// SearchIssues searches for issues within the transaction
func (t *doltTransaction) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	// Simplified search for transaction context
	whereClauses := []string{}
	args := []interface{}{}

	if query != "" {
		whereClauses = append(whereClauses, "(title LIKE ? OR description LIKE ? OR id LIKE ?)")
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
	}

	if filter.Status != nil {
		whereClauses = append(whereClauses, "status = ?")
		args = append(args, *filter.Status)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	rows, err := t.tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT id FROM issues %s ORDER BY priority ASC, created_at DESC
	`, whereSQL), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		issue, err := t.GetIssue(ctx, id)
		if err != nil {
			return nil, err
		}
		if issue != nil {
			issues = append(issues, issue)
		}
	}
	return issues, rows.Err()
}

// UpdateIssue updates an issue within the transaction
func (t *doltTransaction) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	setClauses := []string{"updated_at = ?"}
	args := []interface{}{time.Now().UTC()}

	for key, value := range updates {
		if !isAllowedUpdateField(key) {
			return fmt.Errorf("invalid field for update: %s", key)
		}
		columnName := key
		if key == "wisp" {
			columnName = "ephemeral"
		}
		setClauses = append(setClauses, fmt.Sprintf("`%s` = ?", columnName))
		args = append(args, value)
	}

	args = append(args, id)
	// nolint:gosec // G201: setClauses contains only column names (e.g. "status = ?"), actual values passed via args
	query := fmt.Sprintf("UPDATE issues SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := t.tx.ExecContext(ctx, query, args...)
	return err
}

// CloseIssue closes an issue within the transaction
func (t *doltTransaction) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	now := time.Now().UTC()
	_, err := t.tx.ExecContext(ctx, `
		UPDATE issues SET status = ?, closed_at = ?, updated_at = ?, close_reason = ?, closed_by_session = ?
		WHERE id = ?
	`, types.StatusClosed, now, now, reason, session, id)
	return err
}

// DeleteIssue deletes an issue within the transaction
func (t *doltTransaction) DeleteIssue(ctx context.Context, id string) error {
	_, err := t.tx.ExecContext(ctx, "DELETE FROM issues WHERE id = ?", id)
	return err
}

// AddDependency adds a dependency within the transaction
func (t *doltTransaction) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by, thread_id)
		VALUES (?, ?, ?, NOW(), ?, ?)
		ON DUPLICATE KEY UPDATE type = VALUES(type)
	`, dep.IssueID, dep.DependsOnID, dep.Type, actor, dep.ThreadID)
	return err
}

func (t *doltTransaction) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id
		FROM dependencies
		WHERE issue_id = ?
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []*types.Dependency
	for rows.Next() {
		var d types.Dependency
		var metadata sql.NullString
		var threadID sql.NullString
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &d.Type, &d.CreatedAt, &d.CreatedBy, &metadata, &threadID); err != nil {
			return nil, err
		}
		if metadata.Valid {
			d.Metadata = metadata.String
		}
		if threadID.Valid {
			d.ThreadID = threadID.String
		}
		deps = append(deps, &d)
	}
	return deps, rows.Err()
}

// RemoveDependency removes a dependency within the transaction
func (t *doltTransaction) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	_, err := t.tx.ExecContext(ctx, `
		DELETE FROM dependencies WHERE issue_id = ? AND depends_on_id = ?
	`, issueID, dependsOnID)
	return err
}

// AddLabel adds a label within the transaction
func (t *doltTransaction) AddLabel(ctx context.Context, issueID, label, actor string) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT IGNORE INTO labels (issue_id, label) VALUES (?, ?)
	`, issueID, label)
	return err
}

func (t *doltTransaction) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT label FROM labels WHERE issue_id = ? ORDER BY label`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var labels []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}

// RemoveLabel removes a label within the transaction
func (t *doltTransaction) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	_, err := t.tx.ExecContext(ctx, `
		DELETE FROM labels WHERE issue_id = ? AND label = ?
	`, issueID, label)
	return err
}

// SetConfig sets a config value within the transaction
func (t *doltTransaction) SetConfig(ctx context.Context, key, value string) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO config (`+"`key`"+`, value) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE value = VALUES(value)
	`, key, value)
	return err
}

// GetConfig gets a config value within the transaction
func (t *doltTransaction) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := t.tx.QueryRowContext(ctx, "SELECT value FROM config WHERE `key` = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetMetadata sets a metadata value within the transaction
func (t *doltTransaction) SetMetadata(ctx context.Context, key, value string) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO metadata (`+"`key`"+`, value) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE value = VALUES(value)
	`, key, value)
	return err
}

// GetMetadata gets a metadata value within the transaction
func (t *doltTransaction) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := t.tx.QueryRowContext(ctx, "SELECT value FROM metadata WHERE `key` = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (t *doltTransaction) ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error) {
	// Verify issue exists in tx
	iss, err := t.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	if iss == nil {
		return nil, fmt.Errorf("issue %s not found", issueID)
	}

	createdAt = createdAt.UTC()
	res, err := t.tx.ExecContext(ctx, `
		INSERT INTO comments (issue_id, author, text, created_at)
		VALUES (?, ?, ?, ?)
	`, issueID, author, text, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to add comment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get comment id: %w", err)
	}

	// mark dirty in tx
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO dirty_issues (issue_id, marked_at)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE marked_at = VALUES(marked_at)
	`, issueID, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	return &types.Comment{ID: id, IssueID: issueID, Author: author, Text: text, CreatedAt: createdAt}, nil
}

func (t *doltTransaction) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT id, issue_id, author, text, created_at
		FROM comments
		WHERE issue_id = ?
		ORDER BY created_at ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []*types.Comment
	for rows.Next() {
		var c types.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, &c)
	}
	return comments, rows.Err()
}

// AddComment adds a comment within the transaction
func (t *doltTransaction) AddComment(ctx context.Context, issueID, actor, comment string) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, issueID, types.EventCommented, actor, comment)
	return err
}

// Helper functions for transaction context

func insertIssueTx(ctx context.Context, tx *sql.Tx, issue *types.Issue) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO issues (
			id, content_hash, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, created_by, owner, updated_at, closed_at,
			sender, ephemeral, pinned, is_template, crystallizes,
			await_type, await_id, timeout_ns, waiters
		) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?
		)
	`,
		issue.ID, issue.ContentHash, issue.Title, issue.Description, issue.Design, issue.AcceptanceCriteria, issue.Notes,
		issue.Status, issue.Priority, issue.IssueType, nullString(issue.Assignee), nullInt(issue.EstimatedMinutes),
		issue.CreatedAt, issue.CreatedBy, issue.Owner, issue.UpdatedAt, issue.ClosedAt,
		issue.Sender, issue.Ephemeral, issue.Pinned, issue.IsTemplate, issue.Crystallizes,
		issue.AwaitType, issue.AwaitID, issue.Timeout.Nanoseconds(), formatJSONStringArray(issue.Waiters),
	)
	return err
}

func scanIssueTx(ctx context.Context, tx *sql.Tx, id string) (*types.Issue, error) {
	var issue types.Issue
	var createdAtStr, updatedAtStr sql.NullString // TEXT columns - must parse manually
	var closedAt sql.NullTime
	var estimatedMinutes sql.NullInt64
	var assignee, owner, contentHash, createdBy sql.NullString
	var ephemeral, pinned, isTemplate, crystallizes sql.NullInt64
	var awaitType, awaitID, waiters sql.NullString
	var timeoutNs sql.NullInt64

	err := tx.QueryRowContext(ctx, `
		SELECT id, content_hash, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, created_by, owner, updated_at, closed_at,
		       ephemeral, pinned, is_template, crystallizes,
		       await_type, await_id, timeout_ns, waiters
		FROM issues
		WHERE id = ?
	`, id).Scan(
		&issue.ID, &contentHash, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&createdAtStr, &createdBy, &owner, &updatedAtStr, &closedAt,
		&ephemeral, &pinned, &isTemplate, &crystallizes,
		&awaitType, &awaitID, &timeoutNs, &waiters,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse timestamp strings (TEXT columns require manual parsing)
	if createdAtStr.Valid {
		issue.CreatedAt = parseTimeString(createdAtStr.String)
	}
	if updatedAtStr.Valid {
		issue.UpdatedAt = parseTimeString(updatedAtStr.String)
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
	if owner.Valid {
		issue.Owner = owner.String
	}
	if createdBy.Valid {
		issue.CreatedBy = createdBy.String
	}
	if ephemeral.Valid && ephemeral.Int64 != 0 {
		issue.Ephemeral = true
	}
	if pinned.Valid && pinned.Int64 != 0 {
		issue.Pinned = true
	}
	if isTemplate.Valid && isTemplate.Int64 != 0 {
		issue.IsTemplate = true
	}
	if crystallizes.Valid && crystallizes.Int64 != 0 {
		issue.Crystallizes = true
	}
	// Gate fields
	if awaitType.Valid {
		issue.AwaitType = awaitType.String
	}
	if awaitID.Valid {
		issue.AwaitID = awaitID.String
	}
	if timeoutNs.Valid {
		issue.Timeout = time.Duration(timeoutNs.Int64)
	}
	if waiters.Valid && waiters.String != "" {
		issue.Waiters = parseJSONStringArray(waiters.String)
	}

	return &issue, nil
}

// CreateDecisionPoint creates a new decision point within the transaction.
// TODO(dolt-parity): Implement decision point support for Dolt backend.
func (t *doltTransaction) CreateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error {
	return fmt.Errorf("decision points not yet implemented for Dolt backend")
}

// GetDecisionPoint retrieves the decision point for an issue within the transaction.
// TODO(dolt-parity): Implement decision point support for Dolt backend.
func (t *doltTransaction) GetDecisionPoint(ctx context.Context, issueID string) (*types.DecisionPoint, error) {
	return nil, fmt.Errorf("decision points not yet implemented for Dolt backend")
}

// UpdateDecisionPoint updates an existing decision point within the transaction.
// TODO(dolt-parity): Implement decision point support for Dolt backend.
func (t *doltTransaction) UpdateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error {
	return fmt.Errorf("decision points not yet implemented for Dolt backend")
}

// ListPendingDecisions returns all decision points that haven't been responded to.
// TODO(dolt-parity): Implement decision point support for Dolt backend.
func (t *doltTransaction) ListPendingDecisions(ctx context.Context) ([]*types.DecisionPoint, error) {
	return nil, fmt.Errorf("decision points not yet implemented for Dolt backend")
}
