package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestRenamePrefixRPC tests the rename-prefix operation via RPC (bd-wj80).
func TestRenamePrefixRPC(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Create several test issues
	for i := 0; i < 3; i++ {
		createArgs := &CreateArgs{
			Title:       "Test issue for rename",
			Description: "Testing rename-prefix",
			IssueType:   "task",
			Priority:    2,
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Create %d failed: %v", i, err)
		}
		if !resp.Success {
			t.Fatalf("Create %d unsuccessful: %s", i, resp.Error)
		}
	}

	// Dry run first
	dryRunArgs := &RenamePrefixArgs{
		NewPrefix: "test-",
		DryRun:    true,
	}
	result, err := client.RenamePrefix(dryRunArgs)
	if err != nil {
		t.Fatalf("RenamePrefix dry-run failed: %v", err)
	}
	if !result.DryRun {
		t.Error("Expected dry_run=true in result")
	}
	if result.IssuesRenamed != 3 {
		t.Errorf("Expected 3 issues to rename, got %d", result.IssuesRenamed)
	}
	if result.OldPrefix != "bd-" {
		t.Errorf("Expected old prefix 'bd-', got %q", result.OldPrefix)
	}
	if result.NewPrefix != "test-" {
		t.Errorf("Expected new prefix 'test-', got %q", result.NewPrefix)
	}

	// Verify issues still have old prefix (dry run shouldn't change anything)
	issues, err := server.storage.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	for _, issue := range issues {
		if issue.ID[:3] != "bd-" {
			t.Errorf("Issue %s should still have bd- prefix after dry run", issue.ID)
		}
	}

	// Actual rename
	renameArgs := &RenamePrefixArgs{
		NewPrefix: "test-",
	}
	result, err = client.RenamePrefix(renameArgs)
	if err != nil {
		t.Fatalf("RenamePrefix failed: %v", err)
	}
	if result.DryRun {
		t.Error("Expected dry_run=false in result")
	}
	if result.IssuesRenamed != 3 {
		t.Errorf("Expected 3 issues renamed, got %d", result.IssuesRenamed)
	}

	// Verify issues now have new prefix
	issues, err = server.storage.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues after rename failed: %v", err)
	}
	for _, issue := range issues {
		if len(issue.ID) < 5 || issue.ID[:5] != "test-" {
			t.Errorf("Issue %s should have test- prefix after rename", issue.ID)
		}
	}

	// Verify config updated
	prefix, err := server.storage.GetConfig(ctx, "issue_prefix")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if prefix != "test" {
		t.Errorf("Expected config prefix 'test', got %q", prefix)
	}

	_ = server
}

// TestRenamePrefixRPC_SamePrefix tests that renaming to the same prefix is an error.
func TestRenamePrefixRPC_SamePrefix(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &RenamePrefixArgs{
		NewPrefix: "bd-",
	}
	_, err := client.RenamePrefix(args)
	if err == nil {
		t.Fatal("Expected error when renaming to same prefix")
	}
}

// TestRenamePrefixRPC_TextReferences tests that text references are updated.
func TestRenamePrefixRPC_TextReferences(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue
	createArgs := &CreateArgs{
		Title:       "Test issue",
		Description: "See also bd-12345 for details",
		IssueType:   "task",
		Priority:    2,
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	var created types.Issue
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Rename prefix
	renameArgs := &RenamePrefixArgs{
		NewPrefix: "xx-",
	}
	_, err = client.RenamePrefix(renameArgs)
	if err != nil {
		t.Fatalf("RenamePrefix failed: %v", err)
	}

	// Check that description references were updated
	issues, err := server.storage.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("Expected 1 issue, got %d", len(issues))
	}
	if issues[0].Description != "See also xx-12345 for details" {
		t.Errorf("Expected description text references to be updated, got %q", issues[0].Description)
	}

	_ = server
}

// TestMoveRPC tests the move operation via RPC (bd-wj80).
// Note: move requires cross-rig resolution which needs a full town setup.
// This test verifies basic request validation.
func TestMoveRPC_Validation(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Missing issue_id
	args := &MoveArgs{
		TargetRig: "beads",
	}
	_, err := client.Move(args)
	if err == nil {
		t.Fatal("Expected error for missing issue_id")
	}

	// Missing target_rig
	args = &MoveArgs{
		IssueID: "bd-test",
	}
	_, err = client.Move(args)
	if err == nil {
		t.Fatal("Expected error for missing target_rig")
	}
}

// TestRefileRPC_Validation tests the refile operation validation via RPC (bd-wj80).
func TestRefileRPC_Validation(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Missing issue_id
	args := &RefileArgs{
		TargetRig: "beads",
	}
	_, err := client.Refile(args)
	if err == nil {
		t.Fatal("Expected error for missing issue_id")
	}

	// Missing target_rig
	args = &RefileArgs{
		IssueID: "bd-test",
	}
	_, err = client.Refile(args)
	if err == nil {
		t.Fatal("Expected error for missing target_rig")
	}
}

