package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateDecisionWorkflowColumns adds context, rationale, and urgency columns
// to decision_points table. These fields align beads with the canonical design
// from hq-946577.38:
// - context: background/analysis for the decision
// - rationale: explanation for why this choice was made
// - urgency: priority level (high, medium, low)
func MigrateDecisionWorkflowColumns(db *sql.DB) error {
	columns := []struct {
		name string
		ddl  string
	}{
		{"context", "ALTER TABLE decision_points ADD COLUMN context TEXT"},
		{"rationale", "ALTER TABLE decision_points ADD COLUMN rationale TEXT"},
		{"urgency", "ALTER TABLE decision_points ADD COLUMN urgency TEXT"},
	}

	for _, col := range columns {
		// Check if column already exists
		var colCount int
		err := db.QueryRow(`
			SELECT COUNT(*)
			FROM pragma_table_info('decision_points')
			WHERE name = ?
		`, col.name).Scan(&colCount)
		if err != nil {
			return fmt.Errorf("failed to check for %s column: %w", col.name, err)
		}

		if colCount > 0 {
			continue // Column already exists
		}

		_, err = db.Exec(col.ddl)
		if err != nil {
			return fmt.Errorf("failed to add %s column: %w", col.name, err)
		}
	}

	return nil
}
