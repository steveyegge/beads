package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateReminderCountColumn adds the reminder_count column to decision_points table.
// This tracks how many reminders have been sent for a pending decision.
func MigrateReminderCountColumn(db *sql.DB) error {
	// Check if decision_points table exists first
	var tableExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type='table' AND name='decision_points'
	`).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check decision_points table: %w", err)
	}

	if !tableExists {
		// Table doesn't exist yet, nothing to migrate
		// The column will be added when decision_points table is created
		return nil
	}

	// Check if column already exists
	var columnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('decision_points')
		WHERE name='reminder_count'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check reminder_count column: %w", err)
	}

	if columnExists {
		return nil
	}

	_, err = db.Exec(`ALTER TABLE decision_points ADD COLUMN reminder_count INTEGER DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("failed to add reminder_count column: %w", err)
	}

	return nil
}