// TestCookRPC_MissingFormula tests that cook validates formula_name.
func TestCookRPC_MissingFormula(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &CookArgs{}
	_, err := client.Cook(args)
	if err == nil {
		t.Fatal("Expected error for missing formula_name")
	}
}

// TestCookRPC_FormulaNotFound tests that cook returns error for unknown formula.
func TestCookRPC_FormulaNotFound(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &CookArgs{FormulaName: "nonexistent-formula"}
	_, err := client.Cook(args)
	if err == nil {
		t.Fatal("Expected error for unknown formula")
	}
	if !strings.Contains(err.Error(), "loading formula") {
		t.Errorf("Expected loading error, got: %v", err)
	}
}

// storeTestFormula creates a formula in the test database for cook tests.
func storeTestFormula(t *testing.T, server *Server, name string, steps []map[string]interface{}) {
	t.Helper()
	ctx := context.Background()

	// Build formula JSON
	formulaData := map[string]interface{}{
		"formula":     name,
		"description": "Test formula: " + name,
		"version":     1,
		"type":        "workflow",
		"steps":       steps,
	}
	metadataBytes, err := json.Marshal(formulaData)
	if err != nil {
		t.Fatalf("Failed to marshal formula: %v", err)
	}

	issue := &types.Issue{
		ID:          "bd-formula-" + name,
		Title:       name,
		Description: "Test formula: " + name,
		Status:      types.StatusOpen,
		IssueType:   types.TypeFormula,
		Metadata:    json.RawMessage(metadataBytes),
		IsTemplate:  true,
	}
	if err := server.storage.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to store formula: %v", err)
	}
}

// TestCookRPC_Ephemeral tests ephemeral cook (returns subgraph without persisting).
func TestCookRPC_Ephemeral(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Store a test formula in the DB
	storeTestFormula(t, server, "mol-test-cook", []map[string]interface{}{
		{"id": "design", "title": "Design the feature", "type": "task"},
		{"id": "implement", "title": "Implement the feature", "type": "task", "depends_on": []string{"design"}},
	})

	// Cook ephemeral (default mode - no persist)
	args := &CookArgs{FormulaName: "mol-test-cook"}
	result, err := client.Cook(args)
	if err != nil {
		t.Fatalf("Cook failed: %v", err)
	}
	if result.ProtoID != "mol-test-cook" {
		t.Errorf("Expected proto_id 'mol-test-cook', got %q", result.ProtoID)
	}
	// Root + 2 steps = 3 issues
	if result.Created != 3 {
		t.Errorf("Expected 3 issues, got %d", result.Created)
	}
	if result.DryRun {
		t.Error("Expected dry_run=false")
	}
	if len(result.Subgraph) == 0 {
		t.Error("Expected non-empty subgraph in ephemeral mode")
	}
}

// TestCookRPC_DryRun tests dry-run cook (returns preview info).
func TestCookRPC_DryRun(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	storeTestFormula(t, server, "mol-test-dry", []map[string]interface{}{
		{"id": "step1", "title": "Step 1", "type": "task"},
		{"id": "step2", "title": "Step 2", "type": "task"},
	})

	args := &CookArgs{
		FormulaName: "mol-test-dry",
		DryRun:      true,
	}
	result, err := client.Cook(args)
	if err != nil {
		t.Fatalf("Cook dry-run failed: %v", err)
	}
	if !result.DryRun {
		t.Error("Expected dry_run=true")
	}
	if result.ProtoID != "mol-test-dry" {
		t.Errorf("Expected proto_id 'mol-test-dry', got %q", result.ProtoID)
	}
}

// TestCookRPC_Persist tests persist mode (creates proto in database).
func TestCookRPC_Persist(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	storeTestFormula(t, server, "mol-test-persist", []map[string]interface{}{
		{"id": "plan", "title": "Plan work", "type": "task"},
		{"id": "execute", "title": "Execute work", "type": "task", "depends_on": []string{"plan"}},
	})

	args := &CookArgs{
		FormulaName: "mol-test-persist",
		Persist:     true,
	}
	result, err := client.Cook(args)
	if err != nil {
		t.Fatalf("Cook persist failed: %v", err)
	}
	if result.ProtoID != "mol-test-persist" {
		t.Errorf("Expected proto_id 'mol-test-persist', got %q", result.ProtoID)
	}
	// Root + 2 steps = 3 issues
	if result.Created != 3 {
		t.Errorf("Expected 3 issues created, got %d", result.Created)
	}
	if len(result.Subgraph) != 0 {
		t.Error("Expected no subgraph in persist mode")
	}

	// Verify the proto was actually created in the DB
	ctx := context.Background()
	proto, err := server.storage.GetIssue(ctx, "mol-test-persist")
	if err != nil {
		t.Fatalf("Failed to get persisted proto: %v", err)
	}
	if proto == nil {
		t.Fatal("Proto not found in database")
	}
	if !proto.IsTemplate {
		t.Error("Expected proto to be marked as template")
	}

	// Verify molecule label was added
	labels, err := server.storage.GetLabels(ctx, "mol-test-persist")
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}
	found := false
	for _, l := range labels {
		if l == MoleculeLabel {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected %q label on proto, got labels: %v", MoleculeLabel, labels)
	}
}

