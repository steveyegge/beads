package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestAdviceDocsExamples tests examples from agent-advice.md documentation
// to ensure they work correctly and the documentation is accurate.
func TestAdviceDocsExamples(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	t.Run("bd advice add examples", func(t *testing.T) {
		// Example 1: Global advice (all agents)
		// bd advice add "Always verify git status before pushing" \
		//   -d "Run 'git status' to check for uncommitted changes before 'git push'"
		t.Run("global advice", func(t *testing.T) {
			advice := &types.Issue{
				Title:       "Always verify git status before pushing",
				Description: "Run 'git status' to check for uncommitted changes before 'git push'",
				IssueType:   types.TypeAdvice,
				Status:      types.StatusOpen,
				CreatedAt:   time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}

			// Verify it was created as global (no targeting)
			if advice.AdviceTargetRig != "" || advice.AdviceTargetRole != "" || advice.AdviceTargetAgent != "" {
				t.Error("Global advice should have empty targeting fields")
			}
		})

		// Example 2: Role-targeted advice (note: requires --rig in actual CLI)
		// Documentation says: bd advice add "Check hook before checking mail" --role polecat
		// But code requires --rig with --role
		t.Run("role-targeted advice requires rig", func(t *testing.T) {
			// The correct CLI usage should be:
			// bd advice add "Check hook before checking mail" --rig gastown --role polecat
			advice := &types.Issue{
				Title:            "Check hook before checking mail",
				Description:      "The hook is authoritative. Always run 'gt hook' first on startup.",
				IssueType:        types.TypeAdvice,
				Status:           types.StatusOpen,
				AdviceTargetRig:  "gastown", // Required with role
				AdviceTargetRole: "polecat",
				CreatedAt:        time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}

			if advice.AdviceTargetRole != "polecat" {
				t.Errorf("Expected role 'polecat', got %q", advice.AdviceTargetRole)
			}
			if advice.AdviceTargetRig != "gastown" {
				t.Errorf("Role advice should have rig set, got %q", advice.AdviceTargetRig)
			}
		})

		// Example 3: Rig-targeted advice
		// bd advice add "Use fimbaz account for spawning" --rig gastown
		t.Run("rig-targeted advice", func(t *testing.T) {
			advice := &types.Issue{
				Title:           "Use fimbaz account for spawning",
				Description:     "The matthewbaker account has credential issues. Use --account fimbaz.",
				IssueType:       types.TypeAdvice,
				Status:          types.StatusOpen,
				AdviceTargetRig: "gastown",
				CreatedAt:       time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}

			if advice.AdviceTargetRig != "gastown" {
				t.Errorf("Expected rig 'gastown', got %q", advice.AdviceTargetRig)
			}
			if advice.AdviceTargetRole != "" {
				t.Errorf("Rig advice should have empty role, got %q", advice.AdviceTargetRole)
			}
		})

		// Example 4: Agent-specific advice
		// bd advice add "You own the shiny formula" --agent gastown/crew/prime_analyst
		t.Run("agent-specific advice", func(t *testing.T) {
			advice := &types.Issue{
				Title:             "You own the shiny formula",
				Description:       "Monitor polecats using shiny and iterate on the formula based on results.",
				IssueType:         types.TypeAdvice,
				Status:            types.StatusOpen,
				AdviceTargetAgent: "gastown/crew/prime_analyst",
				CreatedAt:         time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}

			if advice.AdviceTargetAgent != "gastown/crew/prime_analyst" {
				t.Errorf("Expected agent 'gastown/crew/prime_analyst', got %q", advice.AdviceTargetAgent)
			}
		})
	})

	t.Run("bd advice list examples", func(t *testing.T) {
		// Create fresh store for list tests
		tmpDir2 := t.TempDir()
		testDB2 := filepath.Join(tmpDir2, ".beads", "beads.db")
		s2 := newTestStore(t, testDB2)

		// Setup test data
		globalAdvice := &types.Issue{
			Title:     "Global advice",
			IssueType: types.TypeAdvice,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		s2.CreateIssue(ctx, globalAdvice, "test")

		roleAdvice := &types.Issue{
			Title:            "Role advice",
			IssueType:        types.TypeAdvice,
			Status:           types.StatusOpen,
			AdviceTargetRig:  "gastown",
			AdviceTargetRole: "polecat",
			CreatedAt:        time.Now(),
		}
		s2.CreateIssue(ctx, roleAdvice, "test")

		rigAdvice := &types.Issue{
			Title:           "Rig advice",
			IssueType:       types.TypeAdvice,
			Status:          types.StatusOpen,
			AdviceTargetRig: "gastown",
			CreatedAt:       time.Now(),
		}
		s2.CreateIssue(ctx, rigAdvice, "test")

		agentAdvice := &types.Issue{
			Title:             "Agent advice",
			IssueType:         types.TypeAdvice,
			Status:            types.StatusOpen,
			AdviceTargetAgent: "gastown/crew/joe",
			CreatedAt:         time.Now(),
		}
		s2.CreateIssue(ctx, agentAdvice, "test")

		// bd advice list - should return all open advice
		t.Run("list all", func(t *testing.T) {
			adviceType := types.TypeAdvice
			status := types.StatusOpen
			results, err := s2.SearchIssues(ctx, "", types.IssueFilter{
				IssueType: &adviceType,
				Status:    &status,
			})
			if err != nil {
				t.Fatalf("Failed to search: %v", err)
			}
			if len(results) != 4 {
				t.Errorf("Expected 4 advice items, got %d", len(results))
			}
		})

		// bd advice list --role polecat (actually --rig gastown --role polecat)
		t.Run("filter by role", func(t *testing.T) {
			adviceType := types.TypeAdvice
			status := types.StatusOpen
			results, err := s2.SearchIssues(ctx, "", types.IssueFilter{
				IssueType: &adviceType,
				Status:    &status,
			})
			if err != nil {
				t.Fatalf("Failed to search: %v", err)
			}

			// Filter in-memory by role
			var filtered []*types.Issue
			for _, a := range results {
				if a.AdviceTargetRig == "gastown" && a.AdviceTargetRole == "polecat" {
					filtered = append(filtered, a)
				}
			}
			if len(filtered) != 1 {
				t.Errorf("Expected 1 role advice, got %d", len(filtered))
			}
		})

		// bd advice list --rig gastown
		t.Run("filter by rig", func(t *testing.T) {
			adviceType := types.TypeAdvice
			status := types.StatusOpen
			results, err := s2.SearchIssues(ctx, "", types.IssueFilter{
				IssueType: &adviceType,
				Status:    &status,
			})
			if err != nil {
				t.Fatalf("Failed to search: %v", err)
			}

			// Filter for rig-only advice (not role)
			var filtered []*types.Issue
			for _, a := range results {
				if a.AdviceTargetRig == "gastown" && a.AdviceTargetRole == "" && a.AdviceTargetAgent == "" {
					filtered = append(filtered, a)
				}
			}
			if len(filtered) != 1 {
				t.Errorf("Expected 1 rig-level advice, got %d", len(filtered))
			}
		})

		// bd advice list --agent gastown/crew/joe
		t.Run("filter by agent", func(t *testing.T) {
			adviceType := types.TypeAdvice
			status := types.StatusOpen
			results, err := s2.SearchIssues(ctx, "", types.IssueFilter{
				IssueType: &adviceType,
				Status:    &status,
			})
			if err != nil {
				t.Fatalf("Failed to search: %v", err)
			}

			// Filter by agent
			var filtered []*types.Issue
			for _, a := range results {
				if a.AdviceTargetAgent == "gastown/crew/joe" {
					filtered = append(filtered, a)
				}
			}
			if len(filtered) != 1 {
				t.Errorf("Expected 1 agent advice, got %d", len(filtered))
			}
		})
	})

	t.Run("bd advice remove examples", func(t *testing.T) {
		// Create fresh store for remove tests
		tmpDir3 := t.TempDir()
		testDB3 := filepath.Join(tmpDir3, ".beads", "beads.db")
		s3 := newTestStore(t, testDB3)

		// bd advice remove gt-tsk-xyz - closes advice
		t.Run("remove by closing", func(t *testing.T) {
			advice := &types.Issue{
				Title:     "Advice to remove",
				IssueType: types.TypeAdvice,
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			}
			if err := s3.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create: %v", err)
			}

			// Close the advice
			if err := s3.CloseIssue(ctx, advice.ID, "No longer applicable after deploy", "test", ""); err != nil {
				t.Fatalf("Failed to close: %v", err)
			}

			// Verify it's closed
			after, _ := s3.GetIssue(ctx, advice.ID)
			if after.Status != types.StatusClosed {
				t.Errorf("Expected closed status, got %s", after.Status)
			}
		})

		// bd advice remove gt-tsk-xyz --delete - permanently deletes
		t.Run("remove with delete flag", func(t *testing.T) {
			advice := &types.Issue{
				Title:     "Advice to delete",
				IssueType: types.TypeAdvice,
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			}
			if err := s3.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create: %v", err)
			}

			// Delete the advice
			if err := s3.DeleteIssue(ctx, advice.ID); err != nil {
				t.Fatalf("Failed to delete: %v", err)
			}

			// Verify it's gone or tombstoned
			after, _ := s3.GetIssue(ctx, advice.ID)
			if after != nil && after.Status != types.StatusTombstone {
				t.Error("Deleted advice should not be retrievable")
			}
		})
	})
}

