package main

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestSortIssuesForExport_OrdersByPriorityThenCreatedThenID(t *testing.T) {
	t.Parallel()

	early := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	issues := []*types.Issue{
		{ID: "bd-3", Priority: 2, CreatedAt: early},
		{ID: "bd-1", Priority: 0, CreatedAt: early},
		{ID: "bd-2", Priority: 0, CreatedAt: late},
		{ID: "bd-4", Priority: 0, CreatedAt: early},
	}

	sortIssuesForExport(issues)

	// Expected order:
	//  P0 + late created (bd-2)  — newer created_at sorts first (DESC)
	//  P0 + early created, id bd-1
	//  P0 + early created, id bd-4
	//  P2 (bd-3)
	want := []string{"bd-2", "bd-1", "bd-4", "bd-3"}
	for i, id := range want {
		if issues[i].ID != id {
			t.Errorf("position %d: got %s, want %s (full order: %v)", i, issues[i].ID, id, ids(issues))
		}
	}
}

func TestSortIssuesForExport_SortsDependenciesWithinIssue(t *testing.T) {
	t.Parallel()

	issues := []*types.Issue{
		{
			ID:       "bd-1",
			Priority: 1,
			Dependencies: []*types.Dependency{
				{IssueID: "bd-1", DependsOnID: "bd-9", Type: types.DepBlocks},
				{IssueID: "bd-1", DependsOnID: "bd-2", Type: types.DepRelated},
				{IssueID: "bd-1", DependsOnID: "bd-2", Type: types.DepBlocks},
			},
		},
	}

	sortIssuesForExport(issues)

	deps := issues[0].Dependencies
	// Sorted by DependsOnID, then Type.
	if deps[0].DependsOnID != "bd-2" || deps[1].DependsOnID != "bd-2" || deps[2].DependsOnID != "bd-9" {
		t.Fatalf("dependencies not ordered by DependsOnID: %+v", deps)
	}
	if deps[0].Type != types.DepBlocks || deps[1].Type != types.DepRelated {
		t.Errorf("ties not broken by Type: %+v", deps)
	}
}

func TestSortIssuesForExport_StableAcrossRuns(t *testing.T) {
	t.Parallel()

	created := time.Date(2024, 3, 3, 0, 0, 0, 0, time.UTC)
	build := func() []*types.Issue {
		return []*types.Issue{
			{ID: "bd-5", Priority: 1, CreatedAt: created},
			{ID: "bd-2", Priority: 1, CreatedAt: created},
			{ID: "bd-8", Priority: 1, CreatedAt: created},
			{ID: "bd-1", Priority: 1, CreatedAt: created},
		}
	}

	a := build()
	b := build()
	sortIssuesForExport(a)
	sortIssuesForExport(b)

	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("non-deterministic order at %d: %v vs %v", i, ids(a), ids(b))
		}
	}
	// With equal priority and created_at, order falls back to ID ascending.
	want := []string{"bd-1", "bd-2", "bd-5", "bd-8"}
	for i, id := range want {
		if a[i].ID != id {
			t.Errorf("position %d: got %s, want %s", i, a[i].ID, id)
		}
	}
}

func ids(issues []*types.Issue) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.ID
	}
	return out
}
