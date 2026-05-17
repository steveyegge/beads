package dolt

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestDemoteToWispPreservesInboundDependencies(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "mixed-demote-src")
	createPerm(t, ctx, store, "mixed-demote-target")
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     "mixed-demote-src",
		DependsOnID: "mixed-demote-target",
		Type:        types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("AddDependency before demote: %v", err)
	}

	if err := store.UpdateIssue(ctx, "mixed-demote-target", map[string]interface{}{
		"no_history": true,
	}, "tester"); err != nil {
		t.Fatalf("demote target to no-history wisp: %v", err)
	}

	deps, err := store.GetDependencies(ctx, "mixed-demote-src")
	if err != nil {
		t.Fatalf("GetDependencies after demote: %v", err)
	}
	if len(deps) != 1 || deps[0].ID != "mixed-demote-target" || !deps[0].NoHistory {
		t.Fatalf("dependencies after demote = %+v, want no-history target", deps)
	}

	dependents, err := store.GetDependents(ctx, "mixed-demote-target")
	if err != nil {
		t.Fatalf("GetDependents after demote: %v", err)
	}
	if len(dependents) != 1 || dependents[0].ID != "mixed-demote-src" {
		t.Fatalf("dependents after demote = %+v, want source", dependents)
	}
}

func TestPromoteFromEphemeralPreservesInboundDependencies(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "mixed-promote-src")
	createWisp(t, ctx, store, "mixed-promote-target")
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     "mixed-promote-src",
		DependsOnID: "mixed-promote-target",
		Type:        types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("AddDependency before promote: %v", err)
	}

	if err := store.PromoteFromEphemeral(ctx, "mixed-promote-target", "tester"); err != nil {
		t.Fatalf("PromoteFromEphemeral: %v", err)
	}

	deps, err := store.GetDependencies(ctx, "mixed-promote-src")
	if err != nil {
		t.Fatalf("GetDependencies after promote: %v", err)
	}
	if len(deps) != 1 || deps[0].ID != "mixed-promote-target" || deps[0].Ephemeral {
		t.Fatalf("dependencies after promote = %+v, want permanent target", deps)
	}

	dependents, err := store.GetDependents(ctx, "mixed-promote-target")
	if err != nil {
		t.Fatalf("GetDependents after promote: %v", err)
	}
	if len(dependents) != 1 || dependents[0].ID != "mixed-promote-src" {
		t.Fatalf("dependents after promote = %+v, want source", dependents)
	}
}

func TestPromoteFromEphemeralCommitsAuxiliaryTables(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createWisp(t, ctx, store, "mixed-promote-aux")
	createPerm(t, ctx, store, "mixed-promote-blocker")

	if err := store.AddLabel(ctx, "mixed-promote-aux", "keep", "tester"); err != nil {
		t.Fatalf("AddLabel before promote: %v", err)
	}
	if _, err := store.AddIssueComment(ctx, "mixed-promote-aux", "tester", "retain this context"); err != nil {
		t.Fatalf("AddIssueComment before promote: %v", err)
	}
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     "mixed-promote-aux",
		DependsOnID: "mixed-promote-blocker",
		Type:        types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("AddDependency before promote: %v", err)
	}

	if err := store.PromoteFromEphemeral(ctx, "mixed-promote-aux", "tester"); err != nil {
		t.Fatalf("PromoteFromEphemeral: %v", err)
	}

	var dirty int
	err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dolt_status s
		WHERE NOT EXISTS (
			SELECT 1 FROM dolt_ignore di
			WHERE di.ignored = 1
			  AND s.table_name LIKE di.pattern
		)
	`).Scan(&dirty)
	if err != nil {
		t.Fatalf("query dolt_status after promote: %v", err)
	}
	if dirty != 0 {
		t.Fatalf("promotion left %d committable table(s) dirty", dirty)
	}
}

func TestGetReadyWorkIncludesNoHistoryWispsByDefault(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	noHistory := &types.Issue{
		ID:        "mixed-ready-no-history",
		Title:     "no-history ready work",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		NoHistory: true,
	}
	if err := store.CreateIssue(ctx, noHistory, "tester"); err != nil {
		t.Fatalf("CreateIssue no-history: %v", err)
	}
	createWisp(t, ctx, store, "mixed-ready-ephemeral")

	ready, err := store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("GetReadyWork: %v", err)
	}
	ids := issueIDs(ready)
	if !containsID(ids, "mixed-ready-no-history") {
		t.Fatalf("GetReadyWork default omitted no-history wisp; got %v", ids)
	}
	if containsID(ids, "mixed-ready-ephemeral") {
		t.Fatalf("GetReadyWork default included ephemeral wisp; got %v", ids)
	}
}

func TestGetReadyWorkExcludesBlockedNoHistoryWisp(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	noHistory := &types.Issue{
		ID:        "mixed-ready-blocked-no-history",
		Title:     "blocked no-history ready work",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		NoHistory: true,
	}
	if err := store.CreateIssue(ctx, noHistory, "tester"); err != nil {
		t.Fatalf("CreateIssue no-history: %v", err)
	}
	createPerm(t, ctx, store, "mixed-ready-blocker")
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     "mixed-ready-blocked-no-history",
		DependsOnID: "mixed-ready-blocker",
		Type:        types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("AddDependency blocker: %v", err)
	}

	ready, err := store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("GetReadyWork: %v", err)
	}
	ids := issueIDs(ready)
	if containsID(ids, "mixed-ready-blocked-no-history") {
		t.Fatalf("GetReadyWork returned blocked no-history wisp; got %v", ids)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
