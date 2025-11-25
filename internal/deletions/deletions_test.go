package deletions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDeletions_Empty(t *testing.T) {
	// Non-existent file should return empty result
	result, err := LoadDeletions("/nonexistent/path/deletions.jsonl")
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}
	if len(result.Records) != 0 {
		t.Errorf("expected empty map, got %d records", len(result.Records))
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d", len(result.Warnings))
	}
}

func TestRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	// Create test records
	now := time.Now().Truncate(time.Millisecond) // Truncate for JSON round-trip
	record1 := DeletionRecord{
		ID:        "bd-123",
		Timestamp: now,
		Actor:     "testuser",
		Reason:    "duplicate",
	}
	record2 := DeletionRecord{
		ID:        "bd-456",
		Timestamp: now.Add(time.Hour),
		Actor:     "testuser",
	}

	// Append records
	if err := AppendDeletion(path, record1); err != nil {
		t.Fatalf("AppendDeletion failed: %v", err)
	}
	if err := AppendDeletion(path, record2); err != nil {
		t.Fatalf("AppendDeletion failed: %v", err)
	}

	// Load and verify
	result, err := LoadDeletions(path)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(result.Records))
	}

	// Verify record1
	r1, ok := result.Records["bd-123"]
	if !ok {
		t.Fatal("record bd-123 not found")
	}
	if r1.Actor != "testuser" {
		t.Errorf("expected actor 'testuser', got '%s'", r1.Actor)
	}
	if r1.Reason != "duplicate" {
		t.Errorf("expected reason 'duplicate', got '%s'", r1.Reason)
	}

	// Verify record2
	r2, ok := result.Records["bd-456"]
	if !ok {
		t.Fatal("record bd-456 not found")
	}
	if r2.Reason != "" {
		t.Errorf("expected empty reason, got '%s'", r2.Reason)
	}
}

func TestLoadDeletions_CorruptLines(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	// Write mixed valid and corrupt content
	content := `{"id":"bd-001","ts":"2024-01-01T00:00:00Z","by":"user1"}
this is not valid json
{"id":"bd-002","ts":"2024-01-02T00:00:00Z","by":"user2"}
{"broken json
{"id":"bd-003","ts":"2024-01-03T00:00:00Z","by":"user3","reason":"test"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := LoadDeletions(path)
	if err != nil {
		t.Fatalf("LoadDeletions should not fail on corrupt lines: %v", err)
	}
	if result.Skipped != 2 {
		t.Errorf("expected 2 skipped lines, got %d", result.Skipped)
	}
	if len(result.Records) != 3 {
		t.Errorf("expected 3 valid records, got %d", len(result.Records))
	}
	if len(result.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(result.Warnings))
	}

	// Verify valid records were loaded
	for _, id := range []string{"bd-001", "bd-002", "bd-003"} {
		if _, ok := result.Records[id]; !ok {
			t.Errorf("expected record %s to be loaded", id)
		}
	}
}

func TestLoadDeletions_MissingID(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	// Write record without ID
	content := `{"id":"bd-001","ts":"2024-01-01T00:00:00Z","by":"user1"}
{"ts":"2024-01-02T00:00:00Z","by":"user2"}
{"id":"","ts":"2024-01-03T00:00:00Z","by":"user3"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := LoadDeletions(path)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}
	// Two lines should be skipped: one missing "id" field, one with empty "id"
	if result.Skipped != 2 {
		t.Errorf("expected 2 skipped lines (missing/empty ID), got %d", result.Skipped)
	}
	if len(result.Records) != 1 {
		t.Errorf("expected 1 valid record, got %d", len(result.Records))
	}
}

