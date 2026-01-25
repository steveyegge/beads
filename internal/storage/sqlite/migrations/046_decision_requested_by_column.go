package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateDecisionRequestedBy adds requested_by column to decision_points table.
// This column stores the agent/session that created the decision, enabling
// wake notifications when the decision is resolved.
func MigrateDecisionRequestedBy(db *sql.DB) error {
	// Check if column already exists
	var colCount int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('decision_points')
		WHERE name = 'requested_by'
	`).Scan(&colCount)
	if err != nil {
		return fmt.Errorf("failed to check for requested_by column: %w", err)
	}

	if colCount > 0 {
		return nil // Column already exists
	}

	_, err = db.Exec(`ALTER TABLE decision_points ADD COLUMN requested_by TEXT`)
	if err != nil {
		return fmt.Errorf("failed to add requested_by column: %w", err)
	}

	return nil
}
