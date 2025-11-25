package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/deletions"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// TestRecordDeletion tests that recordDeletion creates deletion manifest entries
func TestRecordDeletion(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up dbPath so getDeletionsPath() works
	oldDbPath := dbPath
	dbPath = filepath.Join(tmpDir, "beads.db")
	defer func() { dbPath = oldDbPath }()

	// Create the .beads directory
	if err := os.MkdirAll(tmpDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Test recordDeletion
	err := recordDeletion("test-abc", "test-user", "test reason")
	if err != nil {
		t.Fatalf("recordDeletion failed: %v", err)
	}

	// Verify the deletion was recorded
	deletionsPath := getDeletionsPath()
	result, err := deletions.LoadDeletions(deletionsPath)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}

	if len(result.Records) != 1 {
		t.Fatalf("expected 1 deletion record, got %d", len(result.Records))
	}

	del, found := result.Records["test-abc"]
	if !found {
		t.Fatalf("deletion record for 'test-abc' not found")
	}

	if del.Actor != "test-user" {
		t.Errorf("expected actor 'test-user', got '%s'", del.Actor)
	}

	if del.Reason != "test reason" {
		t.Errorf("expected reason 'test reason', got '%s'", del.Reason)
	}

	// Timestamp should be recent (within last minute)
	if time.Since(del.Timestamp) > time.Minute {
		t.Errorf("timestamp seems too old: %v", del.Timestamp)
	}
}

// TestRecordDeletions tests that recordDeletions creates multiple deletion manifest entries
func TestRecordDeletions(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up dbPath so getDeletionsPath() works
	oldDbPath := dbPath
	dbPath = filepath.Join(tmpDir, "beads.db")
	defer func() { dbPath = oldDbPath }()

	// Create the .beads directory
	if err := os.MkdirAll(tmpDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Test recordDeletions with multiple IDs
	ids := []string{"test-abc", "test-def", "test-ghi"}
	err := recordDeletions(ids, "batch-user", "batch cleanup")
	if err != nil {
		t.Fatalf("recordDeletions failed: %v", err)
	}

	// Verify the deletions were recorded
	deletionsPath := getDeletionsPath()
	result, err := deletions.LoadDeletions(deletionsPath)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}

	if len(result.Records) != 3 {
		t.Fatalf("expected 3 deletion records, got %d", len(result.Records))
	}

	for _, id := range ids {
		del, found := result.Records[id]
		if !found {
			t.Errorf("deletion record for '%s' not found", id)
			continue
		}

		if del.Actor != "batch-user" {
			t.Errorf("expected actor 'batch-user' for %s, got '%s'", id, del.Actor)
		}

		if del.Reason != "batch cleanup" {
			t.Errorf("expected reason 'batch cleanup' for %s, got '%s'", id, del.Reason)
		}
	}
}

// TestGetActorWithGit tests actor sourcing logic
func TestGetActorWithGit(t *testing.T) {
	// Save original actor value
	oldActor := actor
	defer func() { actor = oldActor }()

	// Test case 1: actor is set from flag/env
	actor = "flag-user"
	result := getActorWithGit()
	if result != "flag-user" {
		t.Errorf("expected 'flag-user' when actor is set, got '%s'", result)
	}

	// Test case 2: actor is "unknown" - should try git config
	actor = "unknown"
	result = getActorWithGit()
	// Can't test exact result since it depends on git config, but it shouldn't be empty
	if result == "" {
		t.Errorf("expected non-empty result when actor is 'unknown'")
	}

	// Test case 3: actor is empty - should try git config
	actor = ""
	result = getActorWithGit()
	if result == "" {
		t.Errorf("expected non-empty result when actor is empty")
	}
}

// TestDeleteRecordingOrderOfOperations verifies deletion is recorded before DB delete
func TestDeleteRecordingOrderOfOperations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Set up dbPath
	oldDbPath := dbPath
	dbPath = filepath.Join(tmpDir, "beads.db")
	defer func() { dbPath = oldDbPath }()

	// Create database
	testStore, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer testStore.Close()

	// Initialize prefix
	if err := testStore.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Create an issue
	issue := &types.Issue{
		ID:        "test-delete-order",
		Title:     "Test Order of Operations",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Record deletion (simulating what delete command does)
	if err := recordDeletion(issue.ID, "test-user", "order test"); err != nil {
		t.Fatalf("recordDeletion failed: %v", err)
	}

	// Verify record was created BEFORE any DB changes
	deletionsPath := getDeletionsPath()
	result, err := deletions.LoadDeletions(deletionsPath)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}

	if _, found := result.Records[issue.ID]; !found {
		t.Error("deletion record should exist before DB deletion")
	}

	// Now verify the issue still exists in DB (we only recorded, didn't delete)
	existing, err := testStore.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if existing == nil {
		t.Error("issue should still exist in DB (we only recorded the deletion)")
	}

	// Now delete from DB
	if err := testStore.DeleteIssue(ctx, issue.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	// Verify both: deletion record exists AND issue is gone from DB
	result, err = deletions.LoadDeletions(deletionsPath)
	if err != nil {
		t.Fatalf("LoadDeletions failed: %v", err)
	}
	if _, found := result.Records[issue.ID]; !found {
		t.Error("deletion record should still exist after DB deletion")
	}

	existing, err = testStore.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if existing != nil {
		t.Error("issue should be gone from DB after deletion")
	}
}
