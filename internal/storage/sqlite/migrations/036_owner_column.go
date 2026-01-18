package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateOwnerColumn adds the owner column to the issues table.
// This tracks the human owner responsible for the issue, using git author email
// for HOP CV (curriculum vitae) attribution chains. See Decision 008.
func MigrateOwnerColumn(db *sql.DB) error {
	// Check if column already exists
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'owner'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check owner column: %w", err)
	}

	if columnExists {
		return nil
	}

	// Add the owner column
	_, err = db.Exec(`ALTER TABLE issues ADD COLUMN owner TEXT DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add owner column: %w", err)
	}

	return nil
}
