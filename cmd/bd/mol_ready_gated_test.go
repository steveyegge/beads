//go:build cgo

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// setupGatedTestDB creates a test store on the suite's Dolt test server
// (skipped when it is not running), with the orchestrator custom types
// newTestStore configures. Closed by t.Cleanup; the returned cleanup is a
// no-op kept for the callers' defer.
func setupGatedTestDB(t *testing.T) (*dolt.DoltStore, func()) {
	t.Helper()
	store := newTestStore(t, filepath.Join(t.TempDir(), "test.db"))
	return store, func() {}
}

// =============================================================================
// mol ready --gated Tests (bd-lhalq: Gate-resume discovery)
// =============================================================================

// TestFindGateReadyMolecules_NoGates tests finding gate-ready molecules when no gates exist
func TestFindGateReadyMolecules_NoGates(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupGatedTestDB(t)
	defer cleanup()

	// Create a regular molecule (no gates)
	mol := &types.Issue{
		ID:        "test-mol-001",
		Title:     "Test Molecule",
		IssueType: types.TypeEpic,
		Status:    types.StatusInProgress,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	step := &types.Issue{
		ID:        "test-mol-001.step1",
		Title:     "Step 1",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := store.CreateIssue(ctx, mol, "test"); err != nil {
		t.Fatalf("Failed to create molecule: %v", err)
	}
	if err := store.CreateIssue(ctx, step, "test"); err != nil {
		t.Fatalf("Failed to create step: %v", err)
	}

	// Add parent-child relationship
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     step.ID,
		DependsOnID: mol.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Find gate-ready molecules
	molecules, err := findGateReadyMolecules(ctx, store)
	if err != nil {
		t.Fatalf("findGateReadyMolecules failed: %v", err)
	}

	if len(molecules) != 0 {
		t.Errorf("Expected 0 gate-ready molecules, got %d", len(molecules))
	}
}

// TestFindGateReadyMolecules_ClosedGate tests finding molecules with closed gates
func TestFindGateReadyMolecules_ClosedGate(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupGatedTestDB(t)
	defer cleanup()

	// Create molecule structure:
	// mol-001
	//   └── gate-await-ci (closed)
	//   └── step1 (blocked by gate-await-ci, should become ready)

	mol := &types.Issue{
		ID:        "test-mol-002",
		Title:     "Test Molecule with Gate",
		IssueType: types.TypeEpic,
		Status:    types.StatusInProgress,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	gate := &types.Issue{
		ID:        "test-mol-002.gate-await-ci",
		Title:     "Gate: gh:run ci-workflow",
		IssueType: "gate",
		Status:    types.StatusClosed, // Gate has closed
		AwaitType: "gh:run",
		AwaitID:   "ci-workflow",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	step := &types.Issue{
		ID:        "test-mol-002.step1",
		Title:     "Deploy after CI",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := store.CreateIssue(ctx, mol, "test"); err != nil {
		t.Fatalf("Failed to create molecule: %v", err)
	}
	if err := store.CreateIssue(ctx, gate, "test"); err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	if err := store.CreateIssue(ctx, step, "test"); err != nil {
		t.Fatalf("Failed to create step: %v", err)
	}

	// Add parent-child relationships
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     gate.ID,
		DependsOnID: mol.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add gate parent-child: %v", err)
	}
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     step.ID,
		DependsOnID: mol.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add step parent-child: %v", err)
	}

	// Add blocking dependency: step depends on gate (gate blocks step)
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     step.ID,
		DependsOnID: gate.ID,
		Type:        types.DepBlocks,
	}, "test"); err != nil {
		t.Fatalf("Failed to add blocking dependency: %v", err)
	}

	// Find gate-ready molecules
	molecules, err := findGateReadyMolecules(ctx, store)
	if err != nil {
		t.Fatalf("findGateReadyMolecules failed: %v", err)
	}

	if len(molecules) != 1 {
		t.Errorf("Expected 1 gate-ready molecule, got %d", len(molecules))
		return
	}

	if molecules[0].MoleculeID != mol.ID {
		t.Errorf("Expected molecule ID %s, got %s", mol.ID, molecules[0].MoleculeID)
	}
	if molecules[0].ClosedGate == nil {
		t.Error("Expected closed gate to be set")
	} else if molecules[0].ClosedGate.ID != gate.ID {
		t.Errorf("Expected closed gate ID %s, got %s", gate.ID, molecules[0].ClosedGate.ID)
	}
	if molecules[0].ReadyStep == nil {
		t.Error("Expected ready step to be set")
	} else if molecules[0].ReadyStep.ID != step.ID {
		t.Errorf("Expected ready step ID %s, got %s", step.ID, molecules[0].ReadyStep.ID)
	}
}

// TestFindGateReadyMolecules_OpenGate tests that open gates don't trigger ready
func TestFindGateReadyMolecules_OpenGate(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupGatedTestDB(t)
	defer cleanup()

	// Create molecule with OPEN gate
	mol := &types.Issue{
		ID:        "test-mol-003",
		Title:     "Test Molecule with Open Gate",
		IssueType: types.TypeEpic,
		Status:    types.StatusInProgress,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	gate := &types.Issue{
		ID:        "test-mol-003.gate-await-ci",
		Title:     "Gate: gh:run ci-workflow",
		IssueType: "gate",
		Status:    types.StatusOpen, // Gate is still open
		AwaitType: "gh:run",
		AwaitID:   "ci-workflow",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	step := &types.Issue{
		ID:        "test-mol-003.step1",
		Title:     "Deploy after CI",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := store.CreateIssue(ctx, mol, "test"); err != nil {
		t.Fatalf("Failed to create molecule: %v", err)
	}
	if err := store.CreateIssue(ctx, gate, "test"); err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	if err := store.CreateIssue(ctx, step, "test"); err != nil {
		t.Fatalf("Failed to create step: %v", err)
	}

	// Add parent-child relationships
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     gate.ID,
		DependsOnID: mol.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add gate parent-child: %v", err)
	}
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     step.ID,
		DependsOnID: mol.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add step parent-child: %v", err)
	}

	// Add blocking dependency: step depends on gate
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     step.ID,
		DependsOnID: gate.ID,
		Type:        types.DepBlocks,
	}, "test"); err != nil {
		t.Fatalf("Failed to add blocking dependency: %v", err)
	}

	// Find gate-ready molecules
	molecules, err := findGateReadyMolecules(ctx, store)
	if err != nil {
		t.Fatalf("findGateReadyMolecules failed: %v", err)
	}

	if len(molecules) != 0 {
		t.Errorf("Expected 0 gate-ready molecules (gate is open), got %d", len(molecules))
	}
}

