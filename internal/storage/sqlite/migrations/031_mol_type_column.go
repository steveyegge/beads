package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateMolTypeColumn adds mol_type column to the issues table.
// This field distinguishes molecule types (swarm/patrol/work) for swarm coordination.
// Values: 'swarm' (multi-polecat coordination), 'patrol' (recurring ops), 'work' (regular, default)
func MigrateMolTypeColumn(db *sql.DB) error {
	// Check if column already exists
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'mol_type'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check mol_type column: %w", err)
	}

	if columnExists {
		return nil
	}

	// Add the column
	_, err = db.Exec(`ALTER TABLE issues ADD COLUMN mol_type TEXT DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add mol_type column: %w", err)
	}

	return nil
}
