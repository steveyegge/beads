//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

func TestImportFromLocalJSONL(t *testing.T) {
	skipIfNoDolt(t)

	t.Run("imports issues from JSONL file", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		// Create a JSONL file with test issues
		jsonlContent := `{"id":"test-abc123","title":"First issue","type":"bug","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
{"id":"test-def456","title":"Second issue","type":"task","status":"open","priority":3,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		count, err := importFromLocalJSONL(ctx, store, jsonlPath)
		if err != nil {
			t.Fatalf("importFromLocalJSONL failed: %v", err)
		}

		if count != 2 {
			t.Errorf("Expected 2 issues imported, got %d", count)
		}

		// Verify issues exist in the store
		issue1, err := store.GetIssue(ctx, "test-abc123")
		if err != nil {
			t.Fatalf("Failed to get first issue: %v", err)
		}
		if issue1.Title != "First issue" {
			t.Errorf("Expected title 'First issue', got %q", issue1.Title)
		}

		issue2, err := store.GetIssue(ctx, "test-def456")
		if err != nil {
			t.Fatalf("Failed to get second issue: %v", err)
		}
		if issue2.Title != "Second issue" {
			t.Errorf("Expected title 'Second issue', got %q", issue2.Title)
		}
	})

	t.Run("empty JSONL file imports zero issues", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(""), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		count, err := importFromLocalJSONL(ctx, store, jsonlPath)
		if err != nil {
			t.Fatalf("importFromLocalJSONL failed: %v", err)
		}

		if count != 0 {
			t.Errorf("Expected 0 issues imported from empty file, got %d", count)
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		ctx := context.Background()
		_, err := importFromLocalJSONL(ctx, store, "/nonexistent/issues.jsonl")
		if err == nil {
			t.Error("Expected error for nonexistent file, got nil")
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte("not valid json\n"), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		_, err := importFromLocalJSONL(ctx, store, jsonlPath)
		if err == nil {
			t.Error("Expected error for invalid JSON, got nil")
		}
	})

	t.Run("re-import with duplicate IDs succeeds via upsert", func(t *testing.T) {
		// GH#2061: importing the same JSONL twice should not fail with
		// "duplicate primary key" — the second import should upsert.
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlContent := `{"id":"test-dup1","title":"Original title","type":"bug","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
{"id":"test-dup2","title":"Second issue","type":"task","status":"open","priority":3,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()

		// First import
		count, err := importFromLocalJSONL(ctx, store, jsonlPath)
		if err != nil {
			t.Fatalf("first importFromLocalJSONL failed: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 issues imported on first import, got %d", count)
		}

		// Second import with same IDs — should succeed (upsert), not fail
		updatedContent := `{"id":"test-dup1","title":"Updated title","type":"bug","status":"closed","priority":1,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-06-01T00:00:00Z"}
{"id":"test-dup2","title":"Second issue","type":"task","status":"open","priority":3,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
`
		if err := os.WriteFile(jsonlPath, []byte(updatedContent), 0644); err != nil {
			t.Fatalf("Failed to write updated JSONL file: %v", err)
		}

		count2, err := importFromLocalJSONL(ctx, store, jsonlPath)
		if err != nil {
			t.Fatalf("second importFromLocalJSONL failed (duplicate key?): %v", err)
		}
		if count2 != 2 {
			t.Errorf("Expected 2 issues on re-import, got %d", count2)
		}

		// Verify the first issue was updated (upsert, not just inserted)
		issue, err := store.GetIssue(ctx, "test-dup1")
		if err != nil {
			t.Fatalf("Failed to get upserted issue: %v", err)
		}
		if issue.Title != "Updated title" {
			t.Errorf("Expected title 'Updated title' after upsert, got %q", issue.Title)
		}
		if issue.Status != "closed" {
			t.Errorf("Expected status 'closed' after upsert, got %q", issue.Status)
		}
	})

	t.Run("child counter reconciled after JSONL import prevents overwrites", func(t *testing.T) {
		// Regression test for GH#2166: bd create --parent after bd init --from-jsonl
		// must not overwrite existing child issues. The child_counters table
		// must be reconciled from imported hierarchical IDs.
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		// Import an epic with two existing children via JSONL
		jsonlContent := `{"id":"test-epic1","title":"Epic","type":"epic","status":"open","priority":1,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
{"id":"test-epic1.1","title":"Child 1","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
{"id":"test-epic1.2","title":"Child 2","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		count, err := importFromLocalJSONL(ctx, store, jsonlPath)
		if err != nil {
			t.Fatalf("importFromLocalJSONL failed: %v", err)
		}
		if count != 3 {
			t.Errorf("Expected 3 issues imported, got %d", count)
		}

		// Now request the next child ID for the epic — this MUST be .3, not .1
		nextID, err := store.GetNextChildID(ctx, "test-epic1")
		if err != nil {
			t.Fatalf("GetNextChildID failed: %v", err)
		}
		if nextID != "test-epic1.3" {
			t.Errorf("Expected next child ID 'test-epic1.3', got %q (would overwrite existing child!)", nextID)
		}

		// Verify original children are still intact
		child1, err := store.GetIssue(ctx, "test-epic1.1")
		if err != nil {
			t.Fatalf("Failed to get child 1: %v", err)
		}
		if child1.Title != "Child 1" {
			t.Errorf("Child 1 title changed unexpectedly: got %q", child1.Title)
		}
		child2, err := store.GetIssue(ctx, "test-epic1.2")
		if err != nil {
			t.Fatalf("Failed to get child 2: %v", err)
		}
		if child2.Title != "Child 2" {
			t.Errorf("Child 2 title changed unexpectedly: got %q", child2.Title)
		}
	})

	t.Run("sets prefix from first issue when not configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStoreWithPrefix(t, dbPath, "") // Empty prefix

		jsonlContent := `{"id":"myprefix-abc123","title":"Test issue","type":"bug","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		// Clear any existing prefix
		_ = store.SetConfig(ctx, "issue_prefix", "")

		count, err := importFromLocalJSONL(ctx, store, jsonlPath)
		if err != nil {
			t.Fatalf("importFromLocalJSONL failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 issue imported, got %d", count)
		}

		// Verify prefix was auto-detected
		prefix, err := store.GetConfig(ctx, "issue_prefix")
		if err != nil {
			t.Fatalf("Failed to get issue_prefix: %v", err)
		}
		if prefix != "myprefix" {
			t.Errorf("Expected auto-detected prefix 'myprefix', got %q", prefix)
		}
	})

	t.Run("imports export JSONL with labels dependencies comments and metadata", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlContent := `{"id":"test-rel-1","title":"Issue with relations","type":"feature","status":"open","priority":1,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","metadata":{"source":"import-test"},"labels":["zeta","alpha"],"dependencies":[{"issue_id":"test-rel-1","depends_on_id":"test-rel-2","type":"blocks","created_at":"2025-01-01T00:00:00Z","created_by":"tester"}],"comments":[{"issue_id":"test-rel-1","author":"alice","text":"first comment","created_at":"2025-01-01T01:00:00Z"}],"dependency_count":1,"dependent_count":0,"comment_count":1}
{"id":"test-rel-2","title":"Dependency target","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","labels":["backend"]}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		result, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, ImportOptions{
			Mode:                 importModeUpsert,
			OrphanHandling:       string(storage.OrphanAllow),
			IncludeWisps:         true,
			SkipPrefixValidation: true,
		})
		if err != nil {
			t.Fatalf("importFromLocalJSONLWithOptions failed: %v", err)
		}
		if result.Created != 2 {
			t.Fatalf("expected 2 created issues, got %+v", result)
		}

		labels, err := store.GetLabels(ctx, "test-rel-1")
		if err != nil {
			t.Fatalf("GetLabels failed: %v", err)
		}
		if len(labels) != 2 {
			t.Fatalf("expected 2 labels, got %d (%v)", len(labels), labels)
		}

		deps, err := store.GetDependencyRecords(ctx, "test-rel-1")
		if err != nil {
			t.Fatalf("GetDependencyRecords failed: %v", err)
		}
		if len(deps) != 1 || deps[0].DependsOnID != "test-rel-2" || deps[0].Type != "blocks" {
			t.Fatalf("unexpected dependency records: %+v", deps)
		}

		comments, err := store.GetIssueComments(ctx, "test-rel-1")
		if err != nil {
			t.Fatalf("GetIssueComments failed: %v", err)
		}
		if len(comments) != 1 || comments[0].Text != "first comment" {
			t.Fatalf("unexpected comments: %+v", comments)
		}

		issue, err := store.GetIssue(ctx, "test-rel-1")
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if !json.Valid(issue.Metadata) {
			t.Fatalf("expected valid metadata JSON, got: %s", string(issue.Metadata))
		}
		if !strings.Contains(string(issue.Metadata), `"source":"import-test"`) {
			t.Fatalf("unexpected metadata payload: %s", string(issue.Metadata))
		}
	})

	t.Run("strict mode fails when issue already exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlContent := `{"id":"test-strict-1","title":"Strict import issue","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		if _, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, ImportOptions{
			Mode:                 importModeUpsert,
			OrphanHandling:       string(storage.OrphanAllow),
			IncludeWisps:         true,
			SkipPrefixValidation: true,
		}); err != nil {
			t.Fatalf("initial upsert import failed: %v", err)
		}

		_, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, ImportOptions{
			Mode:                 importModeStrict,
			OrphanHandling:       string(storage.OrphanAllow),
			IncludeWisps:         true,
			SkipPrefixValidation: true,
		})
		if err == nil {
			t.Fatal("expected strict mode to fail on existing issue id")
		}
		if !strings.Contains(err.Error(), "strict mode") {
			t.Fatalf("expected strict mode error, got: %v", err)
		}
	})

	t.Run("invalid JSON reports line number", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlContent := `{"id":"test-line-1","title":"First","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
not valid json
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		_, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, ImportOptions{
			Mode:                 importModeUpsert,
			OrphanHandling:       string(storage.OrphanAllow),
			IncludeWisps:         true,
			SkipPrefixValidation: true,
		})
		if err == nil {
			t.Fatal("expected parse error for invalid json line")
		}
		if !strings.Contains(err.Error(), "line 2") {
			t.Fatalf("expected error to include line number, got: %v", err)
		}
	})

	t.Run("upsert import is idempotent for comments", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlContent := `{"id":"test-idem-1","title":"Idempotent issue","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","comments":[{"issue_id":"test-idem-1","author":"alice","text":"hello","created_at":"2025-01-01T01:00:00Z"}]}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		opts := ImportOptions{
			Mode:                 importModeUpsert,
			OrphanHandling:       string(storage.OrphanAllow),
			IncludeWisps:         true,
			SkipPrefixValidation: true,
		}
		if _, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, opts); err != nil {
			t.Fatalf("first import failed: %v", err)
		}
		if _, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, opts); err != nil {
			t.Fatalf("second import failed: %v", err)
		}

		comments, err := store.GetIssueComments(ctx, "test-idem-1")
		if err != nil {
			t.Fatalf("GetIssueComments failed: %v", err)
		}
		if len(comments) != 1 {
			t.Fatalf("expected exactly 1 comment after idempotent upsert, got %d", len(comments))
		}
	})

	t.Run("orphan strict fails missing dependency target", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlContent := `{"id":"test-orphan-1","title":"Orphan dependency","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","dependencies":[{"issue_id":"test-orphan-1","depends_on_id":"missing-target","type":"blocks","created_at":"2025-01-01T00:00:00Z"}]}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		_, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, ImportOptions{
			Mode:                 importModeUpsert,
			OrphanHandling:       string(storage.OrphanStrict),
			IncludeWisps:         true,
			SkipPrefixValidation: true,
		})
		if err == nil {
			t.Fatal("expected orphan strict mode to fail on missing dependency target")
		}
		if !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("expected missing target error, got: %v", err)
		}
	})

	t.Run("include-wisps=false skips ephemeral records", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "dolt")
		store := newTestStore(t, dbPath)

		jsonlContent := `{"id":"test-wisp-1","title":"Ephemeral issue","type":"task","status":"open","priority":2,"ephemeral":true,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
{"id":"test-main-1","title":"Persistent issue","type":"task","status":"open","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}
`
		jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(jsonlContent), 0644); err != nil {
			t.Fatalf("Failed to write JSONL file: %v", err)
		}

		ctx := context.Background()
		result, err := importFromLocalJSONLWithOptions(ctx, store, jsonlPath, ImportOptions{
			Mode:                 importModeUpsert,
			OrphanHandling:       string(storage.OrphanAllow),
			IncludeWisps:         false,
			SkipPrefixValidation: true,
		})
		if err != nil {
			t.Fatalf("import failed: %v", err)
		}
		if result.Created != 1 || result.Skipped != 1 {
			t.Fatalf("expected 1 created and 1 skipped, got %+v", result)
		}

		if _, err := store.GetIssue(ctx, "test-main-1"); err != nil {
			t.Fatalf("expected persistent issue to be imported: %v", err)
		}
		if _, err := store.GetIssue(ctx, "test-wisp-1"); err == nil {
			t.Fatalf("expected ephemeral issue to be skipped")
		}
	})
}
