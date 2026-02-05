package rpc

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/formula"
)

// testFormula creates a minimal valid formula JSON for testing.
func testFormula(name, description string) json.RawMessage {
	f := formula.Formula{
		Formula:     name,
		Description: description,
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{ID: "step1", Title: "Step 1"},
		},
	}
	data, _ := json.Marshal(f)
	return data
}

func TestFormulaListRPC_Empty(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	result, err := client.FormulaList(&FormulaListArgs{})
	if err != nil {
		t.Fatalf("FormulaList failed: %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Expected 0 formulas, got %d", result.Count)
	}
}

func TestFormulaSaveAndGetRPC(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save a formula
	formulaJSON := testFormula("mol-test-workflow", "A test workflow formula")
	saveResult, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: formulaJSON,
	})
	if err != nil {
		t.Fatalf("FormulaSave failed: %v", err)
	}
	if saveResult.Name != "mol-test-workflow" {
		t.Errorf("Expected name 'mol-test-workflow', got %q", saveResult.Name)
	}
	if !saveResult.Created {
		t.Error("Expected created=true for new formula")
	}
	if saveResult.ID == "" {
		t.Error("Expected non-empty ID")
	}

	// Get by ID
	getResult, err := client.FormulaGet(&FormulaGetArgs{ID: saveResult.ID})
	if err != nil {
		t.Fatalf("FormulaGet by ID failed: %v", err)
	}
	if getResult.Name != "mol-test-workflow" {
		t.Errorf("Expected name 'mol-test-workflow', got %q", getResult.Name)
	}
	if getResult.ID != saveResult.ID {
		t.Errorf("ID mismatch: expected %q, got %q", saveResult.ID, getResult.ID)
	}

	// Verify formula content can be deserialized
	var f formula.Formula
	if err := json.Unmarshal(getResult.Formula, &f); err != nil {
		t.Fatalf("Failed to unmarshal formula content: %v", err)
	}
	if f.Formula != "mol-test-workflow" {
		t.Errorf("Formula name mismatch: expected 'mol-test-workflow', got %q", f.Formula)
	}

	// Get by name
	getByName, err := client.FormulaGet(&FormulaGetArgs{Name: "mol-test-workflow"})
	if err != nil {
		t.Fatalf("FormulaGet by name failed: %v", err)
	}
	if getByName.ID != saveResult.ID {
		t.Errorf("Get by name returned wrong ID: expected %q, got %q", saveResult.ID, getByName.ID)
	}
}

func TestFormulaListRPC_WithFormulas(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save two formulas
	_, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-alpha", "Alpha workflow"),
	})
	if err != nil {
		t.Fatalf("FormulaSave 1 failed: %v", err)
	}

	_, err = client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-beta", "Beta workflow"),
	})
	if err != nil {
		t.Fatalf("FormulaSave 2 failed: %v", err)
	}

	// List all
	result, err := client.FormulaList(&FormulaListArgs{})
	if err != nil {
		t.Fatalf("FormulaList failed: %v", err)
	}
	if result.Count != 2 {
		t.Errorf("Expected 2 formulas, got %d", result.Count)
	}

	// List with limit
	limitResult, err := client.FormulaList(&FormulaListArgs{Limit: 1})
	if err != nil {
		t.Fatalf("FormulaList with limit failed: %v", err)
	}
	if limitResult.Count != 1 {
		t.Errorf("Expected 1 formula with limit=1, got %d", limitResult.Count)
	}
}

func TestFormulaListRPC_TypeFilter(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save a workflow formula
	_, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-workflow", "A workflow"),
	})
	if err != nil {
		t.Fatalf("FormulaSave failed: %v", err)
	}

	// Filter by type (workflow should match)
	result, err := client.FormulaList(&FormulaListArgs{Type: "workflow"})
	if err != nil {
		t.Fatalf("FormulaList with type filter failed: %v", err)
	}
	if result.Count != 1 {
		t.Errorf("Expected 1 workflow formula, got %d", result.Count)
	}

	// Filter by non-matching type
	result2, err := client.FormulaList(&FormulaListArgs{Type: "expansion"})
	if err != nil {
		t.Fatalf("FormulaList with expansion filter failed: %v", err)
	}
	if result2.Count != 0 {
		t.Errorf("Expected 0 expansion formulas, got %d", result2.Count)
	}
}

func TestFormulaSaveRPC_DuplicateWithoutForce(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	formulaJSON := testFormula("mol-duplicate", "First version")

	// Save first time
	_, err := client.FormulaSave(&FormulaSaveArgs{Formula: formulaJSON})
	if err != nil {
		t.Fatalf("First FormulaSave failed: %v", err)
	}

	// Save again without force should fail
	_, err = client.FormulaSave(&FormulaSaveArgs{Formula: formulaJSON})
	if err == nil {
		t.Fatal("Expected error saving duplicate formula without force")
	}
}