// TestCookRPC_PersistForce tests --force replacing an existing proto.
func TestCookRPC_PersistForce(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	storeTestFormula(t, server, "mol-test-force", []map[string]interface{}{
		{"id": "step1", "title": "Original step", "type": "task"},
	})

	// Cook persist first time
	args := &CookArgs{
		FormulaName: "mol-test-force",
		Persist:     true,
	}
	_, err := client.Cook(args)
	if err != nil {
		t.Fatalf("First cook failed: %v", err)
	}

	// Second cook without force should fail
	_, err = client.Cook(args)
	if err == nil {
		t.Fatal("Expected error when proto already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Expected 'already exists' error, got: %v", err)
	}

	// Cook with force should succeed
	args.Force = true
	result, err := client.Cook(args)
	if err != nil {
		t.Fatalf("Force cook failed: %v", err)
	}
	if result.Created != 2 {
		t.Errorf("Expected 2 issues created, got %d", result.Created)
	}
}

// TestCookRPC_Prefix tests proto ID prefix.
func TestCookRPC_Prefix(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	storeTestFormula(t, server, "mol-test-prefix", []map[string]interface{}{
		{"id": "step1", "title": "Step", "type": "task"},
	})

	args := &CookArgs{
		FormulaName: "mol-test-prefix",
		Prefix:      "gt-",
	}
	result, err := client.Cook(args)
	if err != nil {
		t.Fatalf("Cook with prefix failed: %v", err)
	}
	if result.ProtoID != "gt-mol-test-prefix" {
		t.Errorf("Expected proto_id 'gt-mol-test-prefix', got %q", result.ProtoID)
	}
}

// TestCookRPC_Variables tests variable substitution in runtime mode.
func TestCookRPC_Variables(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Store a formula with variables
	formulaData := map[string]interface{}{
		"formula":     "mol-test-vars",
		"description": "Test formula with vars",
		"version":     1,
		"type":        "workflow",
		"vars": map[string]interface{}{
			"component": map[string]interface{}{
				"description": "Component name",
				"required":    true,
			},
		},
		"steps": []map[string]interface{}{
			{"id": "impl", "title": "Implement {{component}}", "type": "task"},
		},
	}
	metadataBytes, err := json.Marshal(formulaData)
	if err != nil {
		t.Fatalf("Failed to marshal formula: %v", err)
	}
	ctx := context.Background()
	issue := &types.Issue{
		ID:          "bd-formula-mol-test-vars",
		Title:       "mol-test-vars",
		Description: "Test formula with vars",
		Status:      types.StatusOpen,
		IssueType:   types.TypeFormula,
		Metadata:    json.RawMessage(metadataBytes),
		IsTemplate:  true,
	}
	if err := server.storage.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to store formula: %v", err)
	}

	// Cook with variable substitution (runtime mode)
	args := &CookArgs{
		FormulaName: "mol-test-vars",
		Vars:        map[string]string{"component": "auth"},
	}
	result, err := client.Cook(args)
	if err != nil {
		t.Fatalf("Cook with vars failed: %v", err)
	}
	if result.Created != 2 {
		t.Errorf("Expected 2 issues, got %d", result.Created)
	}

	// Verify the subgraph has substituted titles
	if len(result.Subgraph) == 0 {
		t.Fatal("Expected non-empty subgraph")
	}
	subgraphStr := string(result.Subgraph)
	if !strings.Contains(subgraphStr, "Implement auth") {
		t.Error("Expected substituted title 'Implement auth' in subgraph")
	}
	if strings.Contains(subgraphStr, "{{component}}") {
		t.Error("Found unsubstituted {{component}} in subgraph")
	}
}

// TestPourRPC_NonexistentProto tests that pour returns an error for a nonexistent proto/formula (bd-wj80).
func TestPourRPC_NonexistentProto(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &PourArgs{
		ProtoID: "mol-feature",
		Vars:    map[string]string{"name": "auth"},
	}
	_, err := client.Pour(args)
	if err == nil {
		t.Fatal("Expected error for nonexistent proto")
	}
	// Verify the error message indicates the proto/formula was not found
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' in error message, got: %v", err)
	}
}

// TestPourRPC_MissingProtoID tests that pour validates proto_id.
func TestPourRPC_MissingProtoID(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &PourArgs{}
	_, err := client.Pour(args)
	if err == nil {
		t.Fatal("Expected error for missing proto_id")
	}
}

// TestRenamePrefixRPC_EmptyNewPrefix tests that empty new_prefix is rejected.
func TestRenamePrefixRPC_EmptyNewPrefix(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &RenamePrefixArgs{
		NewPrefix: "",
	}
	_, err := client.RenamePrefix(args)
	if err == nil {
		t.Fatal("Expected error for empty new_prefix")
	}
}
