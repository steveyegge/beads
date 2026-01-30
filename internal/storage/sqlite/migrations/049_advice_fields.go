package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateAdviceFields adds advice targeting columns to the issues table.
// These fields support hierarchical agent advice:
//   - advice_target_rig: matches agent's rig field
//   - advice_target_role: matches agent's role_type field
//   - advice_target_agent: matches agent's ID exactly
//
// Targeting hierarchy (most specific wins):
//   1. Agent-specific (advice_target_agent set)
//   2. Role-specific (advice_target_role set)
//   3. Rig-specific (advice_target_rig set)
//   4. Global (all empty)
func MigrateAdviceFields(db *sql.DB) error {
	columns := []struct {
		name    string
		sqlType string
	}{
		{"advice_target_rig", "TEXT DEFAULT ''"},
		{"advice_target_role", "TEXT DEFAULT ''"},
		{"advice_target_agent", "TEXT DEFAULT ''"},
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
		_, err = db.Exec(fmt.Sprintf(`ALTER TABLE issues ADD COLUMN %s %s`, col.name, col.sqlType))
		if err != nil {
			return fmt.Errorf("failed to add %s column: %w", col.name, err)
		}
	}

	// Add index for efficient advice queries
	// This index optimizes GetAdviceForAgent queries that filter by issue_type='advice'
	// and match on the targeting fields
	_, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_advice_targets
		ON issues (advice_target_rig, advice_target_role, advice_target_agent)
		WHERE issue_type = 'advice'
	`)
	if err != nil {
		return fmt.Errorf("failed to create advice targets index: %w", err)
	}

	return nil
}
