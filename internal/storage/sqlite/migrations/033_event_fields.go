package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateEventFields adds event-specific fields to the issues table.
// These fields support type: event beads for operational state changes.
// Fields:
//   - event_kind: namespaced event type (e.g., patrol.muted, agent.started)
//   - actor: entity URI who caused this event
//   - target: entity URI or bead ID affected
//   - payload: event-specific JSON data
func MigrateEventFields(db *sql.DB) error {
	columns := []struct {
		name string
		def  string
	}{
		{"event_kind", "TEXT DEFAULT ''"},
		{"actor", "TEXT DEFAULT ''"},
		{"target", "TEXT DEFAULT ''"},
		{"payload", "TEXT DEFAULT ''"},
	}

	for _, col := range columns {
		// Check if column already exists
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

		// Add the column
		_, err = db.Exec(fmt.Sprintf(`ALTER TABLE issues ADD COLUMN %s %s`, col.name, col.def))
		if err != nil {
			return fmt.Errorf("failed to add %s column: %w", col.name, err)
		}
	}

	return nil
}
