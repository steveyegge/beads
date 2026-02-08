package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateMetadataIndexTable creates the issue_metadata_index table for fast querying
// of metadata fields. This is Phase 1 of the Schema-Indexed Metadata architecture (GH#1589).
//
// The table acts as a cache/index over the canonical metadata JSON blob in the issues table.
// It indexes all top-level scalar values (string, int, float, bool) found in metadata.
// If the index gets out of sync, it can be rebuilt from metadata via bd doctor.
func MigrateMetadataIndexTable(db *sql.DB) error {
	// Check if table already exists
	var tableExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type='table' AND name='issue_metadata_index'
	`).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check issue_metadata_index table: %w", err)
	}

	if !tableExists {
		_, err = db.Exec(`
			CREATE TABLE issue_metadata_index (
				issue_id TEXT NOT NULL,
				key TEXT NOT NULL,
				value_text TEXT,
				value_int INTEGER,
				value_real REAL,
				PRIMARY KEY (issue_id, key),
				FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create issue_metadata_index table: %w", err)
		}
	}

	// Create indexes for fast filtering by key+value
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_meta_text ON issue_metadata_index(key, value_text)`)
	if err != nil {
		return fmt.Errorf("failed to create idx_meta_text index: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_meta_int ON issue_metadata_index(key, value_int)`)
	if err != nil {
		return fmt.Errorf("failed to create idx_meta_int index: %w", err)
	}

	// Note: Full backfill of individual keys requires JSON parsing in Go,
	// which is handled by RebuildMetadataIndex. New/updated issues will be
	// indexed automatically; existing issues get indexed on next import or
	// via 'bd doctor'.

	return nil
}
