//go:build cgo
package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateLabelMutexPolicy creates tables and a trigger to enforce label mutex
// constraints at the database level (MySQL/Dolt dialect).
//
// The trigger "fails open" — if the policy tables are empty, all label inserts
// succeed. Policy is populated by `bd doctor --fix` from YAML config.
func MigrateLabelMutexPolicy(db *sql.DB) error {
	// --- Tables ---

	exists, err := tableExists(db, "label_mutex_groups")
	if err != nil {
		return fmt.Errorf("failed to check label_mutex_groups: %w", err)
	}
	if !exists {
		_, err = db.Exec(`
			CREATE TABLE label_mutex_groups (
				id       BIGINT AUTO_INCREMENT PRIMARY KEY,
				name     VARCHAR(255) NOT NULL DEFAULT '',
				required TINYINT(1)   NOT NULL DEFAULT 0
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create label_mutex_groups: %w", err)
		}
	}

	exists, err = tableExists(db, "label_mutex_members")
	if err != nil {
		return fmt.Errorf("failed to check label_mutex_members: %w", err)
	}
	if !exists {
		_, err = db.Exec(`
			CREATE TABLE label_mutex_members (
				group_id BIGINT       NOT NULL,
				label    VARCHAR(255) NOT NULL,
				PRIMARY KEY (group_id, label),
				FOREIGN KEY (group_id) REFERENCES label_mutex_groups(id) ON DELETE CASCADE
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create label_mutex_members: %w", err)
		}

		_, err = db.Exec(`CREATE INDEX idx_label_mutex_members_label ON label_mutex_members(label)`)
		if err != nil {
			return fmt.Errorf("failed to create label_mutex_members index: %w", err)
		}
	}

	// --- Trigger ---
	// Drop-and-recreate for idempotency.
	_, err = db.Exec(`DROP TRIGGER IF EXISTS label_mutex_enforce_insert`)
	if err != nil {
		return fmt.Errorf("failed to drop existing trigger: %w", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER label_mutex_enforce_insert
		BEFORE INSERT ON labels
		FOR EACH ROW
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM labels l
				JOIN label_mutex_members m1 ON m1.label = NEW.label
				JOIN label_mutex_members m2 ON m2.group_id = m1.group_id AND m2.label = l.label
				WHERE l.issue_id = NEW.issue_id
				  AND l.label != NEW.label
			) THEN
				SIGNAL SQLSTATE '45000'
					SET MESSAGE_TEXT = 'label mutex violation';
			END IF;
		END
	`)
	if err != nil {
		return fmt.Errorf("failed to create label_mutex_enforce_insert trigger: %w", err)
	}

	return nil
}
