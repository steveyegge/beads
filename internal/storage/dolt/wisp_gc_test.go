package dolt

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestFindWispDependentsRecursive verifies that FindWispDependentsRecursive
// correctly discovers all transitive wisp dependents. This is the core logic
// for cascade-deleting blocked step children during wisp GC (bd-7hjy).
func TestFindWispDependentsRecursive(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create a parent wisp (simulates a formula root)
	parent := &types.Issue{
		Title:     "parent formula wisp",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.createWisp(ctx, parent, "test"); err != nil {
		t.Fatalf("create parent wisp: %v", err)
	}

	// Create child wisps (simulate formula step wisps that depend on parent)
	child1 := &types.Issue{
		Title:     "step 1",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	child2 := &types.Issue{
		Title:     "step 2",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.createWisp(ctx, child1, "test"); err != nil {
		t.Fatalf("create child1: %v", err)
	}
	if err := store.createWisp(ctx, child2, "test"); err != nil {
		t.Fatalf("create child2: %v", err)
	}

	// Create a grandchild (step that depends on child1)
	grandchild := &types.Issue{
		Title:     "substep of step 1",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.createWisp(ctx, grandchild, "test"); err != nil {
		t.Fatalf("create grandchild: %v", err)
	}

	// Set up dependency links: children depend on parent, grandchild depends on child1
	deps := []*types.Dependency{
		{IssueID: child1.ID, DependsOnID: parent.ID, Type: types.DepBlocks},
		{IssueID: child2.ID, DependsOnID: parent.ID, Type: types.DepBlocks},
		{IssueID: grandchild.ID, DependsOnID: child1.ID, Type: types.DepBlocks},
	}
	for _, dep := range deps {
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("AddDependency %s -> %s: %v", dep.IssueID, dep.DependsOnID, err)
		}
	}

	// Find all dependents starting from the parent
	discovered, err := store.FindWispDependentsRecursive(ctx, []string{parent.ID})
	if err != nil {
		t.Fatalf("FindWispDependentsRecursive: %v", err)
	}

	// Should discover child1, child2, and grandchild (3 dependents)
	if len(discovered) != 3 {
		t.Errorf("expected 3 dependents, got %d: %v", len(discovered), discovered)
	}
	for _, id := range []string{child1.ID, child2.ID, grandchild.ID} {
		if !discovered[id] {
			t.Errorf("expected dependent %s to be discovered", id)
		}
	}

	// Parent should NOT be in the discovered set (it was an input)
	if discovered[parent.ID] {
		t.Errorf("parent %s should not be in discovered set", parent.ID)
	}
}

// TestFindWispDependentsRecursive_Empty verifies empty input returns nil.
func TestFindWispDependentsRecursive_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	discovered, err := store.FindWispDependentsRecursive(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if discovered != nil {
		t.Errorf("expected nil, got %v", discovered)
	}
}

// TestFindWispDependentsRecursive_NoDependents verifies wisps with no
// dependents return an empty map.
func TestFindWispDependentsRecursive_NoDependents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	wisp := &types.Issue{
		Title:     "lone wisp",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.createWisp(ctx, wisp, "test"); err != nil {
		t.Fatalf("create wisp: %v", err)
	}

	discovered, err := store.FindWispDependentsRecursive(ctx, []string{wisp.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(discovered) != 0 {
		t.Errorf("expected 0 dependents, got %d: %v", len(discovered), discovered)
	}
}
