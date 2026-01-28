package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSpecScanEvents adds a spec_scan_events table for risk analysis.
func MigrateSpecScanEvents(db *sql.DB) error {
	var table string
	if err := db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name = 'spec_scan_events'
	`).Scan(&table); err == nil && table == "spec_scan_events" {
		return nil
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS spec_scan_events (
			spec_id TEXT NOT NULL,
			scanned_at DATETIME NOT NULL,
			sha256 TEXT NOT NULL,
			changed INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (spec_id, scanned_at)
		);
	`); err != nil {
		return fmt.Errorf("failed to create spec_scan_events table: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_spec_scan_events_spec_time ON spec_scan_events(spec_id, scanned_at)`); err != nil {
		return fmt.Errorf("failed to create spec_scan_events index: %w", err)
	}

	return nil
}
