package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateSpecRegistryLifecycle adds lifecycle metadata columns to spec_registry.
func MigrateSpecRegistryLifecycle(db *sql.DB) error {
	requiredColumns := map[string]string{
		"lifecycle":      "TEXT DEFAULT 'active'",
		"completed_at":   "DATETIME",
		"summary":        "TEXT DEFAULT ''",
		"summary_tokens": "INTEGER DEFAULT 0",
		"archived_at":    "DATETIME",
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

	return nil
}
