package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// setupMolTestServer creates a test server with an in-memory store (no transaction support)
func setupMolTestServer(t *testing.T) (*Server, storage.Storage) {
	t.Helper()
	store := memory.New("/tmp/test.jsonl")
	server := NewServer("/tmp/test.sock", store, "/tmp", "/tmp/test.db")
	return server, store
}

// setupMolTestServerWithSQLite creates a test server with SQLite storage (with transaction support)
func setupMolTestServerWithSQLite(t *testing.T) (*Server, storage.Storage) {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Initialize database with required config
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	server := NewServer("/tmp/test.sock", store, tmpDir, dbPath)
	return server, store
}

// createTestProto creates a proto (molecule template) in the store
func createTestProto(t *testing.T, store storage.Storage, id, title string) *types.Issue {
	t.Helper()
	proto := &types.Issue{
		ID:        id,
		Title:     title,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Labels:    []string{MoleculeLabel},
	}
	if err := store.CreateIssue(context.Background(), proto, "test"); err != nil {
		t.Fatalf("failed to create proto: %v", err)
	}
	// Labels are stored in a separate table - add them explicitly
	if err := store.AddLabel(context.Background(), proto.ID, MoleculeLabel, "test"); err != nil {
		t.Fatalf("failed to add molecule label: %v", err)
	}
	return proto
}

// createTestMolecule creates a molecule (instantiated proto) in the store
func createTestMolecule(t *testing.T, store storage.Storage, id, title string, ephemeral bool) *types.Issue {
	t.Helper()
	mol := &types.Issue{
		ID:        id,
		Title:     title,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Ephemeral: ephemeral,
	}
	if err := store.CreateIssue(context.Background(), mol, "test"); err != nil {
		t.Fatalf("failed to create molecule: %v", err)
	}
	return mol
}

