package dolt

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/config"
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

	if err := sqlTx.Commit(); err != nil {
		return err
	}

	// Note: blocked_issues_cache rebuild is handled by the daemon event loop
	// with debouncing (bd-b2ts). Rebuilding here caused double-fire on every
	// write transaction. The daemon's mutation channel triggers a debounced
	// rebuild that coalesces rapid mutations into a single cache refresh.

	return nil
}

// CreateIssue creates an issue within the transaction (full fidelity: matches DoltStore.CreateIssue)
func (t *doltTransaction) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Fetch custom statuses and types for validation
	customStatuses, err := t.getCustomStatuses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get custom statuses: %w", err)
	}
	customTypes, err := t.getCustomTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get custom types: %w", err)
	}

	// Set timestamps
	now := time.Now().UTC()
	if issue.CreatedAt.IsZero() {
		issue.CreatedAt = now
	}
	if issue.UpdatedAt.IsZero() {
		issue.UpdatedAt = now
	}

	// Defensive fix for closed_at invariant
	if issue.Status == types.StatusClosed && issue.ClosedAt == nil {
		maxTime := issue.CreatedAt
		if issue.UpdatedAt.After(maxTime) {
			maxTime = issue.UpdatedAt
		}
		closedAt := maxTime.Add(time.Second)
		issue.ClosedAt = &closedAt
	}

	// Defensive fix for deleted_at invariant
	if issue.Status == types.StatusTombstone && issue.DeletedAt == nil {
		maxTime := issue.CreatedAt
		if issue.UpdatedAt.After(maxTime) {
			maxTime = issue.UpdatedAt
		}
		deletedAt := maxTime.Add(time.Second)
		issue.DeletedAt = &deletedAt
	}

	// Validate issue
	if err := issue.ValidateWithCustom(customStatuses, customTypes); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Compute content hash
	if issue.ContentHash == "" {
		issue.ContentHash = issue.ComputeContentHash()
	}

	// Generate ID if not set (hq-8af330.10: fix duplicate primary key error)
	if issue.ID == "" {
		// Get configured prefix
		var prefix string
		err := t.tx.QueryRowContext(ctx, "SELECT value FROM config WHERE `key` = 'issue_prefix'").Scan(&prefix)
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

		// Generate hash-based ID with collision avoidance (adaptive length)
		generated, err := generateIssueIDInTx(ctx, t.tx, prefix, issue, actor)
		if err != nil {
			return fmt.Errorf("failed to generate issue ID: %w", err)
		}
		issue.ID = generated
	}

	// Insert issue (full column set)
	if err := insertIssue(ctx, t.tx, issue); err != nil {
		return fmt.Errorf("failed to insert issue: %w", err)
	}

	// Record creation event
	if err := recordEvent(ctx, t.tx, issue.ID, types.EventCreated, actor, "", ""); err != nil {
		return fmt.Errorf("failed to record creation event: %w", err)
	}

	// Mark issue as dirty
	if err := t.store.markDirty(ctx, t.tx, issue.ID); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	return nil
}

