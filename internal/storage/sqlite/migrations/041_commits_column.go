package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateCommitsColumn adds the commits column to the issues table.
// This stores linked git commit SHAs as a JSON array string.
// Enables bd git link/unlink commands for issue-commit tracking.
func MigrateCommitsColumn(db *sql.DB) error {
	// Check if column already exists
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'commits'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check commits column: %w", err)
	}

	if columnExists {
		return nil
	}

	// Add the commits column (TEXT storing JSON array, nullable - empty means no commits)
	_, err = db.Exec(`ALTER TABLE issues ADD COLUMN commits TEXT DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add commits column: %w", err)
	}

	return nil
}
