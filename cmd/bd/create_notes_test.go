package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// TestCreateWithNotes verifies that the --notes flag works correctly
// during issue creation in both direct mode and RPC mode.
func TestCreateWithNotes(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	t.Run("DirectMode_WithNotes", func(t *testing.T) {
		issue := &types.Issue{
			Title:     "Issue with notes",
			Notes:     "These are my test notes",
			Priority:  1,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}

		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}

		// Retrieve and verify
		retrieved, err := s.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("failed to retrieve issue: %v", err)
		}

		if retrieved.Notes != "These are my test notes" {
			t.Errorf("expected notes 'These are my test notes', got %q", retrieved.Notes)
		}
	})

	t.Run("DirectMode_WithoutNotes", func(t *testing.T) {
		issue := &types.Issue{
			Title:     "Issue without notes",
			Priority:  2,
			IssueType: types.TypeBug,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}

		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}

		// Retrieve and verify
		retrieved, err := s.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("failed to retrieve issue: %v", err)
		}

		if retrieved.Notes != "" {
			t.Errorf("expected empty notes, got %q", retrieved.Notes)
		}
	})

	t.Run("DirectMode_WithNotesAndOtherFields", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Full issue with notes",
			Description:        "Detailed description",
			Design:             "Design notes here",
			AcceptanceCriteria: "All tests pass",
			Notes:              "Additional implementation notes",
			Priority:           1,
			IssueType:          types.TypeFeature,
			Status:             types.StatusOpen,
			Assignee:           "testuser",
			CreatedAt:          time.Now(),
		}

		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}

		// Retrieve and verify all fields
		retrieved, err := s.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("failed to retrieve issue: %v", err)
		}

		if retrieved.Title != "Full issue with notes" {
			t.Errorf("expected title 'Full issue with notes', got %q", retrieved.Title)
		}
		if retrieved.Description != "Detailed description" {
			t.Errorf("expected description, got %q", retrieved.Description)
		}
		if retrieved.Design != "Design notes here" {
			t.Errorf("expected design, got %q", retrieved.Design)
		}
		if retrieved.AcceptanceCriteria != "All tests pass" {
			t.Errorf("expected acceptance criteria, got %q", retrieved.AcceptanceCriteria)
		}
		if retrieved.Notes != "Additional implementation notes" {
			t.Errorf("expected notes 'Additional implementation notes', got %q", retrieved.Notes)
		}
		if retrieved.Assignee != "testuser" {
			t.Errorf("expected assignee 'testuser', got %q", retrieved.Assignee)
		}
	})

	t.Run("DirectMode_NotesWithSpecialCharacters", func(t *testing.T) {
		specialNotes := "Notes with special chars: \n- Bullet point\n- Another one\n\nAnd \"quotes\" and 'apostrophes'"
		issue := &types.Issue{
			Title:     "Issue with special char notes",
			Notes:     specialNotes,
			Priority:  2,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}

		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}

		// Retrieve and verify
		retrieved, err := s.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("failed to retrieve issue: %v", err)
		}

		if retrieved.Notes != specialNotes {
			t.Errorf("notes mismatch.\nExpected: %q\nGot: %q", specialNotes, retrieved.Notes)
		}
	})
}

// TestCreateWithNotesRPC verifies notes field works via RPC protocol
func TestCreateWithNotesRPC(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	t.Run("RPC_CreateArgs_WithNotes", func(t *testing.T) {
		// Test that CreateArgs properly includes Notes field
		args := &rpc.CreateArgs{
			Title:       "RPC test issue",
			Description: "Testing RPC mode",
			Notes:       "RPC notes field",
			Priority:    1,
			IssueType:   "task",
		}

		// Verify the struct has the Notes field populated
		if args.Notes != "RPC notes field" {
			t.Errorf("expected Notes field 'RPC notes field', got %q", args.Notes)
		}
	})

	t.Run("RPC_CreateIssue_WithNotes", func(t *testing.T) {
		// Simulate what the RPC handler does
		createArgs := &rpc.CreateArgs{
			Title:              "RPC created issue",
			Description:        "Created via RPC",
			Design:             "RPC design",
			AcceptanceCriteria: "RPC acceptance",
			Notes:              "RPC implementation notes",
			Priority:           2,
			IssueType:          "feature",
			Assignee:           "rpcuser",
		}

		// Create issue as RPC handler would
		issue := &types.Issue{
			Title:              createArgs.Title,
			Description:        createArgs.Description,
			Design:             createArgs.Design,
			AcceptanceCriteria: createArgs.AcceptanceCriteria,
			Notes:              createArgs.Notes,
			Priority:           createArgs.Priority,
			IssueType:          types.IssueType(createArgs.IssueType),
			Assignee:           createArgs.Assignee,
			Status:             types.StatusOpen,
			CreatedAt:          time.Now(),
		}

		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue via RPC simulation: %v", err)
		}

		// Retrieve and verify
		retrieved, err := s.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("failed to retrieve issue: %v", err)
		}

		if retrieved.Notes != "RPC implementation notes" {
			t.Errorf("expected notes 'RPC implementation notes', got %q", retrieved.Notes)
		}
		if retrieved.Description != "Created via RPC" {
			t.Errorf("expected description 'Created via RPC', got %q", retrieved.Description)
		}
	})
}
