package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateDecisionPointColumns creates the decision_points table for human-in-the-loop choices.
// This is a separate table with FK to issues rather than columns on issues, because:
// 1. Decision points have their own lifecycle (can be iterated multiple times)
// 2. Many issues won't have decision points, avoiding sparse columns
// 3. prior_id creates a chain of iterations requiring self-referential structure
func MigrateDecisionPointColumns(db *sql.DB) error {
	var tableExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type='table' AND name='decision_points'
	`).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check decision_points table: %w", err)
	}

	if tableExists {
		return nil
	}

	_, err = db.Exec(`
		CREATE TABLE decision_points (
			issue_id TEXT PRIMARY KEY,
			prompt TEXT NOT NULL,
			options TEXT NOT NULL,
			default_option TEXT,
			selected_option TEXT,
			response_text TEXT,
			responded_at DATETIME,
			responded_by TEXT,
			iteration INTEGER DEFAULT 1,
			max_iterations INTEGER DEFAULT 3,
			prior_id TEXT,
			guidance TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
			FOREIGN KEY (prior_id) REFERENCES issues(id) ON DELETE SET NULL
		);
		CREATE INDEX idx_decision_points_prior ON decision_points(prior_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create decision_points table: %w", err)
	}

	return nil
}
