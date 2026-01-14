// contributor_routing_e2e_test.go - E2E tests for contributor routing
//
// These tests verify that issues are correctly routed to the planning repo
// when the user is detected as a contributor with auto-routing enabled.

//go:build integration
// +build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// TestContributorRoutingTracer is the Phase 1 tracer bullet test.
// It proves that:
// 1. ExpandPath correctly expands ~ and relative paths
// 2. Routing config is correctly read (including backward compat)
// 3. DetermineTargetRepo returns the correct repo for contributors
//
// Full store switching is deferred to Phase 2.
func TestContributorRoutingTracer(t *testing.T) {
	t.Run("ExpandPath_tilde_expansion", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("cannot get home dir: %v", err)
		}

		tests := []struct {
			input string
			want  string
		}{
			{"~/foo", filepath.Join(home, "foo")},
			{"~/bar/baz", filepath.Join(home, "bar", "baz")},
			{".", "."},
			{"", ""},
		}

		for _, tt := range tests {
			got := routing.ExpandPath(tt.input)
			if got != tt.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		}
	})

	t.Run("DetermineTargetRepo_contributor_routes_to_planning", func(t *testing.T) {
		config := &routing.RoutingConfig{
			Mode:            "auto",
			ContributorRepo: "~/.beads-planning",
		}

		got := routing.DetermineTargetRepo(config, routing.Contributor, ".")
		if got != "~/.beads-planning" {
			t.Errorf("DetermineTargetRepo() = %q, want %q", got, "~/.beads-planning")
		}
	})

	t.Run("DetermineTargetRepo_maintainer_stays_local", func(t *testing.T) {
		config := &routing.RoutingConfig{
			Mode:            "auto",
			MaintainerRepo:  ".",
			ContributorRepo: "~/.beads-planning",
		}

		got := routing.DetermineTargetRepo(config, routing.Maintainer, ".")
		if got != "." {
			t.Errorf("DetermineTargetRepo() = %q, want %q", got, ".")
		}
	})

	t.Run("E2E_routing_decision_with_store", func(t *testing.T) {
		// Set up temporary directory structure
		tmpDir := t.TempDir()
		projectDir := filepath.Join(tmpDir, "project")
		planningDir := filepath.Join(tmpDir, "planning")

		// Create project .beads directory
		projectBeadsDir := filepath.Join(projectDir, ".beads")
		if err := os.MkdirAll(projectBeadsDir, 0755); err != nil {
			t.Fatalf("failed to create project .beads dir: %v", err)
		}

		// Create planning .beads directory
		planningBeadsDir := filepath.Join(planningDir, ".beads")
		if err := os.MkdirAll(planningBeadsDir, 0755); err != nil {
			t.Fatalf("failed to create planning .beads dir: %v", err)
		}

		// Initialize project database
		projectDBPath := filepath.Join(projectBeadsDir, "beads.db")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		projectStore, err := sqlite.New(ctx, projectDBPath)
		if err != nil {
			t.Fatalf("failed to create project store: %v", err)
		}
		defer projectStore.Close()

		// Set routing config in project store (canonical keys)
		if err := projectStore.SetConfig(ctx, "routing.mode", "auto"); err != nil {
			t.Fatalf("failed to set routing.mode: %v", err)
		}
		if err := projectStore.SetConfig(ctx, "routing.contributor", planningDir); err != nil {
			t.Fatalf("failed to set routing.contributor: %v", err)
		}

		// Verify config was stored correctly
		mode, err := projectStore.GetConfig(ctx, "routing.mode")
		if err != nil {
			t.Fatalf("failed to get routing.mode: %v", err)
		}
		if mode != "auto" {
			t.Errorf("routing.mode = %q, want %q", mode, "auto")
		}

		contributorPath, err := projectStore.GetConfig(ctx, "routing.contributor")
		if err != nil {
			t.Fatalf("failed to get routing.contributor: %v", err)
		}
		if contributorPath != planningDir {
			t.Errorf("routing.contributor = %q, want %q", contributorPath, planningDir)
		}

		// Build routing config from stored values
		routingConfig := &routing.RoutingConfig{
			Mode:            mode,
			ContributorRepo: contributorPath,
		}

		// Verify routing decision for contributor
		targetRepo := routing.DetermineTargetRepo(routingConfig, routing.Contributor, projectDir)
		if targetRepo != planningDir {
			t.Errorf("DetermineTargetRepo() = %q, want %q", targetRepo, planningDir)
		}

		// Verify routing decision for maintainer stays local
		targetRepo = routing.DetermineTargetRepo(routingConfig, routing.Maintainer, projectDir)
		if targetRepo != "." {
			t.Errorf("DetermineTargetRepo() for maintainer = %q, want %q", targetRepo, ".")
		}

		// Initialize planning database and verify we can create issues there
		planningDBPath := filepath.Join(planningBeadsDir, "beads.db")
		planningStore, err := sqlite.New(ctx, planningDBPath)
		if err != nil {
			t.Fatalf("failed to create planning store: %v", err)
		}
		defer planningStore.Close()

		// Initialize planning store with required config
		if err := planningStore.SetConfig(ctx, "issue_prefix", "plan-"); err != nil {
			t.Fatalf("failed to set issue_prefix in planning store: %v", err)
		}

		// Create a test issue in planning store (simulating what Phase 2 will do)
		issue := &types.Issue{
			Title:     "Test contributor issue",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
		}

		if err := planningStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue in planning store: %v", err)
		}

		// Verify issue exists in planning store
		retrieved, err := planningStore.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("failed to get issue from planning store: %v", err)
		}
		if retrieved.Title != "Test contributor issue" {
			t.Errorf("issue title = %q, want %q", retrieved.Title, "Test contributor issue")
		}

		// Verify issue does NOT exist in project store (isolation check)
		projectIssue, _ := projectStore.GetIssue(ctx, issue.ID)
		if projectIssue != nil {
			t.Error("issue should NOT exist in project store (isolation failure)")
		}
	})
}

