package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateLabelMutexPolicy creates tables and a trigger to enforce label mutex
// constraints at the database level. The trigger prevents inserting a label
// that conflicts with another label already assigned to the same issue within
// the same mutex group.
//
// The trigger "fails open" — if the policy tables are empty (no groups
// registered), all label inserts succeed. Policy is populated by
// `bd doctor --fix` from the YAML config (source of truth).
func MigrateLabelMutexPolicy(db *sql.DB) error {
	// --- Tables ---

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS label_mutex_groups (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			name     TEXT    NOT NULL DEFAULT '',
			required INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create label_mutex_groups: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS label_mutex_members (
			group_id INTEGER NOT NULL,
			label    TEXT    NOT NULL,
			PRIMARY KEY (group_id, label),
			FOREIGN KEY (group_id) REFERENCES label_mutex_groups(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create label_mutex_members: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_label_mutex_members_label ON label_mutex_members(label)`)
	if err != nil {
		return fmt.Errorf("failed to create label_mutex_members index: %w", err)
	}

	// --- Trigger ---
	// Drop-and-recreate for idempotency (allows trigger logic updates on re-migration).
	_, err = db.Exec(`DROP TRIGGER IF EXISTS label_mutex_enforce_insert`)
	if err != nil {
		return fmt.Errorf("failed to drop existing trigger: %w", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER label_mutex_enforce_insert
		BEFORE INSERT ON labels
		FOR EACH ROW
		WHEN EXISTS (
			SELECT 1 FROM label_mutex_members WHERE label = NEW.label
		)
		BEGIN
			SELECT RAISE(ABORT, 'label mutex violation')
			WHERE EXISTS (
				SELECT 1
				FROM labels l
				JOIN label_mutex_members m1 ON m1.label = NEW.label
				JOIN label_mutex_members m2 ON m2.group_id = m1.group_id AND m2.label = l.label
				WHERE l.issue_id = NEW.issue_id
				  AND l.label != NEW.label
			);
		END
	`)
	if err != nil {
		return fmt.Errorf("failed to create label_mutex_enforce_insert trigger: %w", err)
	}

	return nil
}
