//go:build !no_dolt && cgo && dolt_only

package embeddeddolt_test

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestSearchIssues_Lite_BlankHeavyFields is the embedded-dolt leg of the
// per-backend correctness test for IssueFilter.Lite (be-uwvs.4).
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
// EmbeddedDoltStore delegates SearchIssues to issueops.SearchIssuesInTx
// (list_queries.go:13) which honors filter.Lite via the foundation that
// shipped in be-uwvs.1. This test asserts the contract end-to-end through
// the embedded backend's connection-management plumbing, not just the
// shared scan helpers (which have their own unit tests in
// internal/storage/issueops/scan_test.go).
func TestSearchIssues_Lite_BlankHeavyFields(t *testing.T) {
	skipUnlessEmbeddedDolt(t)

	te := newTestEnv(t, "lt")
	ctx := t.Context()

	meta, err := json.Marshal(map[string]string{"routed_to": "validator"})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	issue := &types.Issue{
		ID:                 "lt-heavy1",
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
	if err := te.store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := te.store.AddLabel(ctx, issue.ID, "routing-test", "tester"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	// ----- Lite=true: heavy fields blank, IsLitePartial=true, rest preserved.
	liteResults, err := te.store.SearchIssues(ctx, "", types.IssueFilter{
		IDs:  []string{issue.ID},
		Lite: true,
	})
	if err != nil {
		t.Fatalf("SearchIssues(Lite=true): %v", err)
	}
	if len(liteResults) != 1 {
		t.Fatalf("SearchIssues(Lite=true): got %d results, want 1", len(liteResults))
	}
	assertLiteRow(t, liteResults[0], issue, []string{"routing-test"})

	// ----- Lite=false: heavy fields populated, IsLitePartial=false.
	fullResults, err := te.store.SearchIssues(ctx, "", types.IssueFilter{
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

// assertLiteRow / assertFullRow / helpers mirror the per-backend pattern in
// internal/storage/dolt/lite_search_test.go and
// internal/storage/postgres/lite_search_test.go. Each backend's test owns its
// own copy because they share no test helpers across packages — _test.go
// helpers do not export across package boundaries.

func assertLiteRow(t *testing.T, got, want *types.Issue, wantLabels []string) {
	t.Helper()

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

	if !got.IsLitePartial {
		t.Error("Lite: IsLitePartial = false, want true (downstream callers detect partial fetch via this flag)")
	}

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

	if !jsonEqual(got.Metadata, want.Metadata) {
		t.Errorf("Lite: Metadata = %s, want %s", string(got.Metadata), string(want.Metadata))
	}

	if !labelSetEqual(got.Labels, wantLabels) {
		t.Errorf("Lite: Labels = %v, want %v", got.Labels, wantLabels)
	}
}

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

func waitersEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return labelSetEqual(a, b)
}
