package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSkillContentColumn adds skill_content column to store SKILL.md content directly.
// This replaces the need for claude_skill_path pointing to external files.
func MigrateSkillContentColumn(db *sql.DB) error {
	// Check if column already exists
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'skill_content'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check skill_content column: %w", err)
	}

	if columnExists {
		return nil
	}

	// Add the column - TEXT can store large SKILL.md content
	_, err = db.Exec(`ALTER TABLE issues ADD COLUMN skill_content TEXT DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add skill_content column: %w", err)
	}

	return nil
}
