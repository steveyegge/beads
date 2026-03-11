package migrations

import "database/sql"

// MigrateMilestones creates the milestones table and adds a milestone column
// to issues and wisps tables.
func MigrateMilestones(db *sql.DB) error {
	// Create milestones table
	exists, err := tableExists(db, "milestones")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.Exec(`
			CREATE TABLE milestones (
				name VARCHAR(255) NOT NULL,
				target_date DATETIME,
				description TEXT DEFAULT '',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				created_by VARCHAR(255) NOT NULL DEFAULT '',
				PRIMARY KEY (name),
				INDEX idx_milestones_created_at (created_at)
			)
		`); err != nil {
			return err
		}
	}

	// Add milestone column to issues and wisps
	for _, table := range []string{"issues", "wisps"} {
		tblExists, err := tableExists(db, table)
		if err != nil {
			return err
		}
		if !tblExists {
			continue
		}
		colExists, err := columnExists(db, table, "milestone")
		if err != nil {
			return err
		}
		if colExists {
			continue
		}

		//nolint:gosec // G202: table name from hardcoded list
		if _, err := db.Exec("ALTER TABLE `" + table + "` ADD COLUMN milestone VARCHAR(255) NOT NULL DEFAULT ''"); err != nil {
			return err
		}

		idxName := "idx_" + table + "_milestone"
		if !indexExists(db, table, idxName) {
			//nolint:gosec // G202
			if _, err := db.Exec("CREATE INDEX `" + idxName + "` ON `" + table + "` (milestone)"); err != nil {
				return err
			}
		}
	}

	return nil
}
