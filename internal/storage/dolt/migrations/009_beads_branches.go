package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateBeadsBranchesTable creates the beads_branches registry table used
// for per-branch merge strategy tracking. This table is the single source
// of truth for active branches and their merge strategies.
//
// Strategies: stay-on-main, merge-with-branch, merge-on-close
// Statuses: active, dormant, merged, abandoned
//
// Idempotent: checks for existing table before creating.
func MigrateBeadsBranchesTable(db *sql.DB) error {
	exists, err := tableExists(db, "beads_branches")
	if err != nil {
		return fmt.Errorf("checking beads_branches table: %w", err)
	}
	if exists {
		return nil
	}

	_, err = db.Exec(`CREATE TABLE beads_branches (
    branch_name VARCHAR(255) PRIMARY KEY,
    merge_strategy VARCHAR(32) NOT NULL DEFAULT 'stay-on-main',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)`)
	if err != nil {
		return fmt.Errorf("creating beads_branches table: %w", err)
	}

	return nil
}