func TestFormulaSaveRPC_DuplicateWithForce(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save first version
	saveResult1, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-overwrite", "First version"),
	})
	if err != nil {
		t.Fatalf("First FormulaSave failed: %v", err)
	}
	if !saveResult1.Created {
		t.Error("Expected created=true for first save")
	}

	// Save again with force
	saveResult2, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-overwrite", "Updated version"),
		Force:   true,
	})
	if err != nil {
		t.Fatalf("FormulaSave with force failed: %v", err)
	}
	if saveResult2.Created {
		t.Error("Expected created=false for update")
	}

	// Verify content was updated
	getResult, err := client.FormulaGet(&FormulaGetArgs{ID: saveResult2.ID})
	if err != nil {
		t.Fatalf("FormulaGet after update failed: %v", err)
	}
	var f formula.Formula
	if err := json.Unmarshal(getResult.Formula, &f); err != nil {
		t.Fatalf("Failed to unmarshal updated formula: %v", err)
	}
	if f.Description != "Updated version" {
		t.Errorf("Expected description 'Updated version', got %q", f.Description)
	}
}

func TestFormulaSaveRPC_InvalidFormula(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Missing required fields
	_, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: json.RawMessage(`{"version": 0}`),
	})
	if err == nil {
		t.Fatal("Expected error saving invalid formula")
	}
}

func TestFormulaDeleteRPC(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save a formula
	saveResult, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-to-delete", "Will be deleted"),
	})
	if err != nil {
		t.Fatalf("FormulaSave failed: %v", err)
	}

	// Delete by ID
	delResult, err := client.FormulaDelete(&FormulaDeleteArgs{
		ID:     saveResult.ID,
		Reason: "no longer needed",
	})
	if err != nil {
		t.Fatalf("FormulaDelete failed: %v", err)
	}
	if delResult.ID != saveResult.ID {
		t.Errorf("Delete ID mismatch: expected %q, got %q", saveResult.ID, delResult.ID)
	}

	// Verify formula no longer appears in list
	listResult, err := client.FormulaList(&FormulaListArgs{})
	if err != nil {
		t.Fatalf("FormulaList after delete failed: %v", err)
	}
	if listResult.Count != 0 {
		t.Errorf("Expected 0 formulas after delete, got %d", listResult.Count)
	}

	// Verify get by name fails
	_, err = client.FormulaGet(&FormulaGetArgs{Name: "mol-to-delete"})
	if err == nil {
		t.Fatal("Expected error getting deleted formula by name")
	}
}

func TestFormulaDeleteRPC_ByName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-name-delete", "Delete by name"),
	})
	if err != nil {
		t.Fatalf("FormulaSave failed: %v", err)
	}

	delResult, err := client.FormulaDelete(&FormulaDeleteArgs{Name: "mol-name-delete"})
	if err != nil {
		t.Fatalf("FormulaDelete by name failed: %v", err)
	}
	if delResult.Name != "mol-name-delete" {
		t.Errorf("Expected name 'mol-name-delete', got %q", delResult.Name)
	}
}

func TestFormulaDeleteRPC_NotFound(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FormulaDelete(&FormulaDeleteArgs{Name: "nonexistent"})
	if err == nil {
		t.Fatal("Expected error deleting nonexistent formula")
	}
}

func TestFormulaGetRPC_NotFound(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FormulaGet(&FormulaGetArgs{ID: "nonexistent-id"})
	if err == nil {
		t.Fatal("Expected error getting nonexistent formula by ID")
	}

	_, err = client.FormulaGet(&FormulaGetArgs{Name: "nonexistent"})
	if err == nil {
		t.Fatal("Expected error getting nonexistent formula by name")
	}
}

func TestFormulaGetRPC_MissingArgs(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FormulaGet(&FormulaGetArgs{})
	if err == nil {
		t.Fatal("Expected error when neither id nor name provided")
	}
}

func TestFormulaDeleteRPC_MissingArgs(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FormulaDelete(&FormulaDeleteArgs{})
	if err == nil {
		t.Fatal("Expected error when neither id nor name provided")
	}
}

func TestFormulaSaveRPC_EmptyFormula(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FormulaSave(&FormulaSaveArgs{})
	if err == nil {
		t.Fatal("Expected error saving empty formula")
	}
}

func TestFormulaDeleteRPC_AlreadyDeleted(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	saveResult, err := client.FormulaSave(&FormulaSaveArgs{
		Formula: testFormula("mol-double-delete", "Delete twice"),
	})
	if err != nil {
		t.Fatalf("FormulaSave failed: %v", err)
	}

	// Delete once
	_, err = client.FormulaDelete(&FormulaDeleteArgs{ID: saveResult.ID})
	if err != nil {
		t.Fatalf("First delete failed: %v", err)
	}

	// Delete again should fail
	_, err = client.FormulaDelete(&FormulaDeleteArgs{ID: saveResult.ID})
	if err == nil {
		t.Fatal("Expected error deleting already-deleted formula")
	}
}
