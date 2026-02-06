//go:build cgo

// Package dolt - database migrations
package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Migration represents a single database migration
type Migration struct {
	Name string
	Func func(context.Context, *sql.DB) error
}

// migrations is the ordered list of all migrations to run
// Migrations are run in order during database initialization
// NOTE: advice_target_fields migration removed - those columns are deprecated (bd-hhbu)
var migrations = []Migration{
	{"advice_hook_fields", migrateAdviceHookFields},
	{"advice_subscription_fields", migrateAdviceSubscriptionFields},
	{"blocked_issues_cache", migrateBlockedIssuesCache},
	{"drop_ready_issues_view", migrateDropReadyIssuesView},
}

// RunMigrations executes all registered migrations in order.
// Each migration is idempotent - it checks if changes are needed before applying.
func RunMigrations(ctx context.Context, db *sql.DB) error {
	for _, migration := range migrations {
		if err := migration.Func(ctx, db); err != nil {
			return fmt.Errorf("migration %s failed: %w", migration.Name, err)
		}
	}
	return nil
}

// columnExists checks if a column exists in the specified table using information_schema
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = ?
		  AND column_name = ?
	`, table, column).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check column %s.%s: %w", table, column, err)
	}
	return count > 0, nil
}

// addColumnIfNotExists adds a column to a table if it doesn't already exist
func addColumnIfNotExists(ctx context.Context, db *sql.DB, table, column, colType string) error {
	exists, err := columnExists(ctx, db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colType))
	if err != nil {
		// Ignore "duplicate column" errors (race condition with another process)
		if strings.Contains(err.Error(), "Duplicate column") ||
			strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("failed to add column %s.%s: %w", table, column, err)
	}
	return nil
}

// migrateAdviceHookFields adds advice hook columns to the issues table (hq--uaim).
// These fields enable advice beads to register stop hooks - commands that run
// at specific lifecycle points (session-end, before-commit, before-push, before-handoff).
//
// New columns:
//   - advice_hook_command: the shell command to execute
//   - advice_hook_trigger: when to run (session-end, before-commit, etc.)
//   - advice_hook_timeout: max execution time in seconds (default: 30)
//   - advice_hook_on_failure: what to do if hook fails (block, warn, ignore)
func migrateAdviceHookFields(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name    string
		sqlType string
	}{
		{"advice_hook_command", "TEXT DEFAULT ''"},
		{"advice_hook_trigger", "VARCHAR(32) DEFAULT ''"},
		{"advice_hook_timeout", "INT DEFAULT 0"},
		{"advice_hook_on_failure", "VARCHAR(16) DEFAULT ''"},
	}

	for _, col := range columns {
		if err := addColumnIfNotExists(ctx, db, "issues", col.name, col.sqlType); err != nil {
			return err
		}
	}

	// Add index for efficient advice hook queries.
	// Note: MySQL/Dolt doesn't support partial indexes like SQLite,
	// so we create a simple index on the trigger column.
	_, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_issues_advice_hook_trigger
		ON issues (advice_hook_trigger)
	`)
	if err != nil {
		// Ignore "index already exists" errors
		if !strings.Contains(err.Error(), "Duplicate key") &&
			!strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create advice hooks index: %w", err)
		}
	}

	return nil
}

// migrateBlockedIssuesCache creates the blocked_issues_cache table for performance (bd-b2ts).
// This materializes the recursive CTE from the ready_issues view, converting
// expensive recursive joins on every read into a simple NOT EXISTS check.
func migrateBlockedIssuesCache(ctx context.Context, db *sql.DB) error {
	// Check if table already exists
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_name = 'blocked_issues_cache'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for blocked_issues_cache: %w", err)
	}
	if count > 0 {
		return nil // Already exists
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE blocked_issues_cache (
			issue_id VARCHAR(255) PRIMARY KEY,
			CONSTRAINT fk_blocked_cache FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("failed to create blocked_issues_cache: %w", err)
	}

	return nil
}

// migrateDropReadyIssuesView drops the ready_issues VIEW which is no longer needed.
// GetReadyWork now uses the blocked_issues_cache table for O(1) lookups instead of
// the expensive recursive CTE that the VIEW executed on every query. (bd-b2ts)
func migrateDropReadyIssuesView(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "DROP VIEW IF EXISTS ready_issues")
	// DROP VIEW IF EXISTS is idempotent - no error if view doesn't exist
	return err
}

// migrateAdviceSubscriptionFields adds advice subscription columns to the issues table.
// These fields enable agent-customized advice filtering (gt-w2mh8a.4).
//
// New columns:
//   - advice_subscriptions: comma-separated list of advice tags to subscribe to
//   - advice_subscriptions_exclude: comma-separated list of advice tags to exclude
func migrateAdviceSubscriptionFields(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name    string
		sqlType string
	}{
		{"advice_subscriptions", "TEXT DEFAULT ''"},
		{"advice_subscriptions_exclude", "TEXT DEFAULT ''"},
	}

	for _, col := range columns {
		if err := addColumnIfNotExists(ctx, db, "issues", col.name, col.sqlType); err != nil {
			return err
		}
	}

	return nil
}
