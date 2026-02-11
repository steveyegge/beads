package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateHookedStatus converts pinned work items to hooked status.
// 'pinned' now means identity/domain records (agents, roles).
// 'hooked' means work actively attached to an agent's hook (GUPP).
func MigrateHookedStatus(db *sql.DB) error {
	// Migrate pinned issues that represent work (not identity records) to hooked.
	// Agent/role beads stay pinned; molecules and regular issues become hooked.
	result, err := db.Exec(`
		UPDATE issues
		SET status = 'hooked'
		WHERE status = 'pinned'
		  AND issue_type NOT IN ('agent', 'role')
	`)
	if err != nil {
		return fmt.Errorf("failed to migrate pinned to hooked: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		// Log migration for audit trail (optional - no-op if table doesn't exist)
		_, _ = db.Exec(`
			INSERT INTO events (issue_id, event_type, actor, old_value, new_value, comment)
			SELECT id, 'migration', 'system', 'pinned', 'hooked', 'bd-s00m: Semantic split of pinned vs hooked'
			FROM issues
			WHERE status = 'hooked'
			  AND issue_type NOT IN ('agent', 'role')
		`)
	}

	return nil
}
