package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateDecisionPointColumns adds decision point columns to the issues table.
// Decision points are gates that wait for structured human input via email/SMS/web.
func MigrateDecisionPointColumns(db *sql.DB) error {
	columns := []struct {
		name     string
		datatype string
		def      string // default value (empty if no default)
	}{
		// Core decision fields
		{"decision_prompt", "TEXT", ""},
		{"decision_options", "TEXT", ""},      // JSON array of DecisionOption
		{"decision_default", "TEXT", ""},      // Option ID for timeout fallback
		{"decision_selected", "TEXT", ""},     // Human's chosen option ID
		{"decision_text", "TEXT", ""},         // Human's text input
		{"decision_responded_at", "TEXT", ""}, // Timestamp of response
		{"decision_responded_by", "TEXT", ""}, // Who responded (email, user ID)

		// Iteration fields for refinement loop
		{"decision_iteration", "INTEGER", "1"},     // Current iteration (1-indexed)
		{"decision_max_iterations", "INTEGER", "3"}, // Max iterations before forcing choice
		{"decision_prior_id", "TEXT", ""},           // Links to previous iteration
		{"decision_guidance", "TEXT", ""},           // Text that triggered this iteration
	}

	for _, col := range columns {
		// Check if column already exists
		var exists bool
		err := db.QueryRow(`
			SELECT COUNT(*) > 0
			FROM pragma_table_info('issues')
			WHERE name = ?
		`, col.name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check %s column: %w", col.name, err)
		}

		if exists {
			continue
		}

		// Build ALTER statement
		alterSQL := fmt.Sprintf("ALTER TABLE issues ADD COLUMN %s %s", col.name, col.datatype)
		if col.def != "" {
			alterSQL += fmt.Sprintf(" DEFAULT %s", col.def)
		}

		if _, err := db.Exec(alterSQL); err != nil {
			return fmt.Errorf("failed to add %s column: %w", col.name, err)
		}
	}

	return nil
}