// TestAdviceDocsDiscrepancies documents known discrepancies between
// documentation and actual CLI behavior.
func TestAdviceDocsDiscrepancies(t *testing.T) {
	t.Run("role flag requires rig", func(t *testing.T) {
		// Documentation example:
		//   bd advice add "Check hook before checking mail" --role polecat
		//
		// But code requires:
		//   bd advice add "Check hook before checking mail" --rig gastown --role polecat
		//
		// The CLI validates: "FatalError("--role requires --rig to specify which rig the role belongs to")"
		//
		// RECOMMENDATION: Update documentation to show --rig is required with --role

		// This test documents the expected behavior
		t.Log("Documentation at agent-advice.md needs updating:")
		t.Log("- Examples using --role without --rig will fail")
		t.Log("- The --role flag requires --rig to be specified")
	})
}

// TestAdviceOutputFormat tests the output format described in documentation
func TestAdviceOutputFormat(t *testing.T) {
	// Documentation shows output format:
	// ## 📝 Agent Advice
	//
	// **[Polecat]** Check hook before checking mail
	//   The hook is authoritative. Always run 'gt hook' first on startup.
	//
	// **[Global]** Always verify git status before pushing
	//   Run 'git status' to check for uncommitted changes before 'git push'

	tests := []struct {
		name          string
		rig           string
		role          string
		agent         string
		expectedScope string
	}{
		{"global scope", "", "", "", "Global"},
		{"rig scope", "beads", "", "", "beads"},         // Uses rig name
		{"role scope", "beads", "polecat", "", "Polecat"}, // Title case role
		{"agent scope", "", "", "beads/crew/wolf", "Agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &types.Issue{
				AdviceTargetRig:   tt.rig,
				AdviceTargetRole:  tt.role,
				AdviceTargetAgent: tt.agent,
			}

			scope := getAdviceScopeForTest(issue)
			if scope != tt.expectedScope {
				t.Errorf("Expected scope %q, got %q", tt.expectedScope, scope)
			}
		})
	}
}

// getAdviceScopeForTest replicates the scope display logic from advice_list.go
func getAdviceScopeForTest(issue *types.Issue) string {
	if issue.AdviceTargetAgent != "" {
		return "Agent"
	}
	if issue.AdviceTargetRole != "" {
		// Capitalize first letter
		if len(issue.AdviceTargetRole) > 0 {
			return string(issue.AdviceTargetRole[0]-32) + issue.AdviceTargetRole[1:]
		}
		return issue.AdviceTargetRole
	}
	if issue.AdviceTargetRig != "" {
		return issue.AdviceTargetRig
	}
	return "Global"
}
