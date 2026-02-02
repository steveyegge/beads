//go:build cgo

package dolt

import (
	"context"
	"testing"
)

func TestMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStore(t)
	defer cleanup()

	db := store.UnderlyingDB()

	// Run migrations twice - should be idempotent
	if err := RunMigrations(ctx, db); err != nil {
		t.Fatalf("First migration run failed: %v", err)
	}

	if err := RunMigrations(ctx, db); err != nil {
		t.Fatalf("Second migration run (idempotent check) failed: %v", err)
	}

	// Verify all expected columns exist
	expectedColumns := []string{
		"advice_target_rig",
		"advice_target_role",
		"advice_target_agent",
		"advice_hook_command",
		"advice_hook_trigger",
		"advice_hook_timeout",
		"advice_hook_on_failure",
		"advice_subscriptions",
		"advice_subscriptions_exclude",
	}

	for _, col := range expectedColumns {
		exists, err := columnExists(ctx, db, "issues", col)
		if err != nil {
			t.Errorf("Failed to check column %s: %v", col, err)
			continue
		}
		if !exists {
			t.Errorf("Expected column %s to exist after migrations", col)
		}
	}
}

func TestColumnExistsFunction(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStore(t)
	defer cleanup()

	db := store.UnderlyingDB()

	// Check a column that definitely exists
	exists, err := columnExists(ctx, db, "issues", "id")
	if err != nil {
		t.Fatalf("columnExists failed: %v", err)
	}
	if !exists {
		t.Error("Expected 'id' column to exist in issues table")
	}

	// Check a column that doesn't exist
	exists, err = columnExists(ctx, db, "issues", "nonexistent_column_xyz")
	if err != nil {
		t.Fatalf("columnExists failed for nonexistent column: %v", err)
	}
	if exists {
		t.Error("Expected nonexistent column to not exist")
	}
}
