package memory

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// setupSearchLabelsTest creates a store with 3 issues and various labels:
//   - bd-1 "Alpha" with labels: backend, urgent
//   - bd-2 "Beta"  with labels: frontend, urgent
//   - bd-3 "Gamma" with no labels
func setupSearchLabelsTest(t *testing.T) *MemoryStorage {
	t.Helper()
	store := setupTestMemory(t)
	ctx := context.Background()

	issues := []*types.Issue{
		{Title: "Alpha", Priority: 2, Status: types.StatusOpen, IssueType: types.TypeTask},
		{Title: "Beta", Priority: 2, Status: types.StatusOpen, IssueType: types.TypeTask},
		{Title: "Gamma", Priority: 2, Status: types.StatusOpen, IssueType: types.TypeTask},
	}
	for _, issue := range issues {
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
	}

	// Add labels
	if err := store.AddLabel(ctx, "bd-1", "backend", "test"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	if err := store.AddLabel(ctx, "bd-1", "urgent", "test"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	if err := store.AddLabel(ctx, "bd-2", "frontend", "test"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	if err := store.AddLabel(ctx, "bd-2", "urgent", "test"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	return store
}

func issueIDs(issues []*types.Issue) []string {
	ids := make([]string, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID
	}
	return ids
}

func TestSearchIssues_LabelsAND(t *testing.T) {
	store := setupSearchLabelsTest(t)
	defer store.Close()
	ctx := context.Background()

	// Both bd-1 and bd-2 have "urgent"
	results, err := store.SearchIssues(ctx, "", types.IssueFilter{
		Labels: []string{"urgent"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for Labels=[urgent], got %d: %v", len(results), issueIDs(results))
	}

	// Only bd-1 has both backend AND urgent
	results, err = store.SearchIssues(ctx, "", types.IssueFilter{
		Labels: []string{"backend", "urgent"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(results) != 1 || results[0].ID != "bd-1" {
		t.Fatalf("expected [bd-1] for Labels=[backend,urgent], got %v", issueIDs(results))
	}
}

func TestSearchIssues_LabelsAnyOR(t *testing.T) {
	store := setupSearchLabelsTest(t)
	defer store.Close()
	ctx := context.Background()

	// bd-1 has "backend", bd-2 has "frontend" — OR should match both
	results, err := store.SearchIssues(ctx, "", types.IssueFilter{
		LabelsAny: []string{"backend", "frontend"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for LabelsAny=[backend,frontend], got %d: %v", len(results), issueIDs(results))
	}

	// Only bd-2 has "frontend"
	results, err = store.SearchIssues(ctx, "", types.IssueFilter{
		LabelsAny: []string{"frontend"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(results) != 1 || results[0].ID != "bd-2" {
		t.Fatalf("expected [bd-2] for LabelsAny=[frontend], got %v", issueIDs(results))
	}

	// No issue has "nonexistent"
	results, err = store.SearchIssues(ctx, "", types.IssueFilter{
		LabelsAny: []string{"nonexistent"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for LabelsAny=[nonexistent], got %d: %v", len(results), issueIDs(results))
	}
}

func TestSearchIssues_LabelsAnyExcludesUnlabeled(t *testing.T) {
	store := setupSearchLabelsTest(t)
	defer store.Close()
	ctx := context.Background()

	// bd-3 has no labels — should not appear in any LabelsAny query
	results, err := store.SearchIssues(ctx, "", types.IssueFilter{
		LabelsAny: []string{"backend", "frontend", "urgent"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	for _, r := range results {
		if r.ID == "bd-3" {
			t.Fatalf("bd-3 (no labels) should not match LabelsAny filter")
		}
	}
}

func TestSearchIssues_LabelsANDandOR(t *testing.T) {
	store := setupSearchLabelsTest(t)
	defer store.Close()
	ctx := context.Background()

	// AND: must have "urgent", OR: must have "backend" or "frontend"
	// bd-1 has urgent+backend (matches both), bd-2 has urgent+frontend (matches both)
	results, err := store.SearchIssues(ctx, "", types.IssueFilter{
		Labels:    []string{"urgent"},
		LabelsAny: []string{"backend", "frontend"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for Labels=[urgent]+LabelsAny=[backend,frontend], got %d: %v", len(results), issueIDs(results))
	}

	// AND: must have "backend", OR: must have "frontend" or "urgent"
	// Only bd-1 has "backend" AND ("frontend" or "urgent") — bd-1 has backend+urgent
	results, err = store.SearchIssues(ctx, "", types.IssueFilter{
		Labels:    []string{"backend"},
		LabelsAny: []string{"frontend", "urgent"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(results) != 1 || results[0].ID != "bd-1" {
		t.Fatalf("expected [bd-1] for Labels=[backend]+LabelsAny=[frontend,urgent], got %v", issueIDs(results))
	}
}
