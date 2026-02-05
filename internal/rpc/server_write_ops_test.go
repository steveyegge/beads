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

// TestCookRPC_NotSupported tests that cook returns a not-supported error via RPC (bd-wj80).
func TestCookRPC_NotSupported(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &CookArgs{
		FormulaName: "mol-feature",
		Persist:     true,
	}
	_, err := client.Cook(args)
	if err == nil {
		t.Fatal("Expected error for unsupported cook operation")
	}
	// Verify the error message indicates it's not supported
	if !strings.Contains(err.Error(), "cook is not yet supported via daemon RPC") {
		t.Errorf("Unexpected error message: %v", err)
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

// TestPourRPC_NotSupported tests that pour returns a not-supported error via RPC (bd-wj80).
func TestPourRPC_NotSupported(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	args := &PourArgs{
		ProtoID: "mol-feature",
		Vars:    map[string]string{"name": "auth"},
	}
	_, err := client.Pour(args)
	if err == nil {
		t.Fatal("Expected error for unsupported pour operation")
	}
	// Verify the error message indicates it's not supported
	if !strings.Contains(err.Error(), "pour is not yet supported via daemon RPC") {
		t.Errorf("Unexpected error message: %v", err)
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
