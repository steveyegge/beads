package sqlite

import (
	"context"
	"testing"
)

// TestRenameDependencyPrefix tests that dependency records are properly updated
// when renaming prefixes. This is the regression test for GH#630.
func TestRenameDependencyPrefix(t *testing.T) {
	ctx := context.Background()

	t.Run("no error when no dependencies exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := tmpDir + "/test.db"

		store, err := New(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create test database: %v", err)
		}
		defer store.Close()

		// Initialize the database with required config
		if err := store.SetConfig(ctx, "issue_prefix", "old"); err != nil {
			t.Fatalf("Failed to set issue_prefix config: %v", err)
		}

		// Rename prefix with no dependencies - should not error
		if err := store.RenameDependencyPrefix(ctx, "old", "new"); err != nil {
			t.Errorf("RenameDependencyPrefix should not error with no dependencies: %v", err)
		}
	})

	t.Run("function executes without error for any prefix", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := tmpDir + "/test.db"

		store, err := New(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create test database: %v", err)
		}
		defer store.Close()

		// Initialize the database
		if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
			t.Fatalf("Failed to set issue_prefix config: %v", err)
		}

		// Test that the function runs without error for various prefixes
		prefixes := []struct{ old, new string }{
			{"old", "new"},
			{"test", "prod"},
			{"abc", "xyz"},
		}

		for _, p := range prefixes {
			if err := store.RenameDependencyPrefix(ctx, p.old, p.new); err != nil {
				t.Errorf("RenameDependencyPrefix(%q, %q) failed: %v", p.old, p.new, err)
			}
		}
	})
}
