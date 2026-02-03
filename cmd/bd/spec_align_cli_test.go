//go:build integration
// +build integration

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSpecAlignReportIncludesSpec(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := setupCLITestDB(t)
	specDir := filepath.Join(tmpDir, "specs", "active")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	specPath := filepath.Join(specDir, "PACMAN_MODE_SPEC.md")
	if err := os.WriteFile(specPath, []byte("# Pacman"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	runBDInProcess(t, tmpDir, "spec", "scan")
	out := runBDInProcess(t, tmpDir, "spec", "align", "--json")

	var report specAlignReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, entry := range report.Entries {
		if entry.SpecID == "specs/active/PACMAN_MODE_SPEC.md" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected pacman spec in report")
	}
}
