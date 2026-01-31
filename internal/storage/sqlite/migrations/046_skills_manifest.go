package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSkillsManifest adds the skills_manifest and skill_bead_links tables
// used by Shadowbook for skill drift detection.
func MigrateSkillsManifest(db *sql.DB) error {
	// Create skills_manifest table
	var manifestExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type = 'table' AND name = 'skills_manifest'
	`).Scan(&manifestExists)
	if err != nil {
		return fmt.Errorf("failed to check skills_manifest table: %w", err)
	}

	if !manifestExists {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS skills_manifest (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				source TEXT NOT NULL,
				path TEXT,
				tier TEXT NOT NULL DEFAULT 'optional',
				sha256 TEXT NOT NULL,
				bytes INTEGER,
				status TEXT DEFAULT 'active',
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				last_used_at DATETIME,
				archived_at DATETIME
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create skills_manifest table: %w", err)
		}

		// Create indexes
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_skills_status ON skills_manifest(status)`); err != nil {
			return fmt.Errorf("failed to create skills_manifest status index: %w", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_skills_tier ON skills_manifest(tier)`); err != nil {
			return fmt.Errorf("failed to create skills_manifest tier index: %w", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_skills_source ON skills_manifest(source)`); err != nil {
			return fmt.Errorf("failed to create skills_manifest source index: %w", err)
		}
	}

	// Create skill_bead_links table
	var linksExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type = 'table' AND name = 'skill_bead_links'
	`).Scan(&linksExists)
	if err != nil {
		return fmt.Errorf("failed to check skill_bead_links table: %w", err)
	}

	if !linksExists {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS skill_bead_links (
				skill_id TEXT NOT NULL,
				bead_id TEXT NOT NULL,
				linked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (skill_id, bead_id),
				FOREIGN KEY (skill_id) REFERENCES skills_manifest(id),
				FOREIGN KEY (bead_id) REFERENCES issues(id) ON DELETE CASCADE
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create skill_bead_links table: %w", err)
		}

		// Create indexes
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_skill_bead_links_bead ON skill_bead_links(bead_id)`); err != nil {
			return fmt.Errorf("failed to create skill_bead_links bead index: %w", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_skill_bead_links_skill ON skill_bead_links(skill_id)`); err != nil {
			return fmt.Errorf("failed to create skill_bead_links skill index: %w", err)
		}
	}

	return nil
}
