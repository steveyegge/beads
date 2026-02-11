package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestReadJSONLToMap(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test.jsonl")

	// Create test JSONL with 3 issues
	issues := []types.Issue{
		{ID: "test-001", Title: "First", Status: types.StatusOpen},
		{ID: "test-002", Title: "Second", Status: types.StatusInProgress},
		{ID: "test-003", Title: "Third", Status: types.StatusClosed},
	}

	var content string
	for _, issue := range issues {
		data, _ := json.Marshal(issue)
		content += string(data) + "\n"
	}

	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Test readJSONLToMap
	issueMap, ids, err := readJSONLToMap(jsonlPath)
	if err != nil {
		t.Fatalf("readJSONLToMap failed: %v", err)
	}

	// Verify count
	if len(issueMap) != 3 {
		t.Errorf("expected 3 issues in map, got %d", len(issueMap))
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(ids))
	}

	// Verify order preserved
	expectedOrder := []string{"test-001", "test-002", "test-003"}
	for i, id := range ids {
		if id != expectedOrder[i] {
			t.Errorf("ID order mismatch at %d: expected %s, got %s", i, expectedOrder[i], id)
		}
	}

	// Verify content can be unmarshaled
	for id, rawJSON := range issueMap {
		var issue types.Issue
		if err := json.Unmarshal(rawJSON, &issue); err != nil {
			t.Errorf("failed to unmarshal issue %s: %v", id, err)
		}
		if issue.ID != id {
			t.Errorf("ID mismatch: expected %s, got %s", id, issue.ID)
		}
	}
}

func TestReadJSONLToMapWithMalformedLines(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test.jsonl")

	content := `{"id": "test-001", "title": "Good"}
{invalid json}
{"id": "test-002", "title": "Also Good"}

{"id": "", "title": "No ID"}
{"id": "test-003", "title": "Third Good"}
`

	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	issueMap, ids, err := readJSONLToMap(jsonlPath)
	if err != nil {
		t.Fatalf("readJSONLToMap failed: %v", err)
	}

	// Should have 3 valid issues (skipped invalid JSON, empty line, and empty ID)
	if len(issueMap) != 3 {
		t.Errorf("expected 3 issues, got %d", len(issueMap))
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(ids))
	}
}
