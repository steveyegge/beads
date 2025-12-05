package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateTombstoneColumns adds tombstone support columns to the issues table.
// These columns support inline soft-delete (bd-vw8) replacing deletions.jsonl:
// - deleted_at: when the issue was deleted
// - deleted_by: who deleted the issue
// - delete_reason: why the issue was deleted
// - original_type: the issue type before deletion (for tombstones)
func MigrateTombstoneColumns(db *sql.DB) error {
	columns := []struct {
		name         string
		definition   string
	}{
		{"deleted_at", "TEXT"},
		{"deleted_by", "TEXT DEFAULT ''"},
		{"delete_reason", "TEXT DEFAULT ''"},
		{"original_type", "TEXT DEFAULT ''"},
	}

	for _, col := range columns {
		var columnExists bool
		err := db.QueryRow(`
			SELECT COUNT(*) > 0
			FROM pragma_table_info('issues')
			WHERE name = ?
		`, col.name).Scan(&columnExists)
		if err != nil {
			return fmt.Errorf("failed to check %s column: %w", col.name, err)
		}

		if columnExists {
			continue
		}

		_, err = db.Exec(fmt.Sprintf(`ALTER TABLE issues ADD COLUMN %s %s`, col.name, col.definition))
		if err != nil {
			return fmt.Errorf("failed to add %s column: %w", col.name, err)
		}
	}

	return nil
}
