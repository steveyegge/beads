package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCodeChecks(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cmd", "bd", "pacman.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("assignCmd"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	checks := []codeCheck{{
		ID:          "assign",
		Description: "assign command exists",
		File:        "cmd/bd/pacman.go",
		Contains:    "assignCmd",
	}}
	results := runCodeChecks(tmpDir, checks)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Fatalf("expected check to pass, got error: %s", results[0].Error)
	}
}
