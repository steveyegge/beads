package dolt

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestSearchIssues_Lite_BlankHeavyFields is the Dolt leg of the per-backend
// correctness test for IssueFilter.Lite (be-uwvs.4).
//
// Round-trip contract under test:
//
//   - Seed an issue with every heavy text column non-empty plus identity,
//     metadata, and a known label.
//   - SearchIssues with Lite=true: heavy fields blank, IsLitePartial=true,
//     identity + metadata + label preserved.
//   - SearchIssues with Lite=false: heavy fields populated, IsLitePartial=false,
//     same identity + metadata + label.
//
// Failure modes this test catches:
//   - Lite path silently still selects heavy columns (no allocation win).
//   - Lite path drops identity / metadata / label hydration alongside heavies.
//   - IsLitePartial not set, so future call sites cannot detect partial fetches.
//   - Full path regresses to lite-shaped output (heavies blank when caller
//     asked for the full payload).
func TestSearchIssues_Lite_BlankHeavyFields(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Seed: every heavy text column non-empty, distinctive metadata, one label.
	meta, err := json.Marshal(map[string]string{"routed_to": "validator"})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	issue := &types.Issue{
		ID:                 "lite-heavy-1",
		Title:              "Lite-mode round trip",
		Description:        "DESCRIPTION-LOAD",
		Design:             "DESIGN-LOAD",
		AcceptanceCriteria: "ACCEPTANCE-LOAD",
		Notes:              "NOTES-LOAD",
		Payload:            `{"event":"heavy"}`,
		Waiters:            []string{"alice", "bob"},
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeTask,
		Assignee:           "alice",
		Metadata:           meta,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := store.AddLabel(ctx, issue.ID, "routing-test", "tester"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	// ----- Lite=true: heavy fields blank, IsLitePartial=true, rest preserved.
	liteResults, err := store.SearchIssues(ctx, "", types.IssueFilter{
		IDs:  []string{issue.ID},
		Lite: true,
	})
	if err != nil {
		t.Fatalf("SearchIssues(Lite=true): %v", err)
	}
	if len(liteResults) != 1 {
		t.Fatalf("SearchIssues(Lite=true): got %d results, want 1", len(liteResults))
	}
	got := liteResults[0]
	assertLiteRow(t, got, issue, []string{"routing-test"})

	// ----- Lite=false: heavy fields populated, IsLitePartial=false.
	fullResults, err := store.SearchIssues(ctx, "", types.IssueFilter{
		IDs:  []string{issue.ID},
		Lite: false,
	})
	if err != nil {
		t.Fatalf("SearchIssues(Lite=false): %v", err)
	}
	if len(fullResults) != 1 {
		t.Fatalf("SearchIssues(Lite=false): got %d results, want 1", len(fullResults))
	}
	assertFullRow(t, fullResults[0], issue, []string{"routing-test"})
}

// assertLiteRow checks the post-conditions of a SearchIssues call with Lite=true.
// Heavy fields must be zero-valued, identity/metadata/label preserved, and
// IsLitePartial must be true so downstream callers can detect partial fetches.
func assertLiteRow(t *testing.T, got, want *types.Issue, wantLabels []string) {
	t.Helper()

	// Heavy fields MUST be zero-valued.
	if got.Description != "" {
		t.Errorf("Lite: Description = %q, want \"\"", got.Description)
	}
	if got.Design != "" {
		t.Errorf("Lite: Design = %q, want \"\"", got.Design)
	}
	if got.AcceptanceCriteria != "" {
		t.Errorf("Lite: AcceptanceCriteria = %q, want \"\"", got.AcceptanceCriteria)
	}
	if got.Notes != "" {
		t.Errorf("Lite: Notes = %q, want \"\"", got.Notes)
	}
	if got.Payload != "" {
		t.Errorf("Lite: Payload = %q, want \"\"", got.Payload)
	}
	if len(got.Waiters) != 0 {
		t.Errorf("Lite: Waiters = %v, want empty", got.Waiters)
	}

	// IsLitePartial MUST be true.
	if !got.IsLitePartial {
		t.Error("Lite: IsLitePartial = false, want true (downstream callers detect partial fetch via this flag)")
	}

	// Identity / status / priority MUST be preserved.
	if got.ID != want.ID {
		t.Errorf("Lite: ID = %q, want %q", got.ID, want.ID)
	}
	if got.Title != want.Title {
		t.Errorf("Lite: Title = %q, want %q", got.Title, want.Title)
	}
	if got.Status != want.Status {
		t.Errorf("Lite: Status = %q, want %q", got.Status, want.Status)
	}
	if got.Priority != want.Priority {
		t.Errorf("Lite: Priority = %d, want %d", got.Priority, want.Priority)
	}
	if got.IssueType != want.IssueType {
		t.Errorf("Lite: IssueType = %q, want %q", got.IssueType, want.IssueType)
	}
	if got.Assignee != want.Assignee {
		t.Errorf("Lite: Assignee = %q, want %q", got.Assignee, want.Assignee)
	}

	// Metadata MUST survive lite hydration. The wire format is JSON bytes; we
	// compare semantic equivalence rather than byte equality because backends
	// may normalize whitespace.
	if !jsonEqual(got.Metadata, want.Metadata) {
		t.Errorf("Lite: Metadata = %s, want %s", string(got.Metadata), string(want.Metadata))
	}

	// Labels MUST hydrate (always, per types.IssueFilter docstring at
	// internal/types/types.go: "Labels are always hydrated").
	if !labelSetEqual(got.Labels, wantLabels) {
		t.Errorf("Lite: Labels = %v, want %v", got.Labels, wantLabels)
	}
}

// assertFullRow checks the post-conditions of a SearchIssues call with Lite=false.
// Every heavy field must hydrate, IsLitePartial must be false, and labels must
// still be present (independent of SELECT shape).
func assertFullRow(t *testing.T, got, want *types.Issue, wantLabels []string) {
	t.Helper()

	if got.Description != want.Description {
		t.Errorf("Full: Description = %q, want %q", got.Description, want.Description)
	}
	if got.Design != want.Design {
		t.Errorf("Full: Design = %q, want %q", got.Design, want.Design)
	}
	if got.AcceptanceCriteria != want.AcceptanceCriteria {
		t.Errorf("Full: AcceptanceCriteria = %q, want %q", got.AcceptanceCriteria, want.AcceptanceCriteria)
	}
	if got.Notes != want.Notes {
		t.Errorf("Full: Notes = %q, want %q", got.Notes, want.Notes)
	}
	if got.Payload != want.Payload {
		t.Errorf("Full: Payload = %q, want %q", got.Payload, want.Payload)
	}
	if !waitersEqual(got.Waiters, want.Waiters) {
		t.Errorf("Full: Waiters = %v, want %v", got.Waiters, want.Waiters)
	}

	if got.IsLitePartial {
		t.Error("Full: IsLitePartial = true, want false (full-shape callers must see IsLitePartial=false)")
	}

	if got.ID != want.ID {
		t.Errorf("Full: ID = %q, want %q", got.ID, want.ID)
	}
	if got.Title != want.Title {
		t.Errorf("Full: Title = %q, want %q", got.Title, want.Title)
	}
	if !labelSetEqual(got.Labels, wantLabels) {
		t.Errorf("Full: Labels = %v, want %v", got.Labels, wantLabels)
	}
}

// jsonEqual compares two JSON byte slices for semantic equality. Empty/nil on
// either side counts as equal to "{}" on the other to bridge backend-specific
// empty-metadata representations (Dolt may persist as NULL, PG as '{}').
func jsonEqual(a, b []byte) bool {
	emptyA := len(a) == 0 || string(a) == "{}" || string(a) == "null"
	emptyB := len(b) == 0 || string(b) == "{}" || string(b) == "null"
	if emptyA && emptyB {
		return true
	}
	if emptyA != emptyB {
		return false
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	aBytes, _ := json.Marshal(av)
	bBytes, _ := json.Marshal(bv)
	return string(aBytes) == string(bBytes)
}

// labelSetEqual compares two label slices as sets (order-insensitive).
// Both sides may be nil; nil and empty slice count as equal.
func labelSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

// waitersEqual compares two waiter slices order-insensitively. The waiters
// column is JSON-encoded, and backends may not preserve insertion order.
func waitersEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return labelSetEqual(a, b)
}