// TestBackwardCompatContributorConfig verifies legacy contributor.* keys still work
func TestBackwardCompatContributorConfig(t *testing.T) {
	// Set up temporary directory
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Initialize database
	dbPath := filepath.Join(beadsDir, "beads.db")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Set LEGACY contributor.* keys (what old versions of bd init --contributor would set)
	if err := store.SetConfig(ctx, "contributor.auto_route", "true"); err != nil {
		t.Fatalf("failed to set contributor.auto_route: %v", err)
	}
	if err := store.SetConfig(ctx, "contributor.planning_repo", "/legacy/planning"); err != nil {
		t.Fatalf("failed to set contributor.planning_repo: %v", err)
	}

	// Simulate backward compat read (as done in create.go)
	routingMode, _ := store.GetConfig(ctx, "routing.mode")
	contributorRepo, _ := store.GetConfig(ctx, "routing.contributor")

	// Fallback to legacy keys
	if routingMode == "" {
		legacyAutoRoute, _ := store.GetConfig(ctx, "contributor.auto_route")
		if legacyAutoRoute == "true" {
			routingMode = "auto"
		}
	}
	if contributorRepo == "" {
		legacyPlanningRepo, _ := store.GetConfig(ctx, "contributor.planning_repo")
		contributorRepo = legacyPlanningRepo
	}

	// Verify backward compat works
	if routingMode != "auto" {
		t.Errorf("backward compat routing.mode = %q, want %q", routingMode, "auto")
	}
	if contributorRepo != "/legacy/planning" {
		t.Errorf("backward compat routing.contributor = %q, want %q", contributorRepo, "/legacy/planning")
	}

	// Build routing config and verify it routes correctly
	config := &routing.RoutingConfig{
		Mode:            routingMode,
		ContributorRepo: contributorRepo,
	}

	targetRepo := routing.DetermineTargetRepo(config, routing.Contributor, ".")
	if targetRepo != "/legacy/planning" {
		t.Errorf("DetermineTargetRepo() with legacy config = %q, want %q", targetRepo, "/legacy/planning")
	}
}
