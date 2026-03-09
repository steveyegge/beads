package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateMetadataToLocalState removes machine-local metadata keys from the
// Dolt-versioned metadata table. These keys (dolt_auto_push_* and tip_*_last_shown)
// differ per clone and cause merge conflicts when multiple machines push/pull
// the same remote.
//
// After this migration, these values are stored in .beads/local-state.json
// which is gitignored and never enters Dolt history. See GH#2466.
func MigrateMetadataToLocalState(db *sql.DB) error {
	// Delete auto-push tracking keys
	for _, key := range []string{"dolt_auto_push_last", "dolt_auto_push_commit"} {
		_, err := db.Exec("DELETE FROM metadata WHERE `key` = ?", key)
		if err != nil {
			return fmt.Errorf("failed to delete metadata key %q: %w", key, err)
		}
	}

	// Delete tip-shown tracking keys (pattern: tip_*_last_shown)
	_, err := db.Exec("DELETE FROM metadata WHERE `key` LIKE 'tip_%_last_shown'")
	if err != nil {
		return fmt.Errorf("failed to delete tip metadata keys: %w", err)
	}

	return nil
}
