// Package sqlite implements the storage interface using SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Import SQLite driver
	_ "github.com/mattn/go-sqlite3"
	"github.com/steveyegge/beads/internal/types"
)

// SQLiteStorage implements the Storage interface using SQLite
type SQLiteStorage struct {
	db *sql.DB
}

// New creates a new SQLite storage backend
func New(path string) (*SQLiteStorage, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open database with WAL mode for better concurrency
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize schema
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &SQLiteStorage{
		db: db,
	}, nil
}

// getNextIDForPrefix atomically generates the next ID for a given prefix
// Uses the issue_counters table for atomic, cross-process ID generation
func (s *SQLiteStorage) getNextIDForPrefix(ctx context.Context, prefix string) (int, error) {
	var nextID int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO issue_counters (prefix, last_id)
		VALUES (?, 1)
		ON CONFLICT(prefix) DO UPDATE SET
			last_id = last_id + 1
		RETURNING last_id
	`, prefix).Scan(&nextID)
	if err != nil {
		return 0, fmt.Errorf("failed to generate next ID for prefix %s: %w", prefix, err)
	}
	return nextID, nil
}

// CreateIssue creates a new issue
func (s *SQLiteStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Validate issue before creating
	if err := issue.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Generate ID if not set (using atomic counter table)
	if issue.ID == "" {
		// Get prefix from config, default to "bd"
		prefix, err := s.GetConfig(ctx, "issue_prefix")
		if err != nil || prefix == "" {
			prefix = "bd"
		}

		// Atomically get next ID from counter table
		nextID, err := s.getNextIDForPrefix(ctx, prefix)
		if err != nil {
			return err
		}

		issue.ID = fmt.Sprintf("%s-%d", prefix, nextID)
	}

	// Set timestamps
	now := time.Now()
	issue.CreatedAt = now
	issue.UpdatedAt = now

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert issue
	_, err = tx.ExecContext(ctx, `
		INSERT INTO issues (
			id, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		issue.ID, issue.Title, issue.Description, issue.Design,
		issue.AcceptanceCriteria, issue.Notes, issue.Status,
		issue.Priority, issue.IssueType, issue.Assignee,
		issue.EstimatedMinutes, issue.CreatedAt, issue.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert issue: %w", err)
	}

	// Record creation event
	eventData, err := json.Marshal(issue)
	if err != nil {
		// Fall back to minimal description if marshaling fails
		eventData = []byte(fmt.Sprintf(`{"id":"%s","title":"%s"}`, issue.ID, issue.Title))
	}
	eventDataStr := string(eventData)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, new_value)
		VALUES (?, ?, ?, ?)
	`, issue.ID, types.EventCreated, actor, eventDataStr)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit()
}

// GetIssue retrieves an issue by ID
func (s *SQLiteStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	var issue types.Issue
	var closedAt sql.NullTime
	var estimatedMinutes sql.NullInt64
	var assignee sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, updated_at, closed_at
		FROM issues
		WHERE id = ?
	`, id).Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&issue.CreatedAt, &issue.UpdatedAt, &closedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
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

	return &issue, nil
}

// Allowed fields for update to prevent SQL injection
var allowedUpdateFields = map[string]bool{
	"status":              true,
	"priority":            true,
	"title":               true,
	"assignee":            true,
	"description":         true,
	"design":              true,
	"acceptance_criteria": true,
	"notes":               true,
	"issue_type":          true,
	"estimated_minutes":   true,
}

// UpdateIssue updates fields on an issue
func (s *SQLiteStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Get old issue for event
	oldIssue, err := s.GetIssue(ctx, id)
	if err != nil {
		return err
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
		switch key {
		case "priority":
			if priority, ok := value.(int); ok {
				if priority < 0 || priority > 4 {
					return fmt.Errorf("priority must be between 0 and 4 (got %d)", priority)
				}
			}
		case "status":
			if status, ok := value.(string); ok {
				if !types.Status(status).IsValid() {
					return fmt.Errorf("invalid status: %s", status)
				}
			}
		case "issue_type":
			if issueType, ok := value.(string); ok {
				if !types.IssueType(issueType).IsValid() {
					return fmt.Errorf("invalid issue type: %s", issueType)
				}
			}
		case "title":
			if title, ok := value.(string); ok {
				if len(title) == 0 || len(title) > 500 {
					return fmt.Errorf("title must be 1-500 characters")
				}
			}
		case "estimated_minutes":
			if mins, ok := value.(int); ok {
				if mins < 0 {
					return fmt.Errorf("estimated_minutes cannot be negative")
				}
			}
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = ?", key))
		args = append(args, value)
	}
	args = append(args, id)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update issue
	query := fmt.Sprintf("UPDATE issues SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Record event
	oldData, err := json.Marshal(oldIssue)
	if err != nil {
		// Fall back to minimal description if marshaling fails
		oldData = []byte(fmt.Sprintf(`{"id":"%s"}`, id))
	}
	newData, err := json.Marshal(updates)
	if err != nil {
		// Fall back to minimal description if marshaling fails
		newData = []byte(`{}`)
	}
	oldDataStr := string(oldData)
	newDataStr := string(newData)

	eventType := types.EventUpdated
	if statusVal, ok := updates["status"]; ok {
		if statusVal == string(types.StatusClosed) {
			eventType = types.EventClosed
		} else {
			eventType = types.EventStatusChanged
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)
	`, id, eventType, actor, oldDataStr, newDataStr)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit()
}

// CloseIssue closes an issue with a reason
func (s *SQLiteStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	now := time.Now()

	// Update with special event handling
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE issues SET status = ?, closed_at = ?, updated_at = ?
		WHERE id = ?
	`, types.StatusClosed, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to close issue: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, id, types.EventClosed, actor, reason)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit()
}

// SearchIssues finds issues matching query and filters
func (s *SQLiteStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
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

	if filter.Priority != nil {
		whereClauses = append(whereClauses, "priority = ?")
		args = append(args, *filter.Priority)
	}

	if filter.IssueType != nil {
		whereClauses = append(whereClauses, "issue_type = ?")
		args = append(args, *filter.IssueType)
	}

	if filter.Assignee != nil {
		whereClauses = append(whereClauses, "assignee = ?")
		args = append(args, *filter.Assignee)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = " LIMIT ?"
		args = append(args, filter.Limit)
	}

	querySQL := fmt.Sprintf(`
		SELECT id, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, updated_at, closed_at
		FROM issues
		%s
		ORDER BY priority ASC, created_at DESC
		%s
	`, whereSQL, limitSQL)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search issues: %w", err)
	}
	defer rows.Close()

	return scanIssues(rows)
}

// SetConfig sets a configuration value
func (s *SQLiteStorage) SetConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// GetConfig gets a configuration value
func (s *SQLiteStorage) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}