// TestFindGateReadyMolecules_HookedMolecule tests that hooked molecules are filtered out
func TestFindGateReadyMolecules_HookedMolecule(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupGatedTestDB(t)
	defer cleanup()

	// Create molecule with closed gate, but molecule is hooked
	mol := &types.Issue{
		ID:        "test-mol-004",
		Title:     "Test Hooked Molecule",
		IssueType: types.TypeEpic,
		Status:    types.StatusHooked, // Already hooked by an agent
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	gate := &types.Issue{
		ID:        "test-mol-004.gate-await-ci",
		Title:     "Gate: gh:run ci-workflow",
		IssueType: "gate",
		Status:    types.StatusClosed,
		AwaitType: "gh:run",
		AwaitID:   "ci-workflow",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	step := &types.Issue{
		ID:        "test-mol-004.step1",
		Title:     "Deploy after CI",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := store.CreateIssue(ctx, mol, "test"); err != nil {
		t.Fatalf("Failed to create molecule: %v", err)
	}
	if err := store.CreateIssue(ctx, gate, "test"); err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	if err := store.CreateIssue(ctx, step, "test"); err != nil {
		t.Fatalf("Failed to create step: %v", err)
	}

	// Add parent-child relationships
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     gate.ID,
		DependsOnID: mol.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add gate parent-child: %v", err)
	}
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     step.ID,
		DependsOnID: mol.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add step parent-child: %v", err)
	}

	// Add blocking dependency
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     step.ID,
		DependsOnID: gate.ID,
		Type:        types.DepBlocks,
	}, "test"); err != nil {
		t.Fatalf("Failed to add blocking dependency: %v", err)
	}

	// Find gate-ready molecules
	molecules, err := findGateReadyMolecules(ctx, store)
	if err != nil {
		t.Fatalf("findGateReadyMolecules failed: %v", err)
	}

	if len(molecules) != 0 {
		t.Errorf("Expected 0 gate-ready molecules (molecule is hooked), got %d", len(molecules))
	}
}

