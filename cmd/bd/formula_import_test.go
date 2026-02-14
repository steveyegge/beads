package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/testutil/teststore"

	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/types"
)

// setupFormulaTestDB creates a test database with issue_prefix configured.
func setupFormulaTestDB(t *testing.T) (storage.Storage, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "bd-formula-import-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	s := teststore.New(t)

	ctx := context.Background()
	if err := s.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		s.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	cleanup := func() {
		s.Close()
		os.RemoveAll(tmpDir)
	}

	return s, cleanup
}

// makeTestFormula creates a minimal valid formula for testing.
func makeTestFormula(name, desc string) *formula.Formula {
	return &formula.Formula{
		Formula:     name,
		Description: desc,
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{ID: "step1", Title: "Step 1"},
		},
	}
}

// setFormulaTestGlobals sets the global variables needed by saveFormulaToDB.
func setFormulaTestGlobals(t *testing.T, s storage.Storage) func() {
	t.Helper()
	oldStore := store
	oldActor := actor
	oldRootCtx := rootCtx
	oldDaemonClient := daemonClient
	oldImportForce := importForce

	store = s
	actor = "tester"
	rootCtx = context.Background()
	daemonClient = nil
	importForce = false

	return func() {
		store = oldStore
		actor = oldActor
		rootCtx = oldRootCtx
		daemonClient = oldDaemonClient
		importForce = oldImportForce
	}
}

