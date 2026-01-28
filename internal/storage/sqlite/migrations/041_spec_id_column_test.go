package migrations

import (
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func TestMigrateSpecIDColumn(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create minimal issues table without spec_id
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	// Run migration
	if err := MigrateSpecIDColumn(db); err != nil {
		t.Fatalf("MigrateSpecIDColumn failed: %v", err)
	}

	// Verify column exists
	var hasSpecID bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('issues')
		WHERE name = 'spec_id'
	`).Scan(&hasSpecID)
	if err != nil {
		t.Fatalf("failed to check spec_id column: %v", err)
	}
	if !hasSpecID {
		t.Error("spec_id column was not added")
	}

	// Verify index exists
	var indexExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_issues_spec_id'
	`).Scan(&indexExists)
	if err != nil {
		t.Fatalf("failed to check spec_id index: %v", err)
	}
	if !indexExists {
		t.Error("idx_issues_spec_id index was not created")
	}
}
