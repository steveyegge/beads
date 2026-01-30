package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateDecisionParentBead adds parent_bead_id column to decision_points table.
// This stores the parent bead (epic/molecule) that a decision belongs to,
// enabling epic-based routing for notifications (e.g., Slack channels).
func MigrateDecisionParentBead(db *sql.DB) error {
	// Check if column already exists
	var columnExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('decision_points')
		WHERE name = 'parent_bead_id'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check for parent_bead_id column: %w", err)
	}

	if columnExists {
		return nil
	}

	// Add the column
	_, err = db.Exec(`ALTER TABLE decision_points ADD COLUMN parent_bead_id TEXT`)
	if err != nil {
		return fmt.Errorf("failed to add parent_bead_id column: %w", err)
	}

	// Add index for efficient lookups by parent
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_decision_points_parent ON decision_points(parent_bead_id)`)
	if err != nil {
		return fmt.Errorf("failed to create parent_bead_id index: %w", err)
	}

	return nil
}
