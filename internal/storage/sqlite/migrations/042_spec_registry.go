package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSpecRegistryTable adds the spec_registry table used by Shadow Ledger.
func MigrateSpecRegistryTable(db *sql.DB) error {
	var tableExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type = 'table' AND name = 'spec_registry'
	`).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check spec_registry table: %w", err)
	}

	if !tableExists {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS spec_registry (
				spec_id TEXT PRIMARY KEY,
				path TEXT NOT NULL,
				title TEXT DEFAULT '',
				sha256 TEXT DEFAULT '',
				mtime DATETIME,
				git_status TEXT DEFAULT 'tracked',
				discovered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_scanned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				missing_at DATETIME
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create spec_registry table: %w", err)
		}
	}

	requiredColumns := map[string]string{
		"path":            "TEXT NOT NULL DEFAULT ''",
		"title":           "TEXT DEFAULT ''",
		"sha256":          "TEXT DEFAULT ''",
		"mtime":           "DATETIME",
		"git_status":      "TEXT DEFAULT 'tracked'",
		"discovered_at":   "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"last_scanned_at": "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"missing_at":      "DATETIME",
	}

	existing := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info('spec_registry')`)
	if err != nil {
		return fmt.Errorf("failed to inspect spec_registry columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan spec_registry columns: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read spec_registry columns: %w", err)
	}

	for col, def := range requiredColumns {
		if existing[col] {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE spec_registry ADD COLUMN %s %s`, col, def)); err != nil {
			return fmt.Errorf("failed to add spec_registry.%s column: %w", col, err)
		}
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_spec_registry_path ON spec_registry(path)`); err != nil {
		return fmt.Errorf("failed to create spec_registry index: %w", err)
	}

	return nil
}
