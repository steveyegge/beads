package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateNamespaceColumns adds project and branch columns to the issues table
// to support branch-based issue namespacing (proposal: BRANCH_NAMESPACING.md).
//
// These columns separate issue identity from source location:
// - project: project identifier (e.g., "beads", "other-project")
// - branch: branch namespace (e.g., "main", "fix-auth")
//
// Together with the existing ID (hash), they form the fully qualified ID:
//   project:branch-hash or project:hash (main branch implied)
func MigrateNamespaceColumns(db *sql.DB) error {
	// Check if columns already exist
	var projectExists, branchExists bool
	err := db.QueryRow(`
		SELECT 
			COUNT(*) FILTER (WHERE name = 'project') > 0,
			COUNT(*) FILTER (WHERE name = 'branch') > 0
		FROM pragma_table_info('issues')
	`).Scan(&projectExists, &branchExists)
	if err != nil {
		return fmt.Errorf("failed to check namespace columns: %w", err)
	}

	if projectExists && branchExists {
		return nil
	}

	// Add project column if missing
	if !projectExists {
		_, err = db.Exec(`ALTER TABLE issues ADD COLUMN project TEXT DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("failed to add project column: %w", err)
		}
	}

	// Add branch column if missing
	if !branchExists {
		_, err = db.Exec(`ALTER TABLE issues ADD COLUMN branch TEXT DEFAULT 'main'`)
		if err != nil {
			return fmt.Errorf("failed to add branch column: %w", err)
		}
	}

	// Create index for efficient branch queries (project, branch)
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_project_branch 
		ON issues(project, branch)
	`)
	if err != nil {
		return fmt.Errorf("failed to create index on (project, branch): %w", err)
	}

	// Create index for efficient project queries
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_project 
		ON issues(project)
	`)
	if err != nil {
		return fmt.Errorf("failed to create index on project: %w", err)
	}

	return nil
}
