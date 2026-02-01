//go:build cgo

package dolt

import (
	"context"
	"database/sql"
	"strings"
)

// applyMigrations applies schema migrations for existing databases.
// This is called during schema initialization and handles adding new columns
// that may be missing from databases created with older schema versions.
// All migrations are idempotent (safe to run multiple times).
func applyMigrations(ctx context.Context, db *sql.DB) error {
	// Migration: Add metadata column to issues table if missing (GH#1414)
	if err := migrateMetadataColumn(ctx, db); err != nil {
		return err
	}

	return nil
}

// migrateMetadataColumn checks if the metadata column exists in the issues table
// and adds it if missing. This handles existing databases created before the
// metadata column was added (GH#1406).
//
// The migration is idempotent: running it multiple times has no effect if the
// column already exists.
func migrateMetadataColumn(ctx context.Context, db *sql.DB) error {
	// Check if the metadata column exists using SHOW COLUMNS
	exists, err := columnExists(ctx, db, "issues", "metadata")
	if err != nil {
		return err
	}

	if exists {
		// Column already exists, nothing to do
		return nil
	}

	// Add the metadata column with the same definition as in schema.go
	_, err = db.ExecContext(ctx,
		"ALTER TABLE issues ADD COLUMN metadata JSON DEFAULT (JSON_OBJECT())")
	if err != nil {
		// Check if the error is because column already exists (race condition protection)
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "duplicate column") ||
			strings.Contains(errLower, "already exists") {
			return nil
		}
		return err
	}

	return nil
}

// columnExists checks if a column exists in a table using SHOW COLUMNS.
// This works in both embedded and server modes for Dolt/MySQL.
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	// #nosec G202 -- table and column names are from internal code, not user input
	// Note: SHOW COLUMNS ... LIKE doesn't support parameterized queries in Dolt,
	// so we use string formatting. The values are internal constants, not user input.
	query := "SHOW COLUMNS FROM " + table + " LIKE '" + column + "'"
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	// If there's at least one row, the column exists
	return rows.Next(), rows.Err()
}
