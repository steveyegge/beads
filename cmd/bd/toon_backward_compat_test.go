package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/toon"
	"github.com/steveyegge/beads/internal/types"
)

// TestBackwardCompatJSONLToTOONRoundTrip verifies lossless conversion of core issue fields in TOON
// Note: Labels, Dependencies, Comments are stored separately in the database and populated
// during export/import via store queries, not in the TOON file itself.
func TestBackwardCompatJSONLToTOONRoundTrip(t *testing.T) {
	// Create comprehensive test issues that cover all field types
	now := time.Now()
	closedTime := now.Add(24 * time.Hour)
	estimatedMin := 120

	original := []*types.Issue{
		{
			ID:          "bd-comp-001",
			Title:       "Simple bug",
			Description: "Basic description",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeBug,
			Assignee:    "alice@example.com",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "bd-comp-002",
			Title:       "Issue with newlines and special chars",
			Description: "Line 1\nLine 2\nLine 3",
			Status:      types.StatusInProgress,
			Priority:    0,
			IssueType:   types.TypeFeature,
			Assignee:    "bob@example.com",
			CreatedAt:   now,
			UpdatedAt:   now.Add(1 * time.Hour),
			// Labels and Dependencies are populated from database during export/import
			// and are not stored in the TOON file itself
		},
		{
			ID:          "bd-comp-003",
			Title:       "Closed issue",
			Description: "Issue that was completed",
			Status:      types.StatusClosed,
			Priority:    3,
			IssueType:   types.TypeTask,
			CreatedAt:   now,
			UpdatedAt:   closedTime,
			ClosedAt:    &closedTime,
			CloseReason: "Completed",
		},
		{
			ID:                "bd-comp-004",
			Title:             "Issue with estimated time",
			Status:            types.StatusOpen,
			Priority:          2,
			IssueType:         types.TypeFeature,
			CreatedAt:         now,
			UpdatedAt:         now,
			EstimatedMinutes:  &estimatedMin,
		},
		{
			ID:        "bd-comp-005",
			Title:     "Minimal issue",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	// Encode to TOON
	encoded, err := toon.EncodeTOON(original)
	if err != nil {
		t.Fatalf("EncodeTOON failed: %v", err)
	}

	// Verify TOON format is detected correctly
	if toon.DetectFormat(encoded) != toon.FormatTOON {
		t.Errorf("Encoded output is not valid TOON format")
	}

	// Decode back from TOON
	decoded, err := toon.DecodeTOON(encoded)
	if err != nil {
		t.Fatalf("DecodeTOON failed: %v", err)
	}

	// Verify count
	if len(decoded) != len(original) {
		t.Fatalf("Issue count mismatch: got %d, want %d", len(decoded), len(original))
	}

	// Verify all fields are preserved
	for i, orig := range original {
		dec := decoded[i]

		tests := []struct {
			name string
			got  interface{}
			want interface{}
		}{
			{"ID", dec.ID, orig.ID},
			{"Title", dec.Title, orig.Title},
			{"Description", dec.Description, orig.Description},
			{"Status", dec.Status, orig.Status},
			{"Priority", dec.Priority, orig.Priority},
			{"IssueType", dec.IssueType, orig.IssueType},
			{"Assignee", dec.Assignee, orig.Assignee},
			{"CloseReason", dec.CloseReason, orig.CloseReason},
		}

		for _, test := range tests {
			if test.got != test.want {
				t.Errorf("Issue %d (%s): %s mismatch: got %v, want %v",
					i, orig.ID, test.name, test.got, test.want)
			}
		}

		// Check timestamps
		if !dec.CreatedAt.Equal(orig.CreatedAt) {
			t.Errorf("Issue %d: CreatedAt mismatch: got %v, want %v",
				i, dec.CreatedAt, orig.CreatedAt)
		}
		if !dec.UpdatedAt.Equal(orig.UpdatedAt) {
			t.Errorf("Issue %d: UpdatedAt mismatch: got %v, want %v",
				i, dec.UpdatedAt, orig.UpdatedAt)
		}

		// Check optional timestamps
		if (dec.ClosedAt == nil) != (orig.ClosedAt == nil) {
			t.Errorf("Issue %d: ClosedAt nil mismatch", i)
		} else if dec.ClosedAt != nil && !dec.ClosedAt.Equal(*orig.ClosedAt) {
			t.Errorf("Issue %d: ClosedAt mismatch: got %v, want %v",
				i, dec.ClosedAt, orig.ClosedAt)
		}

		// Check estimated minutes
		if (dec.EstimatedMinutes == nil) != (orig.EstimatedMinutes == nil) {
			t.Errorf("Issue %d: EstimatedMinutes nil mismatch", i)
		} else if dec.EstimatedMinutes != nil && *dec.EstimatedMinutes != *orig.EstimatedMinutes {
			t.Errorf("Issue %d: EstimatedMinutes mismatch: got %v, want %v",
				i, *dec.EstimatedMinutes, *orig.EstimatedMinutes)
		}

		// Note: Labels, Dependencies, Comments are loaded from database during export/import
		// They are not encoded in the TOON file itself, so we don't check them here
	}
}

// TestBackwardCompatEmptyFields verifies null vs empty string distinction
func TestBackwardCompatEmptyFields(t *testing.T) {
	now := time.Now()

	original := []*types.Issue{
		{
			ID:          "bd-empty-001",
			Title:       "Has assignee",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			CreatedAt:   now,
			UpdatedAt:   now,
			Assignee:    "alice@example.com",
		},
		{
			ID:        "bd-empty-002",
			Title:     "No assignee",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: now,
			UpdatedAt: now,
			// Assignee is empty string (not set)
		},
	}

	// Round-trip
	encoded, err := toon.EncodeTOON(original)
	if err != nil {
		t.Fatalf("EncodeTOON failed: %v", err)
	}

	decoded, err := toon.DecodeTOON(encoded)
	if err != nil {
		t.Fatalf("DecodeTOON failed: %v", err)
	}

	// Verify assignee distinction
	if decoded[0].Assignee != "alice@example.com" {
		t.Errorf("Issue 0: Expected assignee 'alice@example.com', got %q", decoded[0].Assignee)
	}
	if decoded[1].Assignee != "" {
		t.Errorf("Issue 1: Expected empty assignee, got %q", decoded[1].Assignee)
	}
}

// TestBackwardCompatComplexDescription tests escaping of special characters
// Note: toon-go library has limitations with certain control characters.
// This test focuses on characters that round-trip safely.
func TestBackwardCompatComplexDescription(t *testing.T) {
	now := time.Now()

	testCases := []struct {
		name string
		desc string
	}{
		{"Commas", "Line 1, Line 2, Line 3"},
		{"Backslashes", "Path: C:\\Users\\test"},
		{"Newlines", "Line 1\nLine 2\nLine 3"},
		{"Multiple lines", "Description line 1\nDescription line 2\nDescription line 3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := []*types.Issue{
				{
					ID:          "bd-desc-test",
					Title:       tc.name,
					Description: tc.desc,
					Status:      types.StatusOpen,
					Priority:    2,
					IssueType:   types.TypeTask,
					CreatedAt:   now,
					UpdatedAt:   now,
				},
			}

			// Round-trip
			encoded, err := toon.EncodeTOON(original)
			if err != nil {
				t.Fatalf("EncodeTOON failed: %v", err)
			}

			decoded, err := toon.DecodeTOON(encoded)
			if err != nil {
				t.Fatalf("DecodeTOON failed: %v", err)
			}

			if len(decoded) != 1 {
				t.Fatalf("Expected 1 issue, got %d", len(decoded))
			}

			if decoded[0].Description != original[0].Description {
				t.Errorf("Description mismatch:\n  got:  %q\n  want: %q",
					decoded[0].Description, original[0].Description)
			}
		})
	}
}

