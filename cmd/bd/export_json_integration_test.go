//go:build cgo && integration

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCLI_ExportJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := setupCLITestDB(t)
	runBDInProcess(t, tmpDir, "create", "JSON export test issue", "-p", "1", "-t", "bug")
	runBDInProcess(t, tmpDir, "create", "Second issue", "-p", "2", "-t", "task")

	exportFile := filepath.Join(tmpDir, "export.json")
	runBDInProcess(t, tmpDir, "export", "--format", "json", "-o", exportFile)

	data, err := os.ReadFile(exportFile)
	if err != nil {
		t.Fatalf("Failed to read export file: %v", err)
	}

	var doc JSONExport
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Invalid JSON in export file: %v", err)
	}

	if doc.Version == "" {
		t.Error("expected non-empty version")
	}
	if doc.Metadata.Count != 2 {
		t.Errorf("expected 2 issues, got %d", doc.Metadata.Count)
	}
	if len(doc.Issues) != 2 {
		t.Errorf("expected 2 issues in array, got %d", len(doc.Issues))
	}
}

func TestCLI_ExportJSON_SingleBead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := setupCLITestDB(t)

	// Create two issues, export only one by ID
	out1 := runBDInProcess(t, tmpDir, "create", "First issue", "-p", "1", "--json")
	_ = runBDInProcess(t, tmpDir, "create", "Second issue", "-p", "2", "--json")

	var created1 map[string]interface{}
	json.Unmarshal([]byte(out1), &created1)

	id1, _ := created1["id"].(string)

	// Export only the first issue using positional arg
	stdout := runBDInProcess(t, tmpDir, "export", "--format", "json", id1)

	var doc JSONExport
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if doc.Metadata.Count != 1 {
		t.Errorf("expected 1 issue, got %d", doc.Metadata.Count)
	}
	if len(doc.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(doc.Issues))
	}
	if doc.Issues[0].ID != id1 {
		t.Errorf("expected issue %s, got %s", id1, doc.Issues[0].ID)
	}
}

func TestCLI_ExportJSON_SinceFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := setupCLITestDB(t)
	runBDInProcess(t, tmpDir, "create", "Since filter test", "-p", "1")

	// Export with --since set to yesterday (should include all)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	stdout := runBDInProcess(t, tmpDir, "export", "--format", "json", "--since", yesterday)

	var doc JSONExport
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if doc.Metadata.Count < 1 {
		t.Errorf("expected at least 1 issue with --since %s, got %d", yesterday, doc.Metadata.Count)
	}
	if doc.Metadata.Filters["updated_after"] != yesterday {
		t.Errorf("expected updated_after filter = %s, got %s", yesterday, doc.Metadata.Filters["updated_after"])
	}
}

func TestCLI_ExportJSON_SinceConflictsWithUpdatedAfter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := setupCLITestDB(t)
	runBDInProcess(t, tmpDir, "create", "Conflict test", "-p", "1")

	_, _, err := runBDInProcessAllowError(t, tmpDir, "export", "--format", "json", "--since", "2026-01-01", "--updated-after", "2026-01-01")
	if err == nil {
		t.Error("expected error when both --since and --updated-after are provided")
	}
}
