package main

import (
	"os"
	"strings"
	"testing"
)

func TestRemoveGitattributesEntry(t *testing.T) {
	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	t.Run("removes beads entry", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to change to temp dir: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		content := `*.png binary
# Use bd merge for beads JSONL files
.beads/issues.jsonl merge=beads
*.jpg binary
`
		if err := os.WriteFile(".gitattributes", []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		if err := removeGitattributesEntry(); err != nil {
			t.Fatalf("removeGitattributesEntry failed: %v", err)
		}

		result, err := os.ReadFile(".gitattributes")
		if err != nil {
			t.Fatalf("failed to read result: %v", err)
		}

		if strings.Contains(string(result), "merge=beads") {
			t.Error("beads merge entry should have been removed")
		}
		if !strings.Contains(string(result), "*.png binary") {
			t.Error("other entries should be preserved")
		}
		if !strings.Contains(string(result), "*.jpg binary") {
			t.Error("other entries should be preserved")
		}
	})

	t.Run("removes file if only beads entry", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to change to temp dir: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		content := `.beads/issues.jsonl merge=beads
`
		if err := os.WriteFile(".gitattributes", []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		if err := removeGitattributesEntry(); err != nil {
			t.Fatalf("removeGitattributesEntry failed: %v", err)
		}

		if _, err := os.Stat(".gitattributes"); !os.IsNotExist(err) {
			t.Error("file should have been deleted when only beads entries present")
		}
	})
}
