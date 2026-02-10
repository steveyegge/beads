package rpc

import (
	"context"
	"encoding/json"
	"testing"
)

// TestCreateMessageIssue_EmitsMailEvent verifies that creating an issue with
// type="message" succeeds and exercises the emitMailEvent code path (bd-h59f).
// When no event bus is configured (as in tests), emitMailEvent is a no-op but
// the code path must not panic or error.
func TestCreateMessageIssue_EmitsMailEvent(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enable "message" as a custom issue type
	if err := store.SetConfig(ctx, "types.custom", "message"); err != nil {
		t.Fatalf("failed to set types.custom: %v", err)
	}

	// Create a message issue (simulates gt mail send creating a bead)
	resp, err := client.Execute(OpCreate, &CreateArgs{
		Title:     "Test mail subject",
		IssueType: "message",
		Sender:    "mayor/",
		Assignee:  "rig1/witness",
		Priority:  2,
	})
	if err != nil {
		t.Fatalf("Execute OpCreate failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("OpCreate failed: %s", resp.Error)
	}

	// Verify the issue was created with correct fields
	var issue struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		IssueType string `json:"issue_type"`
		Sender    string `json:"sender"`
		Assignee  string `json:"assignee"`
	}
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if issue.IssueType != "message" {
		t.Errorf("expected issue_type=message, got %s", issue.IssueType)
	}
	if issue.Sender != "mayor/" {
		t.Errorf("expected sender=mayor/, got %s", issue.Sender)
	}
	if issue.Assignee != "rig1/witness" {
		t.Errorf("expected assignee=rig1/witness, got %s", issue.Assignee)
	}
	if issue.Title != "Test mail subject" {
		t.Errorf("expected title=Test mail subject, got %s", issue.Title)
	}
}
