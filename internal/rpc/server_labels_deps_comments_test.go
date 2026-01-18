package rpc

import (
	"encoding/json"
	"testing"
)

// TestDepAdd_JSONOutput verifies that handleDepAdd returns JSON data in Response.Data.
// This test is expected to FAIL until the bug is fixed (GH#952 Issue 2).
func TestDepAdd_JSONOutput(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create two test issues for the dependency relationship
	createArgs1 := &CreateArgs{
		Title:     "Issue that depends on another",
		IssueType: "task",
		Priority:  2,
	}
	resp1, err := client.Create(createArgs1)
	if err != nil {
		t.Fatalf("Failed to create first issue: %v", err)
	}
	var issue1 struct{ ID string }
	if err := json.Unmarshal(resp1.Data, &issue1); err != nil {
		t.Fatalf("Failed to unmarshal first issue: %v", err)
	}

	createArgs2 := &CreateArgs{
		Title:     "Issue being depended upon",
		IssueType: "task",
		Priority:  2,
	}
	resp2, err := client.Create(createArgs2)
	if err != nil {
		t.Fatalf("Failed to create second issue: %v", err)
	}
	var issue2 struct{ ID string }
	if err := json.Unmarshal(resp2.Data, &issue2); err != nil {
		t.Fatalf("Failed to unmarshal second issue: %v", err)
	}

	// Add dependency: issue1 depends on issue2
	depArgs := &DepAddArgs{
		FromID:  issue1.ID,
		ToID:    issue2.ID,
		DepType: "blocks",
	}
	resp, err := client.AddDependency(depArgs)
	if err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// BUG: Response.Data is nil when it should contain JSON
	if resp.Data == nil {
		t.Errorf("resp.Data is nil; expected JSON output with {status, issue_id, depends_on_id, type}")
	}

	// Verify JSON structure matches expected format
	if resp.Data != nil {
		var result struct {
			Status      string `json:"status"`
			IssueID     string `json:"issue_id"`
			DependsOnID string `json:"depends_on_id"`
			Type        string `json:"type"`
		}
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			t.Errorf("Failed to unmarshal response data: %v", err)
		}
		if result.Status != "added" {
			t.Errorf("Expected status='added', got %q", result.Status)
		}
		if result.IssueID != issue1.ID {
			t.Errorf("Expected issue_id=%q, got %q", issue1.ID, result.IssueID)
		}
		if result.DependsOnID != issue2.ID {
			t.Errorf("Expected depends_on_id=%q, got %q", issue2.ID, result.DependsOnID)
		}
		if result.Type != "blocks" {
			t.Errorf("Expected type='blocks', got %q", result.Type)
		}
	}

	// Silence unused variable warning
	_ = store
}

// TestDepRemove_JSONOutput verifies that handleDepRemove returns JSON data in Response.Data.
// This test is expected to FAIL until the bug is fixed (GH#952 Issue 2).
func TestDepRemove_JSONOutput(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create two test issues
	createArgs1 := &CreateArgs{
		Title:     "Issue with dependency to remove",
		IssueType: "task",
		Priority:  2,
	}
	resp1, err := client.Create(createArgs1)
	if err != nil {
		t.Fatalf("Failed to create first issue: %v", err)
	}
	var issue1 struct{ ID string }
	if err := json.Unmarshal(resp1.Data, &issue1); err != nil {
		t.Fatalf("Failed to unmarshal first issue: %v", err)
	}

	createArgs2 := &CreateArgs{
		Title:     "Dependency target issue",
		IssueType: "task",
		Priority:  2,
	}
	resp2, err := client.Create(createArgs2)
	if err != nil {
		t.Fatalf("Failed to create second issue: %v", err)
	}
	var issue2 struct{ ID string }
	if err := json.Unmarshal(resp2.Data, &issue2); err != nil {
		t.Fatalf("Failed to unmarshal second issue: %v", err)
	}

	// First add a dependency so we can remove it
	addArgs := &DepAddArgs{
		FromID:  issue1.ID,
		ToID:    issue2.ID,
		DepType: "blocks",
	}
	_, err = client.AddDependency(addArgs)
	if err != nil {
		t.Fatalf("AddDependency (setup) failed: %v", err)
	}

	// Now remove the dependency
	removeArgs := &DepRemoveArgs{
		FromID: issue1.ID,
		ToID:   issue2.ID,
	}
	resp, err := client.RemoveDependency(removeArgs)
	if err != nil {
		t.Fatalf("RemoveDependency failed: %v", err)
	}

	// BUG: Response.Data is nil when it should contain JSON
	if resp.Data == nil {
		t.Errorf("resp.Data is nil; expected JSON output with {status, issue_id, depends_on_id}")
	}

	// Verify JSON structure matches expected format
	if resp.Data != nil {
		var result struct {
			Status      string `json:"status"`
			IssueID     string `json:"issue_id"`
			DependsOnID string `json:"depends_on_id"`
		}
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			t.Errorf("Failed to unmarshal response data: %v", err)
		}
		if result.Status != "removed" {
			t.Errorf("Expected status='removed', got %q", result.Status)
		}
		if result.IssueID != issue1.ID {
			t.Errorf("Expected issue_id=%q, got %q", issue1.ID, result.IssueID)
		}
		if result.DependsOnID != issue2.ID {
			t.Errorf("Expected depends_on_id=%q, got %q", issue2.ID, result.DependsOnID)
		}
	}

	// Silence unused variable warning
	_ = store
}
