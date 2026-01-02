package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateDueDeferColumns adds the due_at and defer_until columns to the issues table.
// These columns support time-based task scheduling (GH#820):
// - due_at: when the issue should be completed
// - defer_until: hide from bd ready until this time passes
func MigrateDueDeferColumns(db *sql.DB) error {
	// Check if due_at column already exists
	var dueAtExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'due_at'
	`).Scan(&dueAtExists)
	if err != nil {
		return fmt.Errorf("failed to check due_at column: %w", err)
	}

	if !dueAtExists {
		// Add the due_at column (nullable DATETIME)
		_, err = db.Exec(`ALTER TABLE issues ADD COLUMN due_at DATETIME`)
		if err != nil {
			return fmt.Errorf("failed to add due_at column: %w", err)
		}
	}

	// Check if defer_until column already exists
	var deferUntilExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'defer_until'
	`).Scan(&deferUntilExists)
	if err != nil {
		return fmt.Errorf("failed to check defer_until column: %w", err)
	}

	if !deferUntilExists {
		// Add the defer_until column (nullable DATETIME)
		_, err = db.Exec(`ALTER TABLE issues ADD COLUMN defer_until DATETIME`)
		if err != nil {
			return fmt.Errorf("failed to add defer_until column: %w", err)
		}
	}

	// Create indexes for efficient filtering queries
	// These are critical for bd ready performance when filtering by defer_until

	// Index on due_at for overdue/upcoming queries
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_due_at ON issues(due_at)`)
	if err != nil {
		return fmt.Errorf("failed to create due_at index: %w", err)
	}

	// Index on defer_until for ready filtering
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_defer_until ON issues(defer_until)`)
	if err != nil {
		return fmt.Errorf("failed to create defer_until index: %w", err)
	}

	return nil
}
