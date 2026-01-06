package main

import (
	"os"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/toon"
	"github.com/steveyegge/beads/internal/types"
)

// TestTOONImportExportRoundTrip verifies that issues can be exported to TOON and re-imported
func TestTOONImportExportRoundTrip(t *testing.T) {
	// Create test issues
	now := time.Now()
	issues := []*types.Issue{
		{
			ID:          "bd-001",
			Title:       "Test issue 1",
			Description: "Description with\nmultiple lines",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeBug,
			Assignee:    "alice@example.com",
			CreatedAt:   now,
			UpdatedAt:   now.Add(time.Minute),
		},
		{
			ID:        "bd-002",
			Title:     "Test issue 2",
			Status:    types.StatusClosed,
			Priority:  0,
			IssueType: types.TypeFeature,
			CreatedAt: now,
			UpdatedAt: now.Add(2 * time.Minute),
			ClosedAt:  &[]time.Time{now.Add(3 * time.Minute)}[0],
		},
	}

	// Encode to TOON
	encoded, err := toon.EncodeTOON(issues)
	if err != nil {
		t.Fatalf("EncodeTOON failed: %v", err)
	}

	// Verify TOON format is valid
	if toon.DetectFormat(encoded) != toon.FormatTOON {
		t.Errorf("Encoded output is not valid TOON format: got %v", toon.DetectFormat(encoded))
	}

	// Decode back from TOON
	decoded, err := toon.DecodeTOON(encoded)
	if err != nil {
		t.Fatalf("DecodeTOON failed: %v", err)
	}

	// Verify count
	if len(decoded) != len(issues) {
		t.Errorf("Issue count mismatch: got %d, want %d", len(decoded), len(issues))
	}

	// Verify critical fields survive round-trip
	for i, original := range issues {
		if i >= len(decoded) {
			t.Errorf("Missing decoded issue at index %d", i)
			continue
		}
		decoded := decoded[i]

		if decoded.ID != original.ID {
			t.Errorf("ID mismatch: got %q, want %q", decoded.ID, original.ID)
		}
		if decoded.Title != original.Title {
			t.Errorf("Title mismatch: got %q, want %q", decoded.Title, original.Title)
		}
		if decoded.Status != original.Status {
			t.Errorf("Status mismatch: got %v, want %v", decoded.Status, original.Status)
		}
		if decoded.Priority != original.Priority {
			t.Errorf("Priority mismatch: got %d, want %d", decoded.Priority, original.Priority)
		}
		if decoded.IssueType != original.IssueType {
			t.Errorf("IssueType mismatch: got %v, want %v", decoded.IssueType, original.IssueType)
		}
	}
}

// TestFormatDetectionFromExtension verifies extension-based format detection
func TestFormatDetectionFromExtension(t *testing.T) {
	tests := []struct {
		filename string
		expected toon.Format
	}{
		{"issues.toon", toon.FormatTOON},
		{"issues.TOON", toon.FormatTOON},
		{"data.Toon", toon.FormatTOON},
		{"issues.jsonl", toon.FormatJSONL},
		{"issues.json", toon.FormatJSONL},
		{"/path/to/issues.toon", toon.FormatTOON},
		{"/path/to/issues.jsonl", toon.FormatJSONL},
		{"issues.txt", toon.FormatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := toon.DetectFormatFromExtension(tt.filename)
			if got != tt.expected {
				t.Errorf("DetectFormatFromExtension(%q) = %v, want %v", tt.filename, got, tt.expected)
			}
		})
	}
}

// TestTOONFileIO verifies writing and reading TOON files
func TestTOONFileIO(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.toon")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Create test issues
	now := time.Now()
	issues := []*types.Issue{
		{
			ID:        "bd-test-001",
			Title:     "TOON file IO test",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	// Encode and write
	encoded, err := toon.EncodeTOON(issues)
	if err != nil {
		t.Fatalf("EncodeTOON failed: %v", err)
	}

	if _, err := tmpFile.Write(encoded); err != nil {
		t.Fatalf("Failed to write TOON file: %v", err)
	}
	tmpFile.Close()

	// Read back
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to read TOON file: %v", err)
	}

	// Decode
	decoded, err := toon.DecodeTOON(data)
	if err != nil {
		t.Fatalf("DecodeTOON failed: %v", err)
	}

	// Verify
	if len(decoded) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(decoded))
	}
	if decoded[0].ID != "bd-test-001" {
		t.Errorf("ID mismatch: got %q, want %q", decoded[0].ID, "bd-test-001")
	}
}
