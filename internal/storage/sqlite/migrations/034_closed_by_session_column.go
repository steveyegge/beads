package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateClosedBySessionColumn adds the closed_by_session column to the issues table.
// This tracks which Claude Code session closed the issue, enabling work attribution
// for entity CV building. See Gas Town decision 009-session-events-architecture.md.
func MigrateClosedBySessionColumn(db *sql.DB) error {
	// Check if column already exists
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'closed_by_session'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check closed_by_session column: %w", err)
	}

	if columnExists {
		return nil
	}

	// Add the closed_by_session column
	_, err = db.Exec(`ALTER TABLE issues ADD COLUMN closed_by_session TEXT DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add closed_by_session column: %w", err)
	}

	return nil
}
