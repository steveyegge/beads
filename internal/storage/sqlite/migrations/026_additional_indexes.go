package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateAdditionalIndexes adds performance optimization indexes identified
// during schema review (bd-h0we).
//
// Indexes added:
//   - idx_issues_updated_at: For GetStaleIssues date filtering (bd-bha9)
//   - idx_issues_status_priority: For common list query patterns (bd-a9y3)
//   - idx_labels_label_issue: Covering index for label lookups (bd-jke6)
//   - idx_dependencies_issue_type: For blocked issues queries (bd-8x3w)
//   - idx_events_issue_type: For close reason queries (bd-lk39)
func MigrateAdditionalIndexes(db *sql.DB) error {
	indexes := []struct {
		name string
		sql  string
	}{
		// Issues table indexes
		{
			name: "idx_issues_updated_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_issues_updated_at ON issues(updated_at)`,
		},
		{
			name: "idx_issues_status_priority",
			sql:  `CREATE INDEX IF NOT EXISTS idx_issues_status_priority ON issues(status, priority)`,
		},

		// Labels table covering index
		{
			name: "idx_labels_label_issue",
			sql:  `CREATE INDEX IF NOT EXISTS idx_labels_label_issue ON labels(label, issue_id)`,
		},

		// Dependencies table composite index
		{
			name: "idx_dependencies_issue_type",
			sql:  `CREATE INDEX IF NOT EXISTS idx_dependencies_issue_type ON dependencies(issue_id, type)`,
		},

		// Events table composite index
		{
			name: "idx_events_issue_type",
			sql:  `CREATE INDEX IF NOT EXISTS idx_events_issue_type ON events(issue_id, event_type)`,
		},
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx.sql); err != nil {
			return fmt.Errorf("failed to create index %s: %w", idx.name, err)
		}
	}

	return nil
}
