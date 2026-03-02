package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckDatabaseFreshness(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}
	dbPath := filepath.Join(beadsDir, "beads.db")
	s := newTestStore(t, dbPath)
	ctx := context.Background()

	t.Run("no_jsonl_file", func(t *testing.T) {
		// No issues.jsonl → should skip check (no error)
		err := checkDatabaseFreshness(ctx, s, beadsDir)
		if err != nil {
			t.Errorf("expected no error when JSONL missing, got: %v", err)
		}
	})

	t.Run("no_last_import_time", func(t *testing.T) {
		// Create JSONL file but no last_import_time metadata → skip check
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644); err != nil {
			t.Fatalf("failed to create JSONL: %v", err)
		}
		defer os.Remove(jsonlPath)

		err := checkDatabaseFreshness(ctx, s, beadsDir)
		if err != nil {
			t.Errorf("expected no error when last_import_time missing, got: %v", err)
		}
	})

	t.Run("database_is_fresh", func(t *testing.T) {
		// Set last_import_time to future → database is fresh
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644); err != nil {
			t.Fatalf("failed to create JSONL: %v", err)
		}
		defer os.Remove(jsonlPath)

		futureTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
		if err := s.SetMetadata(ctx, "last_import_time", futureTime); err != nil {
			t.Fatalf("failed to set metadata: %v", err)
		}

		err := checkDatabaseFreshness(ctx, s, beadsDir)
		if err != nil {
			t.Errorf("expected no error when database is fresh, got: %v", err)
		}
	})

	t.Run("database_is_stale", func(t *testing.T) {
		// Set last_import_time to past → database is stale
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644); err != nil {
			t.Fatalf("failed to create JSONL: %v", err)
		}
		defer os.Remove(jsonlPath)

		pastTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		if err := s.SetMetadata(ctx, "last_import_time", pastTime); err != nil {
			t.Fatalf("failed to set metadata: %v", err)
		}

		err := checkDatabaseFreshness(ctx, s, beadsDir)
		if err == nil {
			t.Error("expected error when database is stale, got nil")
		}
	})

	t.Run("corrupted_last_import_time", func(t *testing.T) {
		// Set corrupted last_import_time → should warn but not error
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644); err != nil {
			t.Fatalf("failed to create JSONL: %v", err)
		}
		defer os.Remove(jsonlPath)

		if err := s.SetMetadata(ctx, "last_import_time", "not-a-timestamp"); err != nil {
			t.Fatalf("failed to set metadata: %v", err)
		}

		err := checkDatabaseFreshness(ctx, s, beadsDir)
		if err != nil {
			t.Errorf("expected no error with corrupted metadata (warn only), got: %v", err)
		}
	})

	t.Run("rfc3339nano_format", func(t *testing.T) {
		// Set last_import_time with nanosecond precision → should parse correctly
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644); err != nil {
			t.Fatalf("failed to create JSONL: %v", err)
		}
		defer os.Remove(jsonlPath)

		futureTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339Nano)
		if err := s.SetMetadata(ctx, "last_import_time", futureTime); err != nil {
			t.Fatalf("failed to set metadata: %v", err)
		}

		err := checkDatabaseFreshness(ctx, s, beadsDir)
		if err != nil {
			t.Errorf("expected no error with RFC3339Nano format, got: %v", err)
		}
	})
}

func TestRefreshLastImportTime(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}
	dbPath := filepath.Join(beadsDir, "beads.db")
	s := newTestStore(t, dbPath)
	ctx := context.Background()

	t.Run("updates_last_import_time_to_now", func(t *testing.T) {
		// Create JSONL file so there's something to compare against
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644); err != nil {
			t.Fatalf("failed to create JSONL: %v", err)
		}
		defer os.Remove(jsonlPath)

		// Set last_import_time to past — DB appears stale
		pastTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		if err := s.SetMetadata(ctx, "last_import_time", pastTime); err != nil {
			t.Fatalf("failed to set metadata: %v", err)
		}

		// Verify it IS stale before refresh
		if err := checkDatabaseFreshness(ctx, s, beadsDir); err == nil {
			t.Fatal("expected staleness error before refresh, got nil")
		}

		// Call refreshLastImportTime — should update metadata
		refreshLastImportTime(ctx, s, beadsDir)

		// Now freshness check should pass
		if err := checkDatabaseFreshness(ctx, s, beadsDir); err != nil {
			t.Errorf("expected no error after refresh, got: %v", err)
		}
	})

	t.Run("survives_git_merge_touching_jsonl", func(t *testing.T) {
		// Simulate: write command runs (refresh), then git merge touches JSONL mtime
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644); err != nil {
			t.Fatalf("failed to create JSONL: %v", err)
		}
		defer os.Remove(jsonlPath)

		// Refresh sets last_import_time to now
		refreshLastImportTime(ctx, s, beadsDir)

		// Simulate git merge by touching JSONL with a future mtime
		futureTime := time.Now().Add(5 * time.Second)
		if err := os.Chtimes(jsonlPath, futureTime, futureTime); err != nil {
			t.Fatalf("failed to set future mtime: %v", err)
		}

		// This SHOULD still fail — the JSONL was genuinely modified after our refresh
		if err := checkDatabaseFreshness(ctx, s, beadsDir); err == nil {
			t.Error("expected staleness error when JSONL was touched after refresh")
		}
	})

	t.Run("no_jsonl_file_is_noop", func(t *testing.T) {
		// If there's no JSONL file, refresh should not panic or error
		noJsonlDir := filepath.Join(t.TempDir(), ".beads")
		if err := os.MkdirAll(noJsonlDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		// Should not panic
		refreshLastImportTime(ctx, s, noJsonlDir)
	})
}

func TestStalenessOnlyAppliesToReadOnlyCommands(t *testing.T) {
	t.Parallel()

	// Read-only commands should trigger staleness checks
	for _, cmd := range []string{"list", "show", "ready", "stats", "search", "duplicates", "blocked", "count", "graph", "comments"} {
		if !isReadOnlyCommand(cmd) {
			t.Errorf("command %q should be read-only (staleness check applies)", cmd)
		}
	}

	// Write commands should NOT be read-only (staleness check skipped)
	for _, cmd := range []string{"create", "update", "close", "delete", "edit", "dep"} {
		if isReadOnlyCommand(cmd) {
			t.Errorf("command %q should NOT be read-only (staleness check should not apply)", cmd)
		}
	}
}
