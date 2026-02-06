package migrations

import (
	"database/sql"
)

// MigrateDropReadyIssuesView drops the ready_issues VIEW which is no longer needed.
// GetReadyWork now uses the blocked_issues_cache table for O(1) lookups instead of
// the expensive recursive CTE that the VIEW executed on every query. (bd-b2ts)
func MigrateDropReadyIssuesView(db *sql.DB) error {
	_, err := db.Exec(`DROP VIEW IF EXISTS ready_issues`)
	// DROP VIEW IF EXISTS is idempotent - no error if view doesn't exist
	return err
}
