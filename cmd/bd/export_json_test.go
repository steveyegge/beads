package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestWriteJSONExport_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := writeJSONExport(&buf, nil, nil)
	if err != nil {
		t.Fatalf("writeJSONExport failed: %v", err)
	}

	var doc JSONExport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if doc.Version == "" {
		t.Error("expected non-empty version")
	}
	if doc.Metadata.Count != 0 {
		t.Errorf("expected count 0, got %d", doc.Metadata.Count)
	}
	if doc.Issues == nil {
		t.Error("expected non-nil issues array")
	}
	if len(doc.Issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(doc.Issues))
	}
}

func TestWriteJSONExport_WithIssues(t *testing.T) {
	now := time.Now().UTC()
	issues := []*types.Issue{
		{
			ID:        "test-001",
			Title:     "First issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
			CreatedAt: now,
			UpdatedAt: now,
			Labels:    []string{"critical"},
			Comments: []*types.Comment{
				{
					ID:        1,
					IssueID:   "test-001",
					Author:    "alice",
					Text:      "Working on it",
					CreatedAt: now,
				},
			},
			Dependencies: []*types.Dependency{
				{
					IssueID:     "test-001",
					DependsOnID: "test-002",
					Type:        types.DepBlocks,
					CreatedAt:   now,
				},
			},
		},
		{
			ID:        "test-002",
			Title:     "Second issue",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	filters := map[string]string{
		"status": "open",
		"type":   "bug",
	}

	var buf bytes.Buffer
	err := writeJSONExport(&buf, issues, filters)
	if err != nil {
		t.Fatalf("writeJSONExport failed: %v", err)
	}

	var doc JSONExport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if doc.Version != Version {
		t.Errorf("expected version %s, got %s", Version, doc.Version)
	}
	if doc.Metadata.Count != 2 {
		t.Errorf("expected count 2, got %d", doc.Metadata.Count)
	}
	if doc.Metadata.ExportedAt.IsZero() {
		t.Error("expected non-zero exported_at")
	}
	if doc.Metadata.Filters["status"] != "open" {
		t.Errorf("expected filter status=open, got %s", doc.Metadata.Filters["status"])
	}
	if doc.Metadata.Filters["type"] != "bug" {
		t.Errorf("expected filter type=bug, got %s", doc.Metadata.Filters["type"])
	}
	if len(doc.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(doc.Issues))
	}
	if doc.Issues[0].ID != "test-001" {
		t.Errorf("expected first issue ID test-001, got %s", doc.Issues[0].ID)
	}
	if len(doc.Issues[0].Comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(doc.Issues[0].Comments))
	}
	if len(doc.Issues[0].Dependencies) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(doc.Issues[0].Dependencies))
	}
}

func TestWriteJSONExport_PrettyPrinted(t *testing.T) {
	issues := []*types.Issue{
		{
			ID:        "test-001",
			Title:     "Test",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}

	var buf bytes.Buffer
	err := writeJSONExport(&buf, issues, nil)
	if err != nil {
		t.Fatalf("writeJSONExport failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "  ") {
		t.Error("expected indented (pretty-printed) JSON output")
	}
}

func TestWriteJSONExport_NoFilters(t *testing.T) {
	var buf bytes.Buffer
	err := writeJSONExport(&buf, []*types.Issue{}, nil)
	if err != nil {
		t.Fatalf("writeJSONExport failed: %v", err)
	}

	var doc JSONExport
	json.Unmarshal(buf.Bytes(), &doc)
	if doc.Metadata.Filters != nil {
		t.Errorf("expected nil filters when none provided, got %v", doc.Metadata.Filters)
	}
}