// generateIssueIDInTx generates a unique hash-based ID within a transaction (adaptive length)
func generateIssueIDInTx(ctx context.Context, tx *sql.Tx, prefix string, issue *types.Issue, actor string) (string, error) {
	// Get adaptive base length based on current database size
	baseLength, err := GetAdaptiveIDLengthTx(ctx, tx, prefix)
	if err != nil {
		// Fallback to 6 on error
		baseLength = 6
	}

	maxLength := 8
	if baseLength > maxLength {
		baseLength = maxLength
	}

	for length := baseLength; length <= maxLength; length++ {
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

	return "", fmt.Errorf("failed to generate unique ID after trying lengths %d-%d with 10 nonces each", baseLength, maxLength)
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

// UpdateIssue updates an issue within the transaction (full fidelity: matches DoltStore.UpdateIssue)
func (t *doltTransaction) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	oldIssue, err := scanIssueTx(ctx, t.tx, id)
	if err != nil {
		return fmt.Errorf("failed to get issue for update: %w", err)
	}
	if oldIssue == nil {
		return fmt.Errorf("issue %s not found", id)
	}

	// Build update query
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

		// Handle JSON serialization for array fields stored as TEXT
		switch key {
		case "waiters", "advice_subscriptions", "advice_subscriptions_exclude":
			jsonData, _ := json.Marshal(value)
			args = append(args, string(jsonData))
		default:
			args = append(args, value)
		}
	}

	// Auto-manage closed_at
	setClauses, args = manageClosedAt(oldIssue, updates, setClauses, args)

	args = append(args, id)

	// nolint:gosec // G201: setClauses contains only column names (e.g. "status = ?"), actual values passed via args
	query := fmt.Sprintf("UPDATE issues SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	if _, err := t.tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Record event
	oldData, _ := json.Marshal(oldIssue)
	newData, _ := json.Marshal(updates)
	eventType := determineEventType(oldIssue, updates)

	if err := recordEvent(ctx, t.tx, id, eventType, actor, string(oldData), string(newData)); err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	if err := t.store.markDirty(ctx, t.tx, id); err != nil {
		return fmt.Errorf("failed to mark dirty: %w", err)
	}

	return nil
}

// CloseIssue closes an issue within the transaction (full fidelity: matches DoltStore.CloseIssue)
func (t *doltTransaction) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	now := time.Now().UTC()
	result, err := t.tx.ExecContext(ctx, `
		UPDATE issues SET status = ?, closed_at = ?, updated_at = ?, close_reason = ?, closed_by_session = ?
		WHERE id = ?
	`, types.StatusClosed, now, now, reason, session, id)
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

	if err := recordEvent(ctx, t.tx, id, types.EventClosed, actor, "", reason); err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	if err := t.store.markDirty(ctx, t.tx, id); err != nil {
		return fmt.Errorf("failed to mark dirty: %w", err)
	}

	return nil
}

// DeleteIssue deletes an issue within the transaction (full fidelity: matches DoltStore.DeleteIssue)
func (t *doltTransaction) DeleteIssue(ctx context.Context, id string) error {
	// Delete related data (foreign keys will cascade, but be explicit)
	tables := []string{"dependencies", "events", "comments", "labels", "dirty_issues"}
	for _, table := range tables {
		var err error
		if table == "dependencies" {
			_, err = t.tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE issue_id = ? OR depends_on_id = ?", table), id, id)
		} else {
			_, err = t.tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE issue_id = ?", table), id)
		}
		if err != nil {
			return fmt.Errorf("failed to delete from %s: %w", table, err)
		}
	}

	result, err := t.tx.ExecContext(ctx, "DELETE FROM issues WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete issue: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("issue not found: %s", id)
	}

	return nil
}

