package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateIUFColumns adds importance, urgency, and feasibility columns to the issues table.
// These fields support IUF priority scoring: P = (2×I + U) × F → P0-P4
// Values: 1-3 (1=low/blocked, 2=medium/partial, 3=high/ready)
func MigrateIUFColumns(db *sql.DB) error {
	columns := []string{"importance", "urgency", "feasibility"}

	for _, col := range columns {
		// Check if column already exists
		var columnExists bool
		err := db.QueryRow(`
			SELECT COUNT(*) > 0
			FROM pragma_table_info('issues')
			WHERE name = ?
		`, col).Scan(&columnExists)
		if err != nil {
			return fmt.Errorf("failed to check %s column: %w", col, err)
		}

		if columnExists {
			continue
		}

		// Add the column (NULL default, since these are optional)
		_, err = db.Exec(fmt.Sprintf(`ALTER TABLE issues ADD COLUMN %s INTEGER`, col))
		if err != nil {
			return fmt.Errorf("failed to add %s column: %w", col, err)
		}
	}

	return nil
}
