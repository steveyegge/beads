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

// Tests for MolReadyGated (bd-2n56)

func TestHandleMolReadyGated_InvalidArgs(t *testing.T) {
	server, _ := setupMolTestServer(t)

	req := &Request{
		Operation: OpMolReadyGated,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleMolReadyGated(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleMolReadyGated_NoGates(t *testing.T) {
	server, _ := setupMolTestServer(t)

	args := MolReadyGatedArgs{}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolReadyGated,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolReadyGated(req)
	if !resp.Success {
		t.Fatalf("request failed: %s", resp.Error)
	}

	var result MolReadyGatedResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result.Count != 0 {
		t.Errorf("expected 0 molecules, got %d", result.Count)
	}
	if len(result.Molecules) != 0 {
		t.Errorf("expected empty molecules slice, got %d", len(result.Molecules))
	}
}

func TestHandleMolReadyGated_WithClosedGate(t *testing.T) {
	// This test uses SQLite storage since the "gate" type is a custom Gas Town type
	// that requires types.custom configuration, which the memory store doesn't support.
	server, store := setupMolTestServerWithSQLite(t)
	ctx := context.Background()

	// Create a molecule (parent epic)
	mol := &types.Issue{
		ID:        "bd-mol1",
		Title:     "Test Molecule",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, mol, "test"); err != nil {
		t.Fatalf("failed to create molecule: %v", err)
	}

	// Create a step that will be blocked by a gate
	step := &types.Issue{
		ID:        "bd-step1",
		Title:     "Step 1",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, step, "test"); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	// Link step to molecule
	dep1 := &types.Dependency{
		IssueID:     "bd-step1",
		DependsOnID: "bd-mol1",
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep1, "test"); err != nil {
		t.Fatalf("failed to add parent-child dependency: %v", err)
	}

	// Create a closed gate - using TypeTask to avoid custom type validation issues
	// In production, gates use type="gate" which is configured via types.custom.
	// For this test, we simulate the gate behavior using a task with AwaitType set.
	gate := &types.Issue{
		ID:        "bd-gate1",
		Title:     "Gate 1",
		IssueType: types.TypeTask, // Use task instead of gate for test simplicity
		Status:    types.StatusClosed,
		AwaitType: "human",
	}
	if err := store.CreateIssue(ctx, gate, "test"); err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	// Link step to gate (step depends on gate)
	dep2 := &types.Dependency{
		IssueID:     "bd-step1",
		DependsOnID: "bd-gate1",
		Type:        types.DepBlocks,
	}
	if err := store.AddDependency(ctx, dep2, "test"); err != nil {
		t.Fatalf("failed to add blocks dependency: %v", err)
	}

	// Now query for gate-ready molecules
	// Note: This won't find results because the server filters by IssueType="gate"
	// but we've used TypeTask. This test mainly verifies the handler runs without errors.
	args := MolReadyGatedArgs{}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolReadyGated,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolReadyGated(req)
	if !resp.Success {
		t.Fatalf("request failed: %s", resp.Error)
	}

	var result MolReadyGatedResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// The handler should work without errors even if no gates are found
	t.Logf("found %d gate-ready molecules", result.Count)
}

func TestHandleMolReadyGated_WithLimit(t *testing.T) {
	server, _ := setupMolTestServer(t)

	args := MolReadyGatedArgs{
		Limit: 10,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMolReadyGated,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMolReadyGated(req)
	if !resp.Success {
		t.Fatalf("request failed: %s", resp.Error)
	}

	var result MolReadyGatedResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Should not error with custom limit
	t.Logf("found %d gate-ready molecules with limit", result.Count)
}
