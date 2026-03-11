package migrations

import "database/sql"

// MigrateCommentTypeColumn adds a `type` column to comments and wisp_comments
// tables for categorizing comments (decision, handoff, note).
// Existing comments get empty string (untyped) — backwards compatible.
func MigrateCommentTypeColumn(db *sql.DB) error {
	for _, table := range []string{"comments", "wisp_comments"} {
		exists, err := columnExists(db, table, "type")
		if err != nil {
			// Table may not exist (e.g., wisp_comments on fresh installs
			// before wisp migration runs). Skip gracefully.
			if isTableNotFoundError(err) {
				continue
			}
			return err
		}
		if exists {
			continue
		}

		//nolint:gosec // G202: table name comes from hardcoded list above
		if _, err := db.Exec("ALTER TABLE `" + table + "` ADD COLUMN `type` VARCHAR(32) NOT NULL DEFAULT ''"); err != nil {
			return err
		}

		// Composite index for filtering comments by type within an issue
		idxName := "idx_" + table + "_issue_type"
		if !indexExists(db, table, idxName) {
			//nolint:gosec // G202: table/index names from internal constants
			if _, err := db.Exec("CREATE INDEX `" + idxName + "` ON `" + table + "` (issue_id, type)"); err != nil {
				return err
			}
		}
	}
	return nil
}
