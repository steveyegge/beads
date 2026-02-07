package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/runbook"
)

// testRunbookContent creates a minimal valid RunbookContent JSON for testing.
func testRunbookContent(name string) json.RawMessage {
	rb := runbook.RunbookContent{
		Name:     name,
		Format:   "hcl",
		Content:  `job "test" { command = "echo hello" }`,
		Jobs:     []string{"test"},
		Commands: []string{},
		Workers:  []string{},
	}
	data, _ := json.Marshal(rb)
	return data
}

// testRunbookContentWithLabels creates a RunbookContent JSON with jobs, commands, and workers.
func testRunbookContentWithLabels(name string, jobs, commands, workers []string) json.RawMessage {
	rb := runbook.RunbookContent{
		Name:     name,
		Format:   "hcl",
		Content:  `job "deploy" { command = "deploy" }`,
		Jobs:     jobs,
		Commands: commands,
		Workers:  workers,
	}
	data, _ := json.Marshal(rb)
	return data
}

func TestRunbookList_Empty(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	result, err := client.RunbookList(&RunbookListArgs{})
	if err != nil {
		t.Fatalf("RunbookList failed: %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Expected 0 runbooks, got %d", result.Count)
	}
}

func TestRunbookSave_Create(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	contentJSON := testRunbookContent("test-runbook")
	saveResult, err := client.RunbookSave(&RunbookSaveArgs{
		Content: contentJSON,
	})
	if err != nil {
		t.Fatalf("RunbookSave failed: %v", err)
	}
	if saveResult.Name != "test-runbook" {
		t.Errorf("Expected name 'test-runbook', got %q", saveResult.Name)
	}
	if !saveResult.Created {
		t.Error("Expected Created=true for new runbook")
	}
	if saveResult.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

func TestRunbookSave_Update(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	contentJSON := testRunbookContent("update-runbook")

	// Save first time
	saveResult1, err := client.RunbookSave(&RunbookSaveArgs{
		Content: contentJSON,
	})
	if err != nil {
		t.Fatalf("First RunbookSave failed: %v", err)
	}
	if !saveResult1.Created {
		t.Error("Expected Created=true for first save")
	}

	// Save again with Force=true
	saveResult2, err := client.RunbookSave(&RunbookSaveArgs{
		Content: contentJSON,
		Force:   true,
	})
	if err != nil {
		t.Fatalf("RunbookSave with Force failed: %v", err)
	}
	if saveResult2.Created {
		t.Error("Expected Created=false for update")
	}
}

func TestRunbookSave_DuplicateNoForce(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	contentJSON := testRunbookContent("dup-runbook")

	// Save first time
	_, err := client.RunbookSave(&RunbookSaveArgs{Content: contentJSON})
	if err != nil {
		t.Fatalf("First RunbookSave failed: %v", err)
	}

	// Save again without Force should fail
	_, err = client.RunbookSave(&RunbookSaveArgs{Content: contentJSON})
	if err == nil {
		t.Fatal("Expected error saving duplicate runbook without Force")
	}
}

func TestRunbookSave_InvalidJSON(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.RunbookSave(&RunbookSaveArgs{
		Content: json.RawMessage(`{not valid json`),
	})
	if err == nil {
		t.Fatal("Expected error saving invalid JSON content")
	}
}

func TestRunbookSave_MissingName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create RunbookContent with empty name
	rb := runbook.RunbookContent{
		Name:    "",
		Format:  "hcl",
		Content: `job "test" { command = "echo hello" }`,
	}
	data, _ := json.Marshal(rb)

	_, err := client.RunbookSave(&RunbookSaveArgs{
		Content: data,
	})
	if err == nil {
		t.Fatal("Expected error saving runbook with empty name")
	}
}

func TestRunbookSave_MissingContent(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.RunbookSave(&RunbookSaveArgs{
		Content: nil,
	})
	if err == nil {
		t.Fatal("Expected error saving runbook with nil content")
	}
}

func TestRunbookGet_ByID(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save a runbook
	contentJSON := testRunbookContent("get-by-id-runbook")
	saveResult, err := client.RunbookSave(&RunbookSaveArgs{
		Content: contentJSON,
	})
	if err != nil {
		t.Fatalf("RunbookSave failed: %v", err)
	}

	// Get by ID
	getResult, err := client.RunbookGet(&RunbookGetArgs{ID: saveResult.ID})
	if err != nil {
		t.Fatalf("RunbookGet by ID failed: %v", err)
	}
	if getResult.ID != saveResult.ID {
		t.Errorf("ID mismatch: expected %q, got %q", saveResult.ID, getResult.ID)
	}
	if getResult.Name != "get-by-id-runbook" {
		t.Errorf("Expected name 'get-by-id-runbook', got %q", getResult.Name)
	}

	// Verify content can be deserialized back to RunbookContent
	var rb runbook.RunbookContent
	if err := json.Unmarshal(getResult.Content, &rb); err != nil {
		t.Fatalf("Failed to unmarshal runbook content: %v", err)
	}
	if rb.Name != "get-by-id-runbook" {
		t.Errorf("RunbookContent name mismatch: expected 'get-by-id-runbook', got %q", rb.Name)
	}
}