// TestBackwardCompatManyIssues tests with a larger dataset
func TestBackwardCompatManyIssues(t *testing.T) {
	// Create 100 issues
	now := time.Now()
	original := make([]*types.Issue, 100)

	for i := 0; i < 100; i++ {
		original[i] = &types.Issue{
			ID:        genIssueID(i),
			Title:     "Issue #" + fmt.Sprintf("%d", i),
			Status:    types.StatusOpen,
			Priority:  i % 5,
			IssueType: types.TypeTask,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i*2) * time.Minute),
		}
	}

	// Encode
	encoded, err := toon.EncodeTOON(original)
	if err != nil {
		t.Fatalf("EncodeTOON failed: %v", err)
	}

	// Decode
	decoded, err := toon.DecodeTOON(encoded)
	if err != nil {
		t.Fatalf("DecodeTOON failed: %v", err)
	}

	// Verify count
	if len(decoded) != len(original) {
		t.Fatalf("Issue count mismatch: got %d, want %d", len(decoded), len(original))
	}

	// Spot check a few issues
	for _, idx := range []int{0, 50, 99} {
		if decoded[idx].ID != original[idx].ID {
			t.Errorf("Issue %d ID mismatch: got %q, want %q",
				idx, decoded[idx].ID, original[idx].ID)
		}
		if decoded[idx].Priority != original[idx].Priority {
			t.Errorf("Issue %d Priority mismatch: got %d, want %d",
				idx, decoded[idx].Priority, original[idx].Priority)
		}
	}
}

// Helper to generate unique issue IDs
func genIssueID(i int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	id := "bd-"
	for j := 0; j < 4; j++ {
		id += string(alphabet[(i*7+j) % len(alphabet)])
	}
	return id
}