// AddDependency adds a dependency within the transaction (full fidelity: matches DoltStore.AddDependency)
func (t *doltTransaction) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	metadata := dep.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	// Pre-check: verify both issues exist (converts FK errors to user-friendly "not found" messages)
	var exists int
	if err := t.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues WHERE id = ?", dep.IssueID).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check issue existence: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("issue %q not found", dep.IssueID)
	}

	if !strings.HasPrefix(dep.DependsOnID, "external:") {
		if err := t.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues WHERE id = ?", dep.DependsOnID).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check dependency target existence: %w", err)
		}
		if exists == 0 {
			return fmt.Errorf("dependency target %q not found", dep.DependsOnID)
		}
	}

	// Cycle detection: check if adding this dependency would create a cycle.
	// A cycle exists if dep.DependsOnID can already reach dep.IssueID via existing dependencies.
	if dep.Type == types.DepBlocks {
		if wouldCreateCycle(ctx, t.tx, dep.IssueID, dep.DependsOnID) {
			return fmt.Errorf("adding dependency %s -> %s would create a cycle", dep.IssueID, dep.DependsOnID)
		}
	}

	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id)
		VALUES (?, ?, ?, NOW(), ?, ?, ?)
		ON DUPLICATE KEY UPDATE type = VALUES(type), metadata = VALUES(metadata)
	`, dep.IssueID, dep.DependsOnID, dep.Type, actor, metadata, dep.ThreadID)
	if err != nil {
		return fmt.Errorf("failed to add dependency: %w", err)
	}

	// Mark source issue as dirty for incremental export
	if err := t.store.markDirty(ctx, t.tx, dep.IssueID); err != nil {
		return fmt.Errorf("failed to mark issue dirty: %w", err)
	}

	// Only mark depends_on as dirty if it's a local issue (not an external reference)
	if !strings.HasPrefix(dep.DependsOnID, "external:") {
		if err := t.store.markDirty(ctx, t.tx, dep.DependsOnID); err != nil {
			return fmt.Errorf("failed to mark depends_on issue dirty: %w", err)
		}
	}

	return nil
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

// wouldCreateCycle checks whether adding an edge from issueID -> dependsOnID
// would create a cycle in the dependency graph. It does a BFS from dependsOnID
// to see if it can reach issueID through existing "blocks" dependencies.
func wouldCreateCycle(ctx context.Context, tx *sql.Tx, issueID, dependsOnID string) bool {
	visited := make(map[string]bool)
	queue := []string{dependsOnID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == issueID {
			return true // Found a path back to issueID — cycle!
		}
		if visited[current] {
			continue
		}
		visited[current] = true

		rows, err := tx.QueryContext(ctx,
			"SELECT depends_on_id FROM dependencies WHERE issue_id = ? AND type = ?",
			current, types.DepBlocks)
		if err != nil {
			continue
		}
		for rows.Next() {
			var next string
			if err := rows.Scan(&next); err != nil {
				continue
			}
			if !visited[next] {
				queue = append(queue, next)
			}
		}
		rows.Close()
	}
	return false
}

// RemoveDependency removes a dependency within the transaction (full fidelity: matches DoltStore.RemoveDependency)
func (t *doltTransaction) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	result, err := t.tx.ExecContext(ctx, `
		DELETE FROM dependencies WHERE issue_id = ? AND depends_on_id = ?
	`, issueID, dependsOnID)
	if err != nil {
		return fmt.Errorf("failed to remove dependency: %w", err)
	}

	// Only mark dirty if something was actually deleted
	rows, _ := result.RowsAffected()
	if rows > 0 {
		if err := t.store.markDirty(ctx, t.tx, issueID); err != nil {
			return fmt.Errorf("failed to mark issue dirty: %w", err)
		}
		if err := t.store.markDirty(ctx, t.tx, dependsOnID); err != nil {
			return fmt.Errorf("failed to mark depends_on issue dirty: %w", err)
		}
	}

	return nil
}

// AddLabel adds a label within the transaction (full fidelity: matches DoltStore.AddLabel)
func (t *doltTransaction) AddLabel(ctx context.Context, issueID, label, actor string) error {
	result, err := t.tx.ExecContext(ctx, `
		INSERT IGNORE INTO labels (issue_id, label) VALUES (?, ?)
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to add label: %w", err)
	}

	// Only record event if label was actually added
	rows, _ := result.RowsAffected()
	if rows > 0 {
		if err := recordEvent(ctx, t.tx, issueID, types.EventLabelAdded, actor, "", fmt.Sprintf("Added label: %s", label)); err != nil {
			return fmt.Errorf("failed to record event: %w", err)
		}
		if err := t.store.markDirty(ctx, t.tx, issueID); err != nil {
			return fmt.Errorf("failed to mark dirty: %w", err)
		}
	}

	return nil
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

