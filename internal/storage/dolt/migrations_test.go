package dolt

import (
	"context"
	"os"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestMigrateMetadataColumnOnNewDB verifies that the migration works on
// a fresh database (column already exists from schema creation).
func TestMigrateMetadataColumnOnNewDB(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-migration-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Verify the metadata column exists
	exists, err := columnExists(ctx, store.db, "issues", "metadata")
	if err != nil {
		t.Fatalf("failed to check column existence: %v", err)
	}
	if !exists {
		t.Error("expected metadata column to exist on new database")
	}
}

// TestMigrateMetadataColumnIsIdempotent verifies that running the migration
// multiple times is safe and doesn't produce errors.
func TestMigrateMetadataColumnIsIdempotent(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-migration-idempotent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Run migration multiple times - should not error
	for i := 0; i < 3; i++ {
		if err := migrateMetadataColumn(ctx, store.db); err != nil {
			t.Fatalf("migration run %d failed: %v", i+1, err)
		}
	}

	// Verify the column still exists
	exists, err := columnExists(ctx, store.db, "issues", "metadata")
	if err != nil {
		t.Fatalf("failed to check column existence: %v", err)
	}
	if !exists {
		t.Error("expected metadata column to exist after idempotent migrations")
	}
}

// TestMigrateMetadataColumnWorksOnExistingDB simulates an existing database
// without the metadata column by dropping it and running the migration.
func TestMigrateMetadataColumnWorksOnExistingDB(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-migration-existing-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Drop the metadata column to simulate an existing database without it
	_, err = store.db.ExecContext(ctx, "ALTER TABLE issues DROP COLUMN metadata")
	if err != nil {
		t.Fatalf("failed to drop metadata column: %v", err)
	}

	// Verify column is gone
	exists, err := columnExists(ctx, store.db, "issues", "metadata")
	if err != nil {
		t.Fatalf("failed to check column existence: %v", err)
	}
	if exists {
		t.Fatal("metadata column should have been dropped")
	}

	// Run migration
	if err := migrateMetadataColumn(ctx, store.db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Verify column now exists
	exists, err = columnExists(ctx, store.db, "issues", "metadata")
	if err != nil {
		t.Fatalf("failed to check column existence: %v", err)
	}
	if !exists {
		t.Error("expected metadata column to exist after migration")
	}
}

// TestMetadataReadWriteAfterMigration verifies that metadata can be read
// and written correctly after running the migration.
func TestMetadataReadWriteAfterMigration(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-migration-rw-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Set prefix for issue creation
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Drop and re-add the metadata column to simulate migration
	_, err = store.db.ExecContext(ctx, "ALTER TABLE issues DROP COLUMN metadata")
	if err != nil {
		t.Fatalf("failed to drop metadata column: %v", err)
	}
	if err := migrateMetadataColumn(ctx, store.db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Create an issue with metadata
	issue := &types.Issue{
		Title:       "Test Issue with Metadata",
		Description: "Testing metadata after migration",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		Metadata:    []byte(`{"custom_field":"value123","count":42}`),
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Retrieve the issue and verify metadata
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected to retrieve issue")
	}

	expectedMetadata := `{"custom_field":"value123","count":42}`
	if string(retrieved.Metadata) != expectedMetadata {
		t.Errorf("expected metadata %q, got %q", expectedMetadata, string(retrieved.Metadata))
	}
}

// TestApplyMigrationsIsIdempotent verifies that the applyMigrations function
// can be called multiple times without errors.
func TestApplyMigrationsIsIdempotent(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-migrations-all-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Run applyMigrations multiple times
	for i := 0; i < 3; i++ {
		if err := applyMigrations(ctx, store.db); err != nil {
			t.Fatalf("applyMigrations run %d failed: %v", i+1, err)
		}
	}
}

// TestColumnExistsFunction tests the columnExists helper function.
func TestColumnExistsFunction(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-column-exists-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Test existing columns
	testCases := []struct {
		column string
		exists bool
	}{
		{"id", true},
		{"title", true},
		{"status", true},
		{"metadata", true},
		{"nonexistent_column", false},
		{"another_missing", false},
	}

	for _, tc := range testCases {
		exists, err := columnExists(ctx, store.db, "issues", tc.column)
		if err != nil {
			t.Errorf("columnExists(%q) error: %v", tc.column, err)
			continue
		}
		if exists != tc.exists {
			t.Errorf("columnExists(%q) = %v, want %v", tc.column, exists, tc.exists)
		}
	}
}
