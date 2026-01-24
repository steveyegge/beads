package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateAutoCloseColumn adds the auto_close column to issues table.
// When auto_close is true for an epic, the epic will automatically close
// when all its children are closed.
func MigrateAutoCloseColumn(db *sql.DB) error {
	// Check if column already exists
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name='auto_close'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check auto_close column: %w", err)
	}

	if columnExists {
		return nil
	}

	_, err = db.Exec(`ALTER TABLE issues ADD COLUMN auto_close INTEGER DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("failed to add auto_close column: %w", err)
	}

	return nil
}