func TestLoadDeletions_LastWriteWins(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	// Write same ID twice with different data
	content := `{"id":"bd-001","ts":"2024-01-01T00:00:00Z","by":"user1","reason":"first"}
{"id":"bd-001","ts":"2024-01-02T00:00:00Z","by":"user2","reason":"second"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := LoadDeletions(path)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}
	if len(result.Records) != 1 {
		t.Errorf("expected 1 record (deduplicated), got %d", len(result.Records))
	}

	r := result.Records["bd-001"]
	if r.Actor != "user2" {
		t.Errorf("expected last write to win (user2), got '%s'", r.Actor)
	}
	if r.Reason != "second" {
		t.Errorf("expected last reason 'second', got '%s'", r.Reason)
	}
}

func TestWriteDeletions_Atomic(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	now := time.Now().Truncate(time.Millisecond)
	records := []DeletionRecord{
		{ID: "bd-001", Timestamp: now, Actor: "user1"},
		{ID: "bd-002", Timestamp: now, Actor: "user2", Reason: "cleanup"},
	}

	if err := WriteDeletions(path, records); err != nil {
		t.Fatalf("WriteDeletions failed: %v", err)
	}

	// Verify by loading
	result, err := LoadDeletions(path)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}
	if len(result.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(result.Records))
	}
}

func TestWriteDeletions_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	now := time.Now().Truncate(time.Millisecond)

	// Write initial records
	initial := []DeletionRecord{
		{ID: "bd-001", Timestamp: now, Actor: "user1"},
		{ID: "bd-002", Timestamp: now, Actor: "user2"},
		{ID: "bd-003", Timestamp: now, Actor: "user3"},
	}
	if err := WriteDeletions(path, initial); err != nil {
		t.Fatalf("initial WriteDeletions failed: %v", err)
	}

	// Overwrite with fewer records (simulates compaction pruning)
	compacted := []DeletionRecord{
		{ID: "bd-002", Timestamp: now, Actor: "user2"},
	}
	if err := WriteDeletions(path, compacted); err != nil {
		t.Fatalf("compacted WriteDeletions failed: %v", err)
	}

	// Verify only compacted records remain
	result, err := LoadDeletions(path)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}
	if len(result.Records) != 1 {
		t.Errorf("expected 1 record after compaction, got %d", len(result.Records))
	}
	if _, ok := result.Records["bd-002"]; !ok {
		t.Error("expected bd-002 to remain after compaction")
	}
}

func TestAppendDeletion_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nested", "dir", "deletions.jsonl")

	record := DeletionRecord{
		ID:        "bd-001",
		Timestamp: time.Now(),
		Actor:     "testuser",
	}

	if err := AppendDeletion(path, record); err != nil {
		t.Fatalf("AppendDeletion should create parent directories: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist after append: %v", err)
	}
}

func TestWriteDeletions_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nested", "dir", "deletions.jsonl")

	records := []DeletionRecord{
		{ID: "bd-001", Timestamp: time.Now(), Actor: "testuser"},
	}

	if err := WriteDeletions(path, records); err != nil {
		t.Fatalf("WriteDeletions should create parent directories: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist after write: %v", err)
	}
}

func TestDefaultPath(t *testing.T) {
	path := DefaultPath("/home/user/project/.beads")
	expected := "/home/user/project/.beads/deletions.jsonl"
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestLoadDeletions_EmptyLines(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	// Write content with empty lines
	content := `{"id":"bd-001","ts":"2024-01-01T00:00:00Z","by":"user1"}

{"id":"bd-002","ts":"2024-01-02T00:00:00Z","by":"user2"}

`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := LoadDeletions(path)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}
	if result.Skipped != 0 {
		t.Errorf("empty lines should not count as skipped, got %d", result.Skipped)
	}
	if len(result.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(result.Records))
	}
}

func TestAppendDeletion_EmptyID(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deletions.jsonl")

	record := DeletionRecord{
		ID:        "",
		Timestamp: time.Now(),
		Actor:     "testuser",
	}

	err := AppendDeletion(path, record)
	if err == nil {
		t.Fatal("AppendDeletion should fail with empty ID")
	}
	if err.Error() != "cannot append deletion record: ID is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}