func TestSaveFormulaToDB_NewFormula(t *testing.T) {
	s, cleanup := setupFormulaTestDB(t)
	defer cleanup()

	restore := setFormulaTestGlobals(t, s)
	defer restore()

	f := makeTestFormula("mol-test-import", "A test import formula")
	result, err := saveFormulaToDB(f)
	if err != nil {
		t.Fatalf("saveFormulaToDB failed: %v", err)
	}

	if result.ID == "" {
		t.Error("Expected non-empty ID")
	}
	if !result.Created {
		t.Error("Expected Created=true for new formula")
	}
	if result.Name != "mol-test-import" {
		t.Errorf("Expected name 'mol-test-import', got %q", result.Name)
	}

	// Verify it was created in the database
	ctx := context.Background()
	issue, err := s.GetIssue(ctx, result.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if issue == nil {
		t.Fatal("Expected issue to exist in database")
	}
	if issue.IssueType != types.TypeFormula {
		t.Errorf("Expected type formula, got %s", issue.IssueType)
	}
	if issue.Title != "mol-test-import" {
		t.Errorf("Expected title 'mol-test-import', got %q", issue.Title)
	}

	// Verify round-trip: can deserialize back to formula
	roundTripped, err := formula.IssueToFormula(issue)
	if err != nil {
		t.Fatalf("IssueToFormula failed: %v", err)
	}
	if roundTripped.Formula != "mol-test-import" {
		t.Errorf("Expected formula name 'mol-test-import', got %q", roundTripped.Formula)
	}
}

func TestSaveFormulaToDB_DuplicateError(t *testing.T) {
	s, cleanup := setupFormulaTestDB(t)
	defer cleanup()

	restore := setFormulaTestGlobals(t, s)
	defer restore()

	f := makeTestFormula("mol-dup-test", "Duplicate test")

	// Import first time
	result1, err := saveFormulaToDB(f)
	if err != nil {
		t.Fatalf("First import failed: %v", err)
	}
	if !result1.Created {
		t.Error("First import should report Created=true")
	}

	// Import same formula again without force => error
	_, err = saveFormulaToDB(f)
	if err == nil {
		t.Error("Expected error on duplicate import without force")
	}
}

func TestSaveFormulaToDB_ForceUpdate(t *testing.T) {
	s, cleanup := setupFormulaTestDB(t)
	defer cleanup()

	restore := setFormulaTestGlobals(t, s)
	defer restore()

	// Import original
	f1 := makeTestFormula("mol-force-test", "Original description")
	result1, err := saveFormulaToDB(f1)
	if err != nil {
		t.Fatalf("First import failed: %v", err)
	}

	// Import updated version with force
	importForce = true
	f2 := makeTestFormula("mol-force-test", "Updated description")
	result2, err := saveFormulaToDB(f2)
	if err != nil {
		t.Fatalf("Force import failed: %v", err)
	}
	if result2.Created {
		t.Error("Expected Created=false for force update")
	}
	if result2.ID != result1.ID {
		t.Errorf("Expected same ID %q, got %q", result1.ID, result2.ID)
	}

	// Verify the description was updated
	ctx := context.Background()
	issue, err := s.GetIssue(ctx, result2.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if issue.Description != "Updated description" {
		t.Errorf("Expected updated description, got %q", issue.Description)
	}
}

func TestSaveFormulaToDB_Labels(t *testing.T) {
	s, cleanup := setupFormulaTestDB(t)
	defer cleanup()

	restore := setFormulaTestGlobals(t, s)
	defer restore()

	f := &formula.Formula{
		Formula:     "mol-label-test",
		Description: "Label test formula",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Phase:       "liquid",
		Steps: []*formula.Step{
			{ID: "step1", Title: "Step 1"},
		},
	}

	result, err := saveFormulaToDB(f)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	ctx := context.Background()
	labels, err := s.GetLabels(ctx, result.ID)
	if err != nil {
		t.Fatalf("GetLabels failed: %v", err)
	}

	hasTypeLabel := false
	hasPhaseLabel := false
	for _, l := range labels {
		if l == "formula-type:workflow" {
			hasTypeLabel = true
		}
		if l == "phase:liquid" {
			hasPhaseLabel = true
		}
	}
	if !hasTypeLabel {
		t.Error("Expected label 'formula-type:workflow'")
	}
	if !hasPhaseLabel {
		t.Error("Expected label 'phase:liquid'")
	}
}

func TestListFormulasFromDB(t *testing.T) {
	s, cleanup := setupFormulaTestDB(t)
	defer cleanup()

	restore := setFormulaTestGlobals(t, s)
	defer restore()

	// Import some formulas
	formulas := []*formula.Formula{
		{Formula: "wf-one", Description: "Workflow one", Version: 1, Type: formula.TypeWorkflow,
			Steps: []*formula.Step{{ID: "s1", Title: "S1"}}},
		{Formula: "exp-one", Description: "Expansion one", Version: 1, Type: formula.TypeExpansion,
			Steps: []*formula.Step{{ID: "s1", Title: "S1"}}},
		{Formula: "wf-two", Description: "Workflow two", Version: 1, Type: formula.TypeWorkflow,
			Steps: []*formula.Step{{ID: "s1", Title: "S1"}}},
	}

	for _, f := range formulas {
		if _, err := saveFormulaToDB(f); err != nil {
			t.Fatalf("Import failed for %s: %v", f.Formula, err)
		}
	}

	// List all formulas
	entries := listFormulasFromDB("")
	if len(entries) != 3 {
		t.Errorf("Expected 3 formulas, got %d", len(entries))
	}

	// List with type filter
	wfEntries := listFormulasFromDB("workflow")
	if len(wfEntries) != 2 {
		t.Errorf("Expected 2 workflow formulas, got %d", len(wfEntries))
	}

	expEntries := listFormulasFromDB("expansion")
	if len(expEntries) != 1 {
		t.Errorf("Expected 1 expansion formula, got %d", len(expEntries))
	}
}

func TestLoadFormulaFromDBForShow(t *testing.T) {
	s, cleanup := setupFormulaTestDB(t)
	defer cleanup()

	restore := setFormulaTestGlobals(t, s)
	defer restore()

	f := makeTestFormula("mol-show-test", "Show test formula")
	result, err := saveFormulaToDB(f)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Load by bead ID
	loaded := loadFormulaFromDBForShow(result.ID)
	if loaded == nil {
		t.Fatal("Expected to load formula by ID")
	}
	if loaded.Formula != "mol-show-test" {
		t.Errorf("Expected formula name 'mol-show-test', got %q", loaded.Formula)
	}

	// Load by formula name
	loaded = loadFormulaFromDBForShow("mol-show-test")
	if loaded == nil {
		t.Fatal("Expected to load formula by name")
	}
	if loaded.Formula != "mol-show-test" {
		t.Errorf("Expected formula name 'mol-show-test', got %q", loaded.Formula)
	}

	// Load non-existent formula
	loaded = loadFormulaFromDBForShow("nonexistent")
	if loaded != nil {
		t.Error("Expected nil for non-existent formula")
	}
}

func TestLoadFormulaByNameOrPath_FilePath(t *testing.T) {
	// Create a temp formula file
	tmpDir, err := os.MkdirTemp("", "bd-formula-load-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	f := makeTestFormula("mol-file-test", "File test formula")
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Failed to marshal formula: %v", err)
	}

	filePath := filepath.Join(tmpDir, "mol-file-test.formula.json")
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		t.Fatalf("Failed to write formula file: %v", err)
	}

	// Load by file path
	loaded, err := loadFormulaByNameOrPath(filePath)
	if err != nil {
		t.Fatalf("loadFormulaByNameOrPath failed: %v", err)
	}
	if loaded.Formula != "mol-file-test" {
		t.Errorf("Expected formula name 'mol-file-test', got %q", loaded.Formula)
	}
}
