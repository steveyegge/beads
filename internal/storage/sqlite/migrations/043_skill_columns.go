package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSkillColumns adds skill-specific fields to the issues table.
// These fields support the skill-as-bead pattern (hq-yhdzq):
//   - skill_name: canonical skill name (e.g., "go-testing")
//   - skill_version: semver version (e.g., "1.0.0")
//   - skill_category: category for organization (e.g., "testing", "devops")
//   - skill_inputs: JSON array of input requirements
//   - skill_outputs: JSON array of outputs produced
//   - skill_examples: JSON array of usage examples
//   - claude_skill_path: path to SKILL.md for Claude integration
func MigrateSkillColumns(db *sql.DB) error {
	columns := []struct {
		name    string
		sqlType string
	}{
		{"skill_name", "TEXT DEFAULT ''"},
		{"skill_version", "TEXT DEFAULT ''"},
		{"skill_category", "TEXT DEFAULT ''"},
		{"skill_inputs", "TEXT DEFAULT ''"},    // JSON array
		{"skill_outputs", "TEXT DEFAULT ''"},   // JSON array
		{"skill_examples", "TEXT DEFAULT ''"},  // JSON array
		{"claude_skill_path", "TEXT DEFAULT ''"},
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

	// Add index on skill_name for efficient skill lookups
	_, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_skill_name ON issues(skill_name)
		WHERE skill_name != ''
	`)
	if err != nil {
		return fmt.Errorf("failed to create skill_name index: %w", err)
	}

	// Add index on skill_category for category filtering
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_skill_category ON issues(skill_category)
		WHERE skill_category != ''
	`)
	if err != nil {
		return fmt.Errorf("failed to create skill_category index: %w", err)
	}

	return nil
}