func TestRunbookGet_ByName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save a runbook
	contentJSON := testRunbookContent("get-by-name-runbook")
	saveResult, err := client.RunbookSave(&RunbookSaveArgs{
		Content: contentJSON,
	})
	if err != nil {
		t.Fatalf("RunbookSave failed: %v", err)
	}

	// Get by name
	getResult, err := client.RunbookGet(&RunbookGetArgs{Name: "get-by-name-runbook"})
	if err != nil {
		t.Fatalf("RunbookGet by name failed: %v", err)
	}
	if getResult.ID != saveResult.ID {
		t.Errorf("Get by name returned wrong ID: expected %q, got %q", saveResult.ID, getResult.ID)
	}
	if getResult.Name != "get-by-name-runbook" {
		t.Errorf("Expected name 'get-by-name-runbook', got %q", getResult.Name)
	}
}

func TestRunbookGet_NotFound(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.RunbookGet(&RunbookGetArgs{ID: "nonexistent-id"})
	if err == nil {
		t.Fatal("Expected error getting nonexistent runbook by ID")
	}

	_, err = client.RunbookGet(&RunbookGetArgs{Name: "nonexistent-name"})
	if err == nil {
		t.Fatal("Expected error getting nonexistent runbook by name")
	}
}

func TestRunbookGet_MissingArgs(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.RunbookGet(&RunbookGetArgs{})
	if err == nil {
		t.Fatal("Expected error when neither id nor name provided")
	}
}

func TestRunbookList_AfterSave(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save two runbooks
	_, err := client.RunbookSave(&RunbookSaveArgs{
		Content: testRunbookContent("list-runbook-alpha"),
	})
	if err != nil {
		t.Fatalf("RunbookSave 1 failed: %v", err)
	}

	_, err = client.RunbookSave(&RunbookSaveArgs{
		Content: testRunbookContent("list-runbook-beta"),
	})
	if err != nil {
		t.Fatalf("RunbookSave 2 failed: %v", err)
	}

	// List all
	result, err := client.RunbookList(&RunbookListArgs{})
	if err != nil {
		t.Fatalf("RunbookList failed: %v", err)
	}
	if result.Count != 2 {
		t.Errorf("Expected 2 runbooks, got %d", result.Count)
	}
}

func TestRunbookList_SkipsClosed(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Save a runbook
	saveResult, err := client.RunbookSave(&RunbookSaveArgs{
		Content: testRunbookContent("close-runbook"),
	})
	if err != nil {
		t.Fatalf("RunbookSave failed: %v", err)
	}

	// Verify it appears in the list
	listResult, err := client.RunbookList(&RunbookListArgs{})
	if err != nil {
		t.Fatalf("RunbookList failed: %v", err)
	}
	if listResult.Count != 1 {
		t.Errorf("Expected 1 runbook before close, got %d", listResult.Count)
	}

	// Close the runbook issue directly via storage since templates
	// cannot be closed through the RPC CloseIssue endpoint.
	ctx := context.Background()
	err = server.storage.UpdateIssue(ctx, saveResult.ID, map[string]interface{}{
		"status": "closed",
	}, "test")
	if err != nil {
		t.Fatalf("UpdateIssue (close) failed: %v", err)
	}

	// List should now be empty (closed runbooks are skipped)
	listResult2, err := client.RunbookList(&RunbookListArgs{})
	if err != nil {
		t.Fatalf("RunbookList after close failed: %v", err)
	}
	if listResult2.Count != 0 {
		t.Errorf("Expected 0 runbooks after close, got %d", listResult2.Count)
	}
}

func TestRunbookSave_WithLabels(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	contentJSON := testRunbookContentWithLabels(
		"labeled-runbook",
		[]string{"deploy", "build"},
		[]string{"restart", "check"},
		[]string{"processor", "indexer"},
	)

	saveResult, err := client.RunbookSave(&RunbookSaveArgs{
		Content: contentJSON,
	})
	if err != nil {
		t.Fatalf("RunbookSave failed: %v", err)
	}
	if saveResult.ID == "" {
		t.Error("Expected non-empty ID")
	}

	// Verify the runbook was saved and can be retrieved
	getResult, err := client.RunbookGet(&RunbookGetArgs{ID: saveResult.ID})
	if err != nil {
		t.Fatalf("RunbookGet failed: %v", err)
	}

	// Verify the content round-trips with the correct jobs/commands/workers
	var rb runbook.RunbookContent
	if err := json.Unmarshal(getResult.Content, &rb); err != nil {
		t.Fatalf("Failed to unmarshal runbook content: %v", err)
	}
	if len(rb.Jobs) != 2 {
		t.Errorf("Expected 2 jobs, got %d", len(rb.Jobs))
	}
	if len(rb.Commands) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(rb.Commands))
	}
	if len(rb.Workers) != 2 {
		t.Errorf("Expected 2 workers, got %d", len(rb.Workers))
	}

	// Verify it appears in the list with correct counts
	listResult, err := client.RunbookList(&RunbookListArgs{})
	if err != nil {
		t.Fatalf("RunbookList failed: %v", err)
	}
	if listResult.Count != 1 {
		t.Fatalf("Expected 1 runbook, got %d", listResult.Count)
	}
	summary := listResult.Runbooks[0]
	if summary.Jobs != 2 {
		t.Errorf("Expected 2 jobs in summary, got %d", summary.Jobs)
	}
	if summary.Commands != 2 {
		t.Errorf("Expected 2 commands in summary, got %d", summary.Commands)
	}
	if summary.Workers != 2 {
		t.Errorf("Expected 2 workers in summary, got %d", summary.Workers)
	}
}
