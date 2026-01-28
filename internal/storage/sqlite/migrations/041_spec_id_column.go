package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSpecIDColumn adds the spec_id column and index to the issues table.
// spec_id links an issue to a specification document (path, ID, or URL).
func MigrateSpecIDColumn(db *sql.DB) error {
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'spec_id'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check spec_id column: %w", err)
	}

	if !columnExists {
		_, err = db.Exec(`ALTER TABLE issues ADD COLUMN spec_id TEXT DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("failed to add spec_id column: %w", err)
		}
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_spec_id ON issues(spec_id)`)
	if err != nil {
		return fmt.Errorf("failed to create index on spec_id: %w", err)
	}

	return nil
}
