package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindJSONLPath_NoDBMode(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(""), 0o600); err != nil {
		t.Fatalf("failed to create issues.jsonl: %v", err)
	}

	oldDBPath := dbPath
	dbPath = ""
	t.Cleanup(func() { dbPath = oldDBPath })

	t.Setenv("BEADS_DIR", "")
	t.Setenv("BEADS_JSONL", "")
	t.Chdir(tmpDir)

	got := findJSONLPath()
	if got != jsonlPath {
		t.Fatalf("findJSONLPath() = %q, want %q", got, jsonlPath)
	}
}
