package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/namespace"
	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/types"
)

// TestPromoteIssue tests the promote command's ability to move issues between branches
func TestPromoteIssue(t *testing.T) {
	ctx := context.Background()
	s := memory.New("test-prefix")
	defer s.Close()

	// Create an issue on the "feature" branch
	issue := &types.Issue{
		Title:      "Test issue to promote",
		Status:     types.StatusOpen,
		Priority:   1,
		IssueType:  types.IssueType("task"),
		Branch:     "feature",
		CreatedBy:  "test@example.com",
	}

	if err := s.CreateIssue(ctx, issue, "test@example.com"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	originalID := issue.ID
	if originalID == "" {
		t.Fatal("issue ID should be generated")
	}

	// Verify branch is set
	fetched, err := s.GetIssue(ctx, originalID)
	if err != nil {
		t.Fatalf("failed to fetch issue: %v", err)
	}
	if fetched.Branch != "feature" {
		t.Errorf("expected branch=feature, got %q", fetched.Branch)
	}

	// Promote to main branch
	updates := map[string]interface{}{
		"branch": "main",
	}
	if err := s.UpdateIssue(ctx, originalID, updates, "test@example.com"); err != nil {
		t.Fatalf("failed to promote issue: %v", err)
	}

	// Verify branch changed
	updated, err := s.GetIssue(ctx, originalID)
	if err != nil {
		t.Fatalf("failed to fetch updated issue: %v", err)
	}
	if updated.Branch != "main" {
		t.Errorf("expected branch=main after promotion, got %q", updated.Branch)
	}
	if updated.Title != "Test issue to promote" {
		t.Errorf("title should remain unchanged")
	}
}

// TestSourcesConfig tests the sources configuration management
func TestSourcesConfig(t *testing.T) {
	tempDir := t.TempDir()
	beadsDir := filepath.Join(tempDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	// Load empty config (file doesn't exist)
	cfg, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		t.Fatalf("failed to load empty config: %v", err)
	}
	if len(cfg.Sources) != 0 {
		t.Errorf("expected empty config, got %d sources", len(cfg.Sources))
	}

	// Add a project
	if err := cfg.AddProject("beads", "github.com/steveyegge/beads"); err != nil {
		t.Fatalf("failed to add project: %v", err)
	}

	// Save config
	if err := namespace.SaveSourcesConfig(beadsDir, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Reload and verify
	reloaded, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	if len(reloaded.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(reloaded.Sources))
	}

	src, ok := reloaded.Sources["beads"]
	if !ok {
		t.Fatal("beads project not found in reloaded config")
	}
	if src.Upstream != "github.com/steveyegge/beads" {
		t.Errorf("expected upstream=github.com/steveyegge/beads, got %q", src.Upstream)
	}

	// Set fork
	if err := reloaded.SetProjectFork("beads", "github.com/matt/beads"); err != nil {
		t.Fatalf("failed to set fork: %v", err)
	}

	if err := namespace.SaveSourcesConfig(beadsDir, reloaded); err != nil {
		t.Fatalf("failed to save config with fork: %v", err)
	}

	// Reload and verify fork
	reloaded2, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		t.Fatalf("failed to reload config again: %v", err)
	}

	src2 := reloaded2.Sources["beads"]
	if src2.Fork != "github.com/matt/beads" {
		t.Errorf("expected fork=github.com/matt/beads, got %q", src2.Fork)
	}
	if src2.GetSourceURL() != "github.com/matt/beads" {
		t.Errorf("expected GetSourceURL to return fork, got %q", src2.GetSourceURL())
	}
}

// TestSourcesConfigLocal tests local override takes precedence
func TestSourcesConfigLocal(t *testing.T) {
	tempDir := t.TempDir()
	beadsDir := filepath.Join(tempDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	cfg, _ := namespace.LoadSourcesConfig(beadsDir)
	cfg.AddProject("beads", "github.com/steveyegge/beads")
	cfg.SetProjectFork("beads", "github.com/matt/beads")
	cfg.SetProjectLocal("beads", "/local/path")

	if err := namespace.SaveSourcesConfig(beadsDir, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	reloaded, _ := namespace.LoadSourcesConfig(beadsDir)
	src := reloaded.Sources["beads"]

	// Local takes precedence
	if src.GetSourceURL() != "/local/path" {
		t.Errorf("expected GetSourceURL to return local path, got %q", src.GetSourceURL())
	}
}
