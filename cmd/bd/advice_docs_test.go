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
// Advice now uses labels for targeting instead of separate fields.
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
				IssueType:   types.IssueType("advice"),
				Status:      types.StatusOpen,
				CreatedAt:   time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}
			// Add global label
			if err := s.AddLabel(ctx, advice.ID, "global", "test"); err != nil {
				t.Fatalf("Failed to add label: %v", err)
			}

			// Verify label was added
			labels, err := s.GetLabels(ctx, advice.ID)
			if err != nil {
				t.Fatalf("Failed to get labels: %v", err)
			}
			hasGlobal := false
			for _, l := range labels {
				if l == "global" {
					hasGlobal = true
					break
				}
			}
			if !hasGlobal {
				t.Error("Global advice should have 'global' label")
			}
		})

		// Example 2: Role-targeted advice
		// bd advice add "Check hook before checking mail" --role polecat
		t.Run("role-targeted advice", func(t *testing.T) {
			advice := &types.Issue{
				Title:       "Check hook before checking mail",
				Description: "The hook is authoritative. Always run 'gt hook' first on startup.",
				IssueType:   types.IssueType("advice"),
				Status:      types.StatusOpen,
				CreatedAt:   time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}
			// Add role label
			if err := s.AddLabel(ctx, advice.ID, "role:polecat", "test"); err != nil {
				t.Fatalf("Failed to add label: %v", err)
			}

			labels, err := s.GetLabels(ctx, advice.ID)
			if err != nil {
				t.Fatalf("Failed to get labels: %v", err)
			}
			hasRole := false
			for _, l := range labels {
				if l == "role:polecat" {
					hasRole = true
					break
				}
			}
			if !hasRole {
				t.Error("Role advice should have 'role:polecat' label")
			}
		})

		// Example 3: Rig-targeted advice
		// bd advice add "Use fimbaz account for spawning" --rig gastown
		t.Run("rig-targeted advice", func(t *testing.T) {
			advice := &types.Issue{
				Title:       "Use fimbaz account for spawning",
				Description: "The matthewbaker account has credential issues. Use --account fimbaz.",
				IssueType:   types.IssueType("advice"),
				Status:      types.StatusOpen,
				CreatedAt:   time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}
			// Add rig label
			if err := s.AddLabel(ctx, advice.ID, "rig:gastown", "test"); err != nil {
				t.Fatalf("Failed to add label: %v", err)
			}

			labels, err := s.GetLabels(ctx, advice.ID)
			if err != nil {
				t.Fatalf("Failed to get labels: %v", err)
			}
			hasRig := false
			for _, l := range labels {
				if l == "rig:gastown" {
					hasRig = true
					break
				}
			}
			if !hasRig {
				t.Error("Rig advice should have 'rig:gastown' label")
			}
		})

		// Example 4: Agent-specific advice
		// bd advice add "You own the shiny formula" --agent gastown/crew/prime_analyst
		t.Run("agent-specific advice", func(t *testing.T) {
			advice := &types.Issue{
				Title:       "You own the shiny formula",
				Description: "Monitor polecats using shiny and iterate on the formula based on results.",
				IssueType:   types.IssueType("advice"),
				Status:      types.StatusOpen,
				CreatedAt:   time.Now(),
			}
			if err := s.CreateIssue(ctx, advice, "test"); err != nil {
				t.Fatalf("Failed to create advice: %v", err)
			}
			// Add agent label
			if err := s.AddLabel(ctx, advice.ID, "agent:gastown/crew/prime_analyst", "test"); err != nil {
				t.Fatalf("Failed to add label: %v", err)
			}

			labels, err := s.GetLabels(ctx, advice.ID)
			if err != nil {
				t.Fatalf("Failed to get labels: %v", err)
			}
			hasAgent := false
			for _, l := range labels {
				if l == "agent:gastown/crew/prime_analyst" {
					hasAgent = true
					break
				}
			}
			if !hasAgent {
				t.Error("Agent advice should have 'agent:gastown/crew/prime_analyst' label")
			}
		})
	})

	t.Run("bd advice list examples", func(t *testing.T) {
		// Create fresh store for list tests
		tmpDir2 := t.TempDir()
		testDB2 := filepath.Join(tmpDir2, ".beads", "beads.db")
		s2 := newTestStore(t, testDB2)

		// Setup test data with labels
		globalAdvice := &types.Issue{
			Title:     "Global advice",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		s2.CreateIssue(ctx, globalAdvice, "test")
		s2.AddLabel(ctx, globalAdvice.ID, "global", "test")

		roleAdvice := &types.Issue{
			Title:     "Role advice",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		s2.CreateIssue(ctx, roleAdvice, "test")
		s2.AddLabel(ctx, roleAdvice.ID, "role:polecat", "test")

		rigAdvice := &types.Issue{
			Title:     "Rig advice",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		s2.CreateIssue(ctx, rigAdvice, "test")
		s2.AddLabel(ctx, rigAdvice.ID, "rig:gastown", "test")

		agentAdvice := &types.Issue{
			Title:     "Agent advice",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		s2.CreateIssue(ctx, agentAdvice, "test")
		s2.AddLabel(ctx, agentAdvice.ID, "agent:gastown/crew/joe", "test")

		// bd advice list - should return all open advice
		t.Run("list all", func(t *testing.T) {
			adviceType := types.IssueType("advice")
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

		// bd advice list -l role:polecat
		t.Run("filter by role label", func(t *testing.T) {
			adviceType := types.IssueType("advice")
			status := types.StatusOpen
			results, err := s2.SearchIssues(ctx, "", types.IssueFilter{
				IssueType: &adviceType,
				Status:    &status,
			})
			if err != nil {
				t.Fatalf("Failed to search: %v", err)
			}

			// Get labels and filter
			issueIDs := make([]string, len(results))
			for i, a := range results {
				issueIDs[i] = a.ID
			}
			labelsMap, _ := s2.GetLabelsForIssues(ctx, issueIDs)

			var filtered []*types.Issue
			for _, a := range results {
				for _, l := range labelsMap[a.ID] {
					if l == "role:polecat" {
						filtered = append(filtered, a)
						break
					}
				}
			}
			if len(filtered) != 1 {
				t.Errorf("Expected 1 role advice, got %d", len(filtered))
			}
		})

		// bd advice list -l rig:gastown
		t.Run("filter by rig label", func(t *testing.T) {
			adviceType := types.IssueType("advice")
			status := types.StatusOpen
			results, err := s2.SearchIssues(ctx, "", types.IssueFilter{
				IssueType: &adviceType,
				Status:    &status,
			})
			if err != nil {
				t.Fatalf("Failed to search: %v", err)
			}

			// Get labels and filter
			issueIDs := make([]string, len(results))
			for i, a := range results {
				issueIDs[i] = a.ID
			}
			labelsMap, _ := s2.GetLabelsForIssues(ctx, issueIDs)

			var filtered []*types.Issue
			for _, a := range results {
				for _, l := range labelsMap[a.ID] {
					if l == "rig:gastown" {
						filtered = append(filtered, a)
						break
					}
				}
			}
			if len(filtered) != 1 {
				t.Errorf("Expected 1 rig-level advice, got %d", len(filtered))
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
				IssueType: types.IssueType("advice"),
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
				IssueType: types.IssueType("advice"),
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

// TestAdviceLabelBasedModel documents the label-based subscription model
func TestAdviceLabelBasedModel(t *testing.T) {
	t.Run("targeting via labels", func(t *testing.T) {
		// The new model uses labels for targeting:
		// - "global" - applies to all agents
		// - "rig:X" - applies to agents in rig X
		// - "role:Y" - applies to agents with role Y
		// - "agent:Z" - applies to specific agent Z
		//
		// Agents auto-subscribe to their context labels (global, rig:X, role:Y, agent:Z)
		// This enables a flexible subscription model where:
		// - Advice can have multiple labels for reusability
		// - Agents can subscribe to additional labels (testing, security, etc.)
		t.Log("Advice uses labels for targeting instead of separate fields")
		t.Log("Example labels: global, rig:beads, role:polecat, agent:beads/polecats/quartz")
	})
}