// RemoveLabel removes a label within the transaction (full fidelity: matches DoltStore.RemoveLabel)
func (t *doltTransaction) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	result, err := t.tx.ExecContext(ctx, `
		DELETE FROM labels WHERE issue_id = ? AND label = ?
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}

	// Only record event if label was actually removed
	rows, _ := result.RowsAffected()
	if rows > 0 {
		if err := recordEvent(ctx, t.tx, issueID, types.EventLabelRemoved, actor, "", fmt.Sprintf("Removed label: %s", label)); err != nil {
			return fmt.Errorf("failed to record event: %w", err)
		}
		if err := t.store.markDirty(ctx, t.tx, issueID); err != nil {
			return fmt.Errorf("failed to mark dirty: %w", err)
		}
	}

	return nil
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

// getCustomStatuses retrieves custom statuses within the transaction
func (t *doltTransaction) getCustomStatuses(ctx context.Context) ([]string, error) {
	value, err := t.GetConfig(ctx, "status.custom")
	if err != nil {
		if yamlStatuses := config.GetCustomStatusesFromYAML(); len(yamlStatuses) > 0 {
			return yamlStatuses, nil
		}
		return nil, err
	}
	if value != "" {
		return parseCommaSeparatedList(value), nil
	}
	if yamlStatuses := config.GetCustomStatusesFromYAML(); len(yamlStatuses) > 0 {
		return yamlStatuses, nil
	}
	return nil, nil
}

// getCustomTypes retrieves custom types within the transaction
func (t *doltTransaction) getCustomTypes(ctx context.Context) ([]string, error) {
	value, err := t.GetConfig(ctx, "types.custom")
	if err != nil {
		if yamlTypes := config.GetCustomTypesFromYAML(); len(yamlTypes) > 0 {
			return yamlTypes, nil
		}
		return nil, err
	}
	if value != "" {
		return parseCommaSeparatedList(value), nil
	}
	if yamlTypes := config.GetCustomTypesFromYAML(); len(yamlTypes) > 0 {
		return yamlTypes, nil
	}
	return nil, nil
}

// scanIssueTx retrieves a full-fidelity issue within a transaction (matches scanIssue column set)
func scanIssueTx(ctx context.Context, tx *sql.Tx, id string) (*types.Issue, error) {
	var issue types.Issue
	var createdAtStr, updatedAtStr sql.NullString // TEXT columns - must parse manually
	var closedAt, compactedAt, deletedAt, lastActivity, dueAt, deferUntil sql.NullTime
	var estimatedMinutes, originalSize, timeoutNs sql.NullInt64
	var assignee, externalRef, compactedAtCommit, owner, createdBy sql.NullString
	var contentHash, sourceRepo, closeReason, deletedBy, deleteReason, originalType sql.NullString
	var workType, sourceSystem sql.NullString
	var sender, molType, eventKind, actor, target, payload sql.NullString
	var awaitType, awaitID, waiters sql.NullString
	var hookBead, roleBead, agentState, roleType, rig sql.NullString
	var podName, podIP, podNode, podStatus, screenSession sql.NullString
	var ephemeral, pinned, isTemplate, crystallizes sql.NullInt64
	var qualityScore sql.NullFloat64
	var metadata sql.NullString
	var adviceHookCommand, adviceHookTrigger, adviceHookOnFailure sql.NullString
	var adviceHookTimeout sql.NullInt64
	var adviceSubscriptions, adviceSubscriptionsExclude sql.NullString

	err := tx.QueryRowContext(ctx, `
		SELECT id, content_hash, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, created_by, owner, updated_at, closed_at, external_ref,
		       compaction_level, compacted_at, compacted_at_commit, original_size, source_repo, close_reason,
		       deleted_at, deleted_by, delete_reason, original_type,
		       sender, ephemeral, pinned, is_template, crystallizes,
		       await_type, await_id, timeout_ns, waiters,
		       hook_bead, role_bead, agent_state, last_activity, role_type, rig,
		       pod_name, pod_ip, pod_node, pod_status, screen_session,
		       mol_type,
		       event_kind, actor, target, payload,
		       due_at, defer_until,
		       quality_score, work_type, source_system, metadata,
		       advice_hook_command, advice_hook_trigger, advice_hook_timeout, advice_hook_on_failure,
		       advice_subscriptions, advice_subscriptions_exclude
		FROM issues
		WHERE id = ?
	`, id).Scan(
		&issue.ID, &contentHash, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&createdAtStr, &createdBy, &owner, &updatedAtStr, &closedAt, &externalRef,
		&issue.CompactionLevel, &compactedAt, &compactedAtCommit, &originalSize, &sourceRepo, &closeReason,
		&deletedAt, &deletedBy, &deleteReason, &originalType,
		&sender, &ephemeral, &pinned, &isTemplate, &crystallizes,
		&awaitType, &awaitID, &timeoutNs, &waiters,
		&hookBead, &roleBead, &agentState, &lastActivity, &roleType, &rig,
		&podName, &podIP, &podNode, &podStatus, &screenSession,
		&molType,
		&eventKind, &actor, &target, &payload,
		&dueAt, &deferUntil,
		&qualityScore, &workType, &sourceSystem, &metadata,
		&adviceHookCommand, &adviceHookTrigger, &adviceHookTimeout, &adviceHookOnFailure,
		&adviceSubscriptions, &adviceSubscriptionsExclude,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	// Parse timestamp strings (TEXT columns require manual parsing)
	if createdAtStr.Valid {
		issue.CreatedAt = parseTimeString(createdAtStr.String)
	}
	if updatedAtStr.Valid {
		issue.UpdatedAt = parseTimeString(updatedAtStr.String)
	}

	// Map nullable fields
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
	if closeReason.Valid {
		issue.CloseReason = closeReason.String
	}
	if deletedAt.Valid {
		issue.DeletedAt = &deletedAt.Time
	}
	if deletedBy.Valid {
		issue.DeletedBy = deletedBy.String
	}
	if deleteReason.Valid {
		issue.DeleteReason = deleteReason.String
	}
	if originalType.Valid {
		issue.OriginalType = originalType.String
	}
	if sender.Valid {
		issue.Sender = sender.String
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
	if hookBead.Valid {
		issue.HookBead = hookBead.String
	}
	if roleBead.Valid {
		issue.RoleBead = roleBead.String
	}
	if agentState.Valid {
		issue.AgentState = types.AgentState(agentState.String)
	}
	if lastActivity.Valid {
		issue.LastActivity = &lastActivity.Time
	}
	if roleType.Valid {
		issue.RoleType = roleType.String
	}
	if rig.Valid {
		issue.Rig = rig.String
	}
	if podName.Valid {
		issue.PodName = podName.String
	}
	if podIP.Valid {
		issue.PodIP = podIP.String
	}
	if podNode.Valid {
		issue.PodNode = podNode.String
	}
	if podStatus.Valid {
		issue.PodStatus = podStatus.String
	}
	if screenSession.Valid {
		issue.ScreenSession = screenSession.String
	}
	if molType.Valid {
		issue.MolType = types.MolType(molType.String)
	}
	if eventKind.Valid {
		issue.EventKind = eventKind.String
	}
	if actor.Valid {
		issue.Actor = actor.String
	}
	if target.Valid {
		issue.Target = target.String
	}
	if payload.Valid {
		issue.Payload = payload.String
	}
	if dueAt.Valid {
		issue.DueAt = &dueAt.Time
	}
	if deferUntil.Valid {
		issue.DeferUntil = &deferUntil.Time
	}
	if qualityScore.Valid {
		qs := float32(qualityScore.Float64)
		issue.QualityScore = &qs
	}
	if workType.Valid {
		issue.WorkType = types.WorkType(workType.String)
	}
	if sourceSystem.Valid {
		issue.SourceSystem = sourceSystem.String
	}
	if metadata.Valid && metadata.String != "" && metadata.String != "{}" {
		issue.Metadata = []byte(metadata.String)
	}
	if adviceHookCommand.Valid {
		issue.AdviceHookCommand = adviceHookCommand.String
	}
	if adviceHookTrigger.Valid {
		issue.AdviceHookTrigger = adviceHookTrigger.String
	}
	if adviceHookTimeout.Valid {
		issue.AdviceHookTimeout = int(adviceHookTimeout.Int64)
	}
	if adviceHookOnFailure.Valid {
		issue.AdviceHookOnFailure = adviceHookOnFailure.String
	}
	if adviceSubscriptions.Valid && adviceSubscriptions.String != "" {
		issue.AdviceSubscriptions = parseJSONStringArray(adviceSubscriptions.String)
	}
	if adviceSubscriptionsExclude.Valid && adviceSubscriptionsExclude.String != "" {
		issue.AdviceSubscriptionsExclude = parseJSONStringArray(adviceSubscriptionsExclude.String)
	}

	return &issue, nil
}