// createTestChild creates a child issue linked to a parent
func createTestChild(t *testing.T, store storage.Storage, id, title, parentID string, ephemeral bool) *types.Issue {
	t.Helper()
	child := &types.Issue{
		ID:        id,
		Title:     title,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Ephemeral: ephemeral,
	}
	if err := store.CreateIssue(context.Background(), child, "test"); err != nil {
		t.Fatalf("failed to create child: %v", err)
	}
	dep := &types.Dependency{
		IssueID:     id,
		DependsOnID: parentID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(context.Background(), dep, "test"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}
	return child
}

func TestHandleMolBond_InvalidArgs(t *testing.T) {
	server, _ := setupMolTestServer(t)

	req := &Request{
		Operation: OpMolBond,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleMolBond(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleMolBond_InvalidBondType(t *testing.T) {
	server, _ := setupMolTestServer(t)

	args := MolBondArgs{
		IDa:      "bd-a",
		IDb:      "bd-b",
		BondType: "invalid",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBond,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBond(req)
	if resp.Success {
		t.Error("expected failure for invalid bond type")
	}
	if resp.Error == "" {
		t.Error("expected error message about bond type")
	}
}

func TestHandleMolBond_EphemeralAndPourConflict(t *testing.T) {
	server, _ := setupMolTestServer(t)

	args := MolBondArgs{
		IDa:       "bd-a",
		IDb:       "bd-b",
		BondType:  types.BondTypeSequential,
		Ephemeral: true,
		Pour:      true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBond,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBond(req)
	if resp.Success {
		t.Error("expected failure when both ephemeral and pour are set")
	}
}

func TestHandleMolBond_ProtoProto(t *testing.T) {
	server, store := setupMolTestServerWithSQLite(t)

	// Create two protos
	createTestProto(t, store, "bd-proto1", "Proto 1")
	createTestProto(t, store, "bd-proto2", "Proto 2")

	args := MolBondArgs{
		IDa:      "bd-proto1",
		IDb:      "bd-proto2",
		BondType: types.BondTypeSequential,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBond,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBond(req)
	if !resp.Success {
		t.Fatalf("bond failed: %s", resp.Error)
	}

	var result MolBondResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Bonding two protos creates a compound molecule (instantiated from the protos)
	if result.ResultType != "compound_molecule" && result.ResultType != "compound_proto" {
		t.Errorf("expected result type 'compound_molecule' or 'compound_proto', got %q", result.ResultType)
	}
	if result.ResultID == "" {
		t.Error("expected non-empty result ID")
	}
}

func TestHandleMolBond_DryRun(t *testing.T) {
	server, store := setupMolTestServer(t)

	createTestProto(t, store, "bd-proto1", "Proto 1")
	createTestProto(t, store, "bd-proto2", "Proto 2")

	args := MolBondArgs{
		IDa:      "bd-proto1",
		IDb:      "bd-proto2",
		BondType: types.BondTypeSequential,
		DryRun:   true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBond,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBond(req)
	if !resp.Success {
		t.Fatalf("dry-run bond failed: %s", resp.Error)
	}

	// Verify no new issues were created (dry run)
	issues, _ := store.SearchIssues(context.Background(), "", types.IssueFilter{})
	if len(issues) != 2 {
		t.Errorf("expected 2 issues (protos only), got %d", len(issues))
	}
}

func TestHandleMolSquash_InvalidArgs(t *testing.T) {
	server, _ := setupMolTestServer(t)

	req := &Request{
		Operation: OpMolSquash,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleMolSquash(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
}

func TestHandleMolSquash_MoleculeNotFound(t *testing.T) {
	server, _ := setupMolTestServer(t)

	args := MolSquashArgs{
		MoleculeID: "bd-nonexistent",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolSquash,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolSquash(req)
	if resp.Success {
		t.Error("expected failure for non-existent molecule")
	}
}

func TestHandleMolSquash_NoEphemeralChildren(t *testing.T) {
	server, store := setupMolTestServer(t)

	// Create molecule with non-ephemeral child
	createTestMolecule(t, store, "bd-mol1", "Test Molecule", false)
	createTestChild(t, store, "bd-child1", "Child 1", "bd-mol1", false)

	args := MolSquashArgs{
		MoleculeID: "bd-mol1",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolSquash,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolSquash(req)
	if !resp.Success {
		t.Fatalf("squash failed: %s", resp.Error)
	}

	var result MolSquashResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.SquashedCount != 0 {
		t.Errorf("expected 0 squashed (no ephemeral children), got %d", result.SquashedCount)
	}
}

func TestHandleMolSquash_WithEphemeralChildren(t *testing.T) {
	server, store := setupMolTestServerWithSQLite(t)

	// Create molecule with ephemeral children
	createTestMolecule(t, store, "bd-mol1", "Test Molecule", false)
	createTestChild(t, store, "bd-wisp1", "Wisp 1", "bd-mol1", true)
	createTestChild(t, store, "bd-wisp2", "Wisp 2", "bd-mol1", true)

	args := MolSquashArgs{
		MoleculeID:   "bd-mol1",
		KeepChildren: true, // Don't delete children for this test
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolSquash,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolSquash(req)
	if !resp.Success {
		t.Fatalf("squash failed: %s", resp.Error)
	}

	var result MolSquashResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.SquashedCount != 2 {
		t.Errorf("expected 2 squashed, got %d", result.SquashedCount)
	}
	if result.DigestID == "" {
		t.Error("expected digest to be created")
	}
	if !result.KeptChildren {
		t.Error("expected KeptChildren to be true")
	}
}

func TestHandleMolSquash_DryRun(t *testing.T) {
	server, store := setupMolTestServer(t)

	createTestMolecule(t, store, "bd-mol1", "Test Molecule", false)
	createTestChild(t, store, "bd-wisp1", "Wisp 1", "bd-mol1", true)

	args := MolSquashArgs{
		MoleculeID: "bd-mol1",
		DryRun:     true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolSquash,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolSquash(req)
	if !resp.Success {
		t.Fatalf("dry-run squash failed: %s", resp.Error)
	}

	// Verify no digest was created (dry run)
	issues, _ := store.SearchIssues(context.Background(), "", types.IssueFilter{})
	if len(issues) != 2 {
		t.Errorf("expected 2 issues (mol + wisp only), got %d", len(issues))
	}
}

func TestHandleMolBurn_InvalidArgs(t *testing.T) {
	server, _ := setupMolTestServer(t)

	req := &Request{
		Operation: OpMolBurn,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleMolBurn(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
}

func TestHandleMolBurn_NoMoleculeIDs(t *testing.T) {
	server, _ := setupMolTestServer(t)

	args := MolBurnArgs{
		MoleculeIDs: []string{},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBurn,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBurn(req)
	if resp.Success {
		t.Error("expected failure when no molecule IDs provided")
	}
}

func TestHandleMolBurn_MoleculeNotFound(t *testing.T) {
	server, _ := setupMolTestServer(t)

	args := MolBurnArgs{
		MoleculeIDs: []string{"bd-nonexistent"},
		DryRun:      true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBurn,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBurn(req)
	if !resp.Success {
		t.Fatalf("burn dry-run failed: %s", resp.Error)
	}

	var result MolBurnResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.FailedCount != 1 {
		t.Errorf("expected 1 failed, got %d", result.FailedCount)
	}
}

func TestHandleMolBurn_SingleMolecule(t *testing.T) {
	server, store := setupMolTestServer(t)

	// Create a molecule with children
	createTestMolecule(t, store, "bd-mol1", "Test Molecule", true)
	createTestChild(t, store, "bd-child1", "Child 1", "bd-mol1", true)
	createTestChild(t, store, "bd-child2", "Child 2", "bd-mol1", true)

	args := MolBurnArgs{
		MoleculeIDs: []string{"bd-mol1"},
		Force:       true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBurn,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBurn(req)
	if !resp.Success {
		t.Fatalf("burn failed: %s", resp.Error)
	}

	var result MolBurnResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.DeletedCount != 3 {
		t.Errorf("expected 3 deleted (mol + 2 children), got %d", result.DeletedCount)
	}

	// Verify issues were deleted
	issues, _ := store.SearchIssues(context.Background(), "", types.IssueFilter{})
	if len(issues) != 0 {
		t.Errorf("expected 0 issues after burn, got %d", len(issues))
	}
}

func TestHandleMolBurn_DryRun(t *testing.T) {
	server, store := setupMolTestServer(t)

	createTestMolecule(t, store, "bd-mol1", "Test Molecule", true)
	createTestChild(t, store, "bd-child1", "Child 1", "bd-mol1", true)

	args := MolBurnArgs{
		MoleculeIDs: []string{"bd-mol1"},
		DryRun:      true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBurn,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBurn(req)
	if !resp.Success {
		t.Fatalf("dry-run burn failed: %s", resp.Error)
	}

	// Verify issues still exist (dry run)
	issues, _ := store.SearchIssues(context.Background(), "", types.IssueFilter{})
	if len(issues) != 2 {
		t.Errorf("expected 2 issues after dry-run burn, got %d", len(issues))
	}
}

func TestHandleMolBurn_BatchMolecules(t *testing.T) {
	server, store := setupMolTestServer(t)

	// Create multiple molecules
	createTestMolecule(t, store, "bd-mol1", "Molecule 1", true)
	createTestMolecule(t, store, "bd-mol2", "Molecule 2", true)
	createTestMolecule(t, store, "bd-mol3", "Molecule 3", true)

	args := MolBurnArgs{
		MoleculeIDs: []string{"bd-mol1", "bd-mol2", "bd-mol3"},
		Force:       true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolBurn,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolBurn(req)
	if !resp.Success {
		t.Fatalf("batch burn failed: %s", resp.Error)
	}

	var result MolBurnResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.DeletedCount != 3 {
		t.Errorf("expected 3 deleted, got %d", result.DeletedCount)
	}
	if result.FailedCount != 0 {
		t.Errorf("expected 0 failed, got %d", result.FailedCount)
	}
}

func TestResolvePartialID(t *testing.T) {
	server, store := setupMolTestServer(t)

	// Create an issue
	createTestMolecule(t, store, "bd-abc123", "Test Issue", false)

	ctx := context.Background()

	// Test full ID
	resolved, err := server.resolvePartialID(ctx, "bd-abc123")
	if err != nil {
		t.Fatalf("failed to resolve full ID: %v", err)
	}
	if resolved != "bd-abc123" {
		t.Errorf("expected bd-abc123, got %s", resolved)
	}

	// Test partial ID
	resolved, err = server.resolvePartialID(ctx, "bd-abc")
	if err != nil {
		t.Fatalf("failed to resolve partial ID: %v", err)
	}
	if resolved != "bd-abc123" {
		t.Errorf("expected bd-abc123, got %s", resolved)
	}

	// Test non-existent ID
	_, err = server.resolvePartialID(ctx, "bd-nonexistent")
	if err == nil {
		t.Error("expected error for non-existent ID")
	}
}

func TestIsProto(t *testing.T) {
	server, store := setupMolTestServer(t)

	// Create a proto
	proto := createTestProto(t, store, "bd-proto1", "Proto")

	// Create a regular issue
	regular := createTestMolecule(t, store, "bd-regular1", "Regular", false)

	if !server.isProto(proto) {
		t.Error("expected proto to be identified as proto")
	}

	if server.isProto(regular) {
		t.Error("expected regular issue to not be identified as proto")
	}

	if server.isProto(nil) {
		t.Error("expected nil to not be identified as proto")
	}
}

// Tests for handleCloseContinue (bd-ympw)

func TestHandleCloseContinue_InvalidArgs(t *testing.T) {
	server, _ := setupMolTestServer(t)

	req := &Request{
		Operation: OpCloseContinue,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleCloseContinue(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleCloseContinue_NonExistentStep(t *testing.T) {
	server, store := setupMolTestServerWithSQLite(t)

	// Create some test data to ensure the store is initialized
	createTestMolecule(t, store, "bd-mol1", "Test Molecule", false)

	args := CloseContinueArgs{
		ClosedStepID: "bd-nonexistent",
		AutoClaim:    true,
		Actor:        "test",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCloseContinue,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCloseContinue(req)
	if resp.Success {
		t.Error("expected failure for non-existent step")
	}
}

func TestHandleCloseContinue_NotPartOfMolecule(t *testing.T) {
	server, store := setupMolTestServerWithSQLite(t)

	// Create a standalone issue (not part of a molecule)
	standalone := createTestMolecule(t, store, "bd-standalone", "Standalone Issue", false)

	// Close the issue first
	ctx := context.Background()
	store.CloseIssue(ctx, standalone.ID, "Closed", "test", "")

	args := CloseContinueArgs{
		ClosedStepID: standalone.ID,
		AutoClaim:    true,
		Actor:        "test",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCloseContinue,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCloseContinue(req)
	if !resp.Success {
		t.Fatalf("expected success for standalone issue: %s", resp.Error)
	}

	var result CloseContinueResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Should return empty result (no molecule to advance)
	if result.MoleculeID != "" {
		t.Errorf("expected empty molecule ID, got %s", result.MoleculeID)
	}
	if result.NextStep != nil {
		t.Error("expected nil next step")
	}
}

func TestHandleCloseContinue_WithMolecule(t *testing.T) {
	server, store := setupMolTestServerWithSQLite(t)

	// Create a molecule (proto) with children (steps)
	// Use createTestProto so findParentMolecule will recognize it as a molecule root
	mol := createTestProto(t, store, "bd-mol1", "Test Molecule")
	step1 := createTestChild(t, store, "bd-step1", "Step 1", mol.ID, false)
	step2 := createTestChild(t, store, "bd-step2", "Step 2", mol.ID, false)

	ctx := context.Background()

	// Close step1
	store.CloseIssue(ctx, step1.ID, "Closed", "test", "")
	_ = step2 // suppress unused warning
	_ = server // suppress unused warning

	args := CloseContinueArgs{
		ClosedStepID: step1.ID,
		AutoClaim:    false, // Don't auto-claim
		Actor:        "test",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCloseContinue,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCloseContinue(req)
	if !resp.Success {
		t.Fatalf("expected success: %s", resp.Error)
	}

	var result CloseContinueResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result.MoleculeID != mol.ID {
		t.Errorf("expected molecule ID %s, got %s", mol.ID, result.MoleculeID)
	}
	if result.MolComplete {
		t.Error("expected molecule not complete")
	}
	if result.NextStep == nil {
		t.Error("expected next step to be found")
	} else if result.NextStep.ID != step2.ID {
		t.Errorf("expected next step %s, got %s", step2.ID, result.NextStep.ID)
	}
	if result.AutoAdvanced {
		t.Error("expected auto_advanced to be false")
	}
}

func TestHandleCloseContinue_AutoClaim(t *testing.T) {
	server, store := setupMolTestServerWithSQLite(t)

	// Create a molecule (proto) with children (steps)
	mol := createTestProto(t, store, "bd-mol1", "Test Molecule")
	step1 := createTestChild(t, store, "bd-step1", "Step 1", mol.ID, false)
	step2 := createTestChild(t, store, "bd-step2", "Step 2", mol.ID, false)

	// Close step1
	ctx := context.Background()
	store.CloseIssue(ctx, step1.ID, "Closed", "test", "")

	args := CloseContinueArgs{
		ClosedStepID: step1.ID,
		AutoClaim:    true, // Auto-claim the next step
		Actor:        "test",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCloseContinue,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCloseContinue(req)
	if !resp.Success {
		t.Fatalf("expected success: %s", resp.Error)
	}

	var result CloseContinueResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if !result.AutoAdvanced {
		t.Error("expected auto_advanced to be true")
	}
	if result.NextStep == nil {
		t.Fatal("expected next step")
	}

	// Verify step2 is now in_progress
	step2Updated, err := store.GetIssue(ctx, step2.ID)
	if err != nil {
		t.Fatalf("failed to get step2: %v", err)
	}
	if step2Updated.Status != types.StatusInProgress {
		t.Errorf("expected step2 status in_progress, got %s", step2Updated.Status)
	}
}

func TestHandleCloseContinue_MoleculeComplete(t *testing.T) {
	server, store := setupMolTestServerWithSQLite(t)

	// Create a molecule (proto) with only one child
	mol := createTestProto(t, store, "bd-mol1", "Test Molecule")
	step1 := createTestChild(t, store, "bd-step1", "Step 1", mol.ID, false)

	// Close step1 (the only step)
	ctx := context.Background()
	store.CloseIssue(ctx, step1.ID, "Closed", "test", "")

	args := CloseContinueArgs{
		ClosedStepID: step1.ID,
		AutoClaim:    true,
		Actor:        "test",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCloseContinue,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCloseContinue(req)
	if !resp.Success {
		t.Fatalf("expected success: %s", resp.Error)
	}

	var result CloseContinueResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if !result.MolComplete {
		t.Error("expected molecule to be complete")
	}
	if result.NextStep != nil {
		t.Error("expected no next step when molecule is complete")
	}
}
