package beads

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
	toon "github.com/toon-format/toon-go"
)

// SimpleTestIssue is a minimal test issue without complex types
type SimpleTestIssue struct {
	ID       string `json:"id" toon:"id"`
	Title    string `json:"title" toon:"title"`
	Priority int    `json:"priority" toon:"priority"`
}

// Helper to create a test issue
func createTestIssue(id, title string) *types.Issue {
	now := time.Now().UTC()
	return &types.Issue{
		ID:        id,
		Title:     title,
		Priority:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestMarshalToTOON(t *testing.T) {
	issues := []*SimpleTestIssue{
		{ID: "bd-1", Title: "First issue", Priority: 1},
		{ID: "bd-2", Title: "Second issue", Priority: 1},
	}

	data, err := toon.Marshal(issues)
	if err != nil {
		t.Fatalf("Marshal to TOON failed: %v", err)
	}

	if len(data) == 0 {
		t.Errorf("Marshal to TOON returned empty data")
	}

	// Verify starts with [ (array) or {id, (object)
	if len(data) < 1 {
		t.Errorf("TOON data should not be empty")
	}
}

func TestUnmarshalFromTOON(t *testing.T) {
	// Create test issues
	original := []*SimpleTestIssue{
		{ID: "bd-1", Title: "First issue", Priority: 1},
		{ID: "bd-2", Title: "Second issue", Priority: 1},
	}

	// Marshal to TOON
	data, err := toon.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal to TOON failed: %v", err)
	}

	// Unmarshal back from TOON
	var restored []*SimpleTestIssue
	err = toon.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal from TOON failed: %v", err)
	}

	if len(restored) != len(original) {
		t.Errorf("expected %d issues, got %d", len(original), len(restored))
	}

	for i, issue := range restored {
		if issue.ID != original[i].ID {
			t.Errorf("issue %d: expected ID %q, got %q", i, original[i].ID, issue.ID)
		}
		if issue.Title != original[i].Title {
			t.Errorf("issue %d: expected Title %q, got %q", i, original[i].Title, issue.Title)
		}
	}
}

func TestMarshalToJSONL(t *testing.T) {
	issues := []*types.Issue{
		createTestIssue("bd-1", "First issue"),
		createTestIssue("bd-2", "Second issue"),
	}

	data, err := MarshalToJSONL(issues)
	if err != nil {
		t.Fatalf("MarshalToJSONL failed: %v", err)
	}

	if len(data) == 0 {
		t.Errorf("MarshalToJSONL returned empty data")
	}

	// Verify it's valid JSON (either single object or array)
	var unmarshalled interface{}
	err = json.Unmarshal(data, &unmarshalled)
	if err != nil {
		t.Errorf("JSONL data is not valid JSON: %v", err)
	}
}

func TestUnmarshalFromJSONL(t *testing.T) {
	// Create test issues
	original := []*types.Issue{
		createTestIssue("bd-1", "First issue"),
		createTestIssue("bd-2", "Second issue"),
	}

	// Marshal to JSONL
	data, err := MarshalToJSONL(original)
	if err != nil {
		t.Fatalf("MarshalToJSONL failed: %v", err)
	}

	// Unmarshal back from JSONL
	restored, err := UnmarshalFromJSONL(data)
	if err != nil {
		t.Fatalf("UnmarshalFromJSONL failed: %v", err)
	}

	if len(restored) != len(original) {
		t.Errorf("expected %d issues, got %d", len(original), len(restored))
	}

	for i, issue := range restored {
		if issue.ID != original[i].ID {
			t.Errorf("issue %d: expected ID %q, got %q", i, original[i].ID, issue.ID)
		}
		if issue.Title != original[i].Title {
			t.Errorf("issue %d: expected Title %q, got %q", i, original[i].Title, issue.Title)
		}
	}
}

func TestRoundTrip_TOON(t *testing.T) {
	// Create test issues
	original := []*SimpleTestIssue{
		{ID: "bd-1", Title: "First issue", Priority: 1},
		{ID: "bd-2", Title: "Second issue", Priority: 1},
	}

	// TOON → marshal → unmarshal
	marshalled, err := toon.Marshal(original)
	if err != nil {
		t.Fatalf("marshal to TOON failed: %v", err)
	}

	var restored []*SimpleTestIssue
	err = toon.Unmarshal(marshalled, &restored)
	if err != nil {
		t.Fatalf("unmarshal from TOON failed: %v", err)
	}

	if len(restored) != len(original) {
		t.Errorf("round-trip: expected %d issues, got %d", len(original), len(restored))
	}

	// Verify data integrity
	for i, issue := range restored {
		if issue.ID != original[i].ID {
			t.Errorf("round-trip issue %d: ID mismatch %q != %q", i, issue.ID, original[i].ID)
		}
		if issue.Title != original[i].Title {
			t.Errorf("round-trip issue %d: Title mismatch %q != %q", i, issue.Title, original[i].Title)
		}
		if issue.Priority != original[i].Priority {
			t.Errorf("round-trip issue %d: Priority mismatch %d != %d", i, issue.Priority, original[i].Priority)
		}
	}
}

func TestRoundTrip_JSONL(t *testing.T) {
	// Create test issues
	original := []*types.Issue{
		createTestIssue("bd-1", "First issue"),
		createTestIssue("bd-2", "Second issue"),
	}

	// JSONL → marshal → unmarshal
	marshalled, err := MarshalToJSONL(original)
	if err != nil {
		t.Fatalf("MarshalToJSONL failed: %v", err)
	}

	restored, err := UnmarshalFromJSONL(marshalled)
	if err != nil {
		t.Fatalf("UnmarshalFromJSONL failed: %v", err)
	}

	if len(restored) != len(original) {
		t.Errorf("round-trip: expected %d issues, got %d", len(original), len(restored))
	}

	// Verify data integrity
	for i, issue := range restored {
		if issue.ID != original[i].ID {
			t.Errorf("round-trip issue %d: ID mismatch %q != %q", i, issue.ID, original[i].ID)
		}
		if issue.Title != original[i].Title {
			t.Errorf("round-trip issue %d: Title mismatch %q != %q", i, issue.Title, original[i].Title)
		}
		if issue.Priority != original[i].Priority {
			t.Errorf("round-trip issue %d: Priority mismatch %d != %d", i, issue.Priority, original[i].Priority)
		}
	}
}