// TestFindGateReadyMolecules_MultipleGates tests handling multiple closed gates
func TestFindGateReadyMolecules_MultipleGates(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupGatedTestDB(t)
	defer cleanup()

	// Create two molecules, each with a closed gate
	for i := 1; i <= 2; i++ {
		molID := fmt.Sprintf("test-multi-%d", i)
		mol := &types.Issue{
			ID:        molID,
			Title:     fmt.Sprintf("Multi Gate Mol %d", i),
			IssueType: types.TypeEpic,
			Status:    types.StatusInProgress,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		gate := &types.Issue{
			ID:        fmt.Sprintf("%s.gate", molID),
			Title:     "Gate: gh:run",
			IssueType: "gate",
			Status:    types.StatusClosed,
			AwaitType: "gh:run",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		step := &types.Issue{
			ID:        fmt.Sprintf("%s.step1", molID),
			Title:     "Step 1",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := store.CreateIssue(ctx, mol, "test"); err != nil {
			t.Fatalf("Failed to create molecule %d: %v", i, err)
		}
		if err := store.CreateIssue(ctx, gate, "test"); err != nil {
			t.Fatalf("Failed to create gate %d: %v", i, err)
		}
		if err := store.CreateIssue(ctx, step, "test"); err != nil {
			t.Fatalf("Failed to create step %d: %v", i, err)
		}

		// Add dependencies
		if err := store.AddDependency(ctx, &types.Dependency{
			IssueID:     gate.ID,
			DependsOnID: mol.ID,
			Type:        types.DepParentChild,
		}, "test"); err != nil {
			t.Fatalf("Failed to add gate parent-child %d: %v", i, err)
		}
		if err := store.AddDependency(ctx, &types.Dependency{
			IssueID:     step.ID,
			DependsOnID: mol.ID,
			Type:        types.DepParentChild,
		}, "test"); err != nil {
			t.Fatalf("Failed to add step parent-child %d: %v", i, err)
		}
		if err := store.AddDependency(ctx, &types.Dependency{
			IssueID:     step.ID,
			DependsOnID: gate.ID,
			Type:        types.DepBlocks,
		}, "test"); err != nil {
			t.Fatalf("Failed to add blocking dep %d: %v", i, err)
		}
	}

	// Find gate-ready molecules
	molecules, err := findGateReadyMolecules(ctx, store)
	if err != nil {
		t.Fatalf("findGateReadyMolecules failed: %v", err)
	}

	if len(molecules) != 2 {
		t.Errorf("Expected 2 gate-ready molecules, got %d", len(molecules))
	}
}

// TestFindGateReadyMolecules_ExitGateDownstream covers the GH#3316 compound
// exit-gate form end to end, against the real cook code path: the issue-repro
// formula (prep -> type=gate agree with a human [steps.gate] -> execute) is
// cooked and poured into the store, then the workflow is walked — prep
// closes, the agree prep task closes, the human companion resolves — and
// findGateReadyMolecules must report execute as the molecule's ready step
// only after the companion resolves. Before that, execute is blocked by the
// open gate and no gate-resume molecule is listed.
func TestFindGateReadyMolecules_ExitGateDownstream(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupGatedTestDB(t)
	defer cleanup()

	f := &formula.Formula{
		Formula:     "mol-exit-gate",
		Description: "GH#3316 issue repro",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{ID: "prep", Title: "Prep evidence"},
			{
				ID:        "agree",
				Title:     "Human approves",
				Type:      "gate",
				DependsOn: []string{"prep"},
				Gate:      &formula.Gate{Type: "human"},
			},
			{ID: "execute", Title: "Do the thing", DependsOn: []string{"agree"}},
		},
	}
	subgraph, err := cookFormulaToSubgraph(f, "mol-exit-gate")
	if err != nil {
		t.Fatalf("cookFormulaToSubgraph failed: %v", err)
	}
	result, err := cloneSubgraph(ctx, store, subgraph, CloneOptions{Actor: "test"})
	if err != nil {
		t.Fatalf("cloneSubgraph failed: %v", err)
	}
	pouredID := func(t *testing.T, templateID string) string {
		t.Helper()
		id, ok := result.IDMapping[templateID]
		if !ok {
			t.Fatalf("poured id for %q missing from IDMapping %v", templateID, result.IDMapping)
		}
		return id
	}
	prepID := pouredID(t, "mol-exit-gate.prep")
	agreeID := pouredID(t, "mol-exit-gate.agree")
	gateID := pouredID(t, "mol-exit-gate.gate-agree")
	executeID := pouredID(t, "mol-exit-gate.execute")

	// The poured agree bead is the prep task; the companion carries the await.
	agreeIssue, err := store.GetIssue(ctx, agreeID)
	if err != nil {
		t.Fatalf("get agree: %v", err)
	}
	if agreeIssue.IssueType != types.TypeTask {
		t.Errorf("poured agree bead type = %q, want %q", agreeIssue.IssueType, types.TypeTask)
	}
	gateIssue, err := store.GetIssue(ctx, gateID)
	if err != nil {
		t.Fatalf("get gate: %v", err)
	}
	if gateIssue.IssueType != types.TypeGate || gateIssue.AwaitType != "human" {
		t.Errorf("poured companion = type %q await %q, want gate/human", gateIssue.IssueType, gateIssue.AwaitType)
	}

	// Walk the workflow: prep closes, then the agree prep task closes.
	if err := store.CloseIssue(ctx, prepID, "evidence gathered", "test", ""); err != nil {
		t.Fatalf("close prep: %v", err)
	}
	ready, err := store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("get ready work after prep closed: %v", err)
	}
	var agreeReady bool
	for _, issue := range ready {
		if issue.ID == agreeID {
			agreeReady = true
		}
	}
	if !agreeReady {
		t.Errorf("agree prep task %s should be ready (dispatchable task) once prep closed", agreeID)
	}
	if err := store.CloseIssue(ctx, agreeID, "template delivered", "test", ""); err != nil {
		t.Fatalf("close agree task: %v", err)
	}

	// Prep task closed but companion not yet resolved: execute is blocked by
	// the open gate and no gate-resume molecule is listed.
	ready, err = store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("get ready work before resolve: %v", err)
	}
	for _, issue := range ready {
		if issue.ID == executeID {
			t.Errorf("execute %s must not be ready before the companion resolves", executeID)
		}
	}
	molecules, err := findGateReadyMolecules(ctx, store)
	if err != nil {
		t.Fatalf("findGateReadyMolecules before resolve failed: %v", err)
	}
	if len(molecules) != 0 {
		t.Errorf("want no gate-ready molecules before the companion resolves, got %d", len(molecules))
	}

	// Resolve the human companion (what bd gate resolve closes).
	if err := store.CloseIssue(ctx, gateID, "approved", "test", ""); err != nil {
		t.Fatalf("resolve companion gate: %v", err)
	}

	molecules, err = findGateReadyMolecules(ctx, store)
	if err != nil {
		t.Fatalf("findGateReadyMolecules after resolve failed: %v", err)
	}
	if len(molecules) != 1 {
		t.Fatalf("want exactly 1 gate-ready molecule after resolve, got %d", len(molecules))
	}
	gm := molecules[0]
	if gm.ReadyStep == nil || gm.ReadyStep.ID != executeID {
		t.Errorf("ready step = %+v, want execute %s", gm.ReadyStep, executeID)
	}
	if gm.ClosedGate == nil || gm.ClosedGate.ID != gateID {
		t.Errorf("closed gate = %+v, want companion %s", gm.ClosedGate, gateID)
	}
}
