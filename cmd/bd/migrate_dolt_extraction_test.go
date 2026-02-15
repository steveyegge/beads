//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFromSQLite_OpensSQLiteFile(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "beads.db")
	store := newTestStore(t, dbPath)
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close sqlite store: %v", err)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("failed to stat sqlite database: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("expected sqlite database file, got directory: %s", dbPath)
	}

	data, err := extractFromSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("extractFromSQLite should open sqlite file successfully: %v", err)
	}
	if data == nil {
		t.Fatal("extractFromSQLite returned nil data")
	}
}
