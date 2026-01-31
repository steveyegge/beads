package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateAdviceHookFields adds advice hook columns to the issues table (hq--uaim).
// These fields enable advice beads to register stop hooks - commands that run
// at specific lifecycle points (session-end, before-commit, before-push, before-handoff).
//
// New columns:
//   - advice_hook_command: the shell command to execute
//   - advice_hook_trigger: when to run (session-end, before-commit, etc.)
//   - advice_hook_timeout: max execution time in seconds (default: 30)
//   - advice_hook_on_failure: what to do if hook fails (block, warn, ignore)
func MigrateAdviceHookFields(db *sql.DB) error {
	columns := []struct {
		name    string
		sqlType string
	}{
		{"advice_hook_command", "TEXT DEFAULT ''"},
		{"advice_hook_trigger", "TEXT DEFAULT ''"},
		{"advice_hook_timeout", "INTEGER DEFAULT 0"},
		{"advice_hook_on_failure", "TEXT DEFAULT ''"},
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

	// Add index for efficient advice hook queries
	// This index optimizes queries that find hooks for a specific trigger
	_, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_advice_hooks
		ON issues (advice_hook_trigger)
		WHERE issue_type = 'advice' AND advice_hook_command != ''
	`)
	if err != nil {
		return fmt.Errorf("failed to create advice hooks index: %w", err)
	}

	return nil
}
