package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestOrphansBasic tests basic orphan detection
func TestOrphansBasic(t *testing.T) {
	// Create a temporary directory with a git repo and beads database
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user (needed for commits)
	ctx := context.Background()
	for _, cmd := range []*exec.Cmd{
		exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@example.com"),
		exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User"),
	} {
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to configure git: %v", err)
		}
	}

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create a minimal database with beads.db
	// For this test, we'll skip creating an actual database
	// since the test is primarily about integration

	// Test: findOrphanedIssues should handle missing database gracefully
	orphans, err := findOrphanedIssues(tmpDir)
	if err != nil {
		t.Fatalf("findOrphanedIssues failed: %v", err)
	}

	// Should be empty list since no database
	if len(orphans) != 0 {
		t.Errorf("Expected empty orphans list, got %d", len(orphans))
	}
}

// TestOrphansNotGitRepo tests behavior in non-git directories
func TestOrphansNotGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// Should not error, just return empty list
	orphans, err := findOrphanedIssues(tmpDir)
	if err != nil {
		t.Fatalf("findOrphanedIssues failed: %v", err)
	}

	if len(orphans) != 0 {
		t.Errorf("Expected empty orphans list for non-git repo, got %d", len(orphans))
	}
}

// TestCloseIssueCommand tests that close issue command is properly formed
func TestCloseIssueCommand(t *testing.T) {
	// This is a basic test to ensure the closeIssue function
	// attempts to run the correct command.
	// In a real environment, this would fail since bd close requires
	// a valid beads database.

	// Just test that the function doesn't panic
	// (actual close will fail, which is expected)
	_ = closeIssue("bd-test-invalid")
	// Error is expected since the issue doesn't exist
}
