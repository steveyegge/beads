package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSpecChangedAtColumn adds spec_changed_at column to issues table.
func MigrateSpecChangedAtColumn(db *sql.DB) error {
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'spec_changed_at'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check spec_changed_at column: %w", err)
	}

	if !columnExists {
		_, err = db.Exec(`ALTER TABLE issues ADD COLUMN spec_changed_at DATETIME`)
		if err != nil {
			return fmt.Errorf("failed to add spec_changed_at column: %w", err)
		}
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_spec_changed_at ON issues(spec_changed_at)`); err != nil {
		return fmt.Errorf("failed to create index on spec_changed_at: %w", err)
	}

	return nil
}
