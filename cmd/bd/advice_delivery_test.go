package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestAdviceDeliveryPipeline tests the advice matching logic used by gt prime
// to deliver advice to agents. This tests the beads-side logic that gt prime calls.
func TestAdviceDeliveryPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// Setup: Create advice at various scopes
	globalAdvice := &types.Issue{
		Title:       "Global: Check hook first",
		Description: "Always run gt hook at session start",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, globalAdvice, "test"); err != nil {
		t.Fatalf("Failed to create global advice: %v", err)
	}

	beadsRigAdvice := &types.Issue{
		Title:           "Beads rig: Use go test",
		Description:     "Run go test ./... for testing",
		IssueType:       types.IssueType("advice"),
		Status:          types.StatusOpen,
		AdviceTargetRig: "beads",
		CreatedAt:       time.Now(),
	}
	if err := s.CreateIssue(ctx, beadsRigAdvice, "test"); err != nil {
		t.Fatalf("Failed to create rig advice: %v", err)
	}

	gastownRigAdvice := &types.Issue{
		Title:           "Gastown rig: Check mayor",
		Description:     "Coordinate with mayor for cross-rig work",
		IssueType:       types.IssueType("advice"),
		Status:          types.StatusOpen,
		AdviceTargetRig: "gastown",
		CreatedAt:       time.Now(),
	}
	if err := s.CreateIssue(ctx, gastownRigAdvice, "test"); err != nil {
		t.Fatalf("Failed to create gastown rig advice: %v", err)
	}

	polecatRoleAdvice := &types.Issue{
		Title:            "Polecat role: Complete before gt done",
		Description:      "Finish work before running gt done",
		IssueType:        types.IssueType("advice"),
		Status:           types.StatusOpen,
		AdviceTargetRig:  "beads",
		AdviceTargetRole: "polecat",
		CreatedAt:        time.Now(),
	}
	if err := s.CreateIssue(ctx, polecatRoleAdvice, "test"); err != nil {
		t.Fatalf("Failed to create role advice: %v", err)
	}

	crewRoleAdvice := &types.Issue{
		Title:            "Crew role: Maintain formulas",
		Description:      "Crew members maintain workflow formulas",
		IssueType:        types.IssueType("advice"),
		Status:           types.StatusOpen,
		AdviceTargetRig:  "beads",
		AdviceTargetRole: "crew",
		CreatedAt:        time.Now(),
	}
	if err := s.CreateIssue(ctx, crewRoleAdvice, "test"); err != nil {
		t.Fatalf("Failed to create crew role advice: %v", err)
	}

	specificAgentAdvice := &types.Issue{
		Title:             "Agent: Focus on CLI",
		Description:       "quartz specializes in CLI implementation",
		IssueType:         types.IssueType("advice"),
		Status:            types.StatusOpen,
		AdviceTargetAgent: "beads/polecats/quartz",
		CreatedAt:         time.Now(),
	}
	if err := s.CreateIssue(ctx, specificAgentAdvice, "test"); err != nil {
		t.Fatalf("Failed to create agent advice: %v", err)
	}

	// Helper function to get applicable advice for an agent
	getApplicableAdvice := func(agentID, roleType, rigName string) []*types.Issue {
		adviceType := types.IssueType("advice")
		status := types.StatusOpen
		allAdvice, err := s.SearchIssues(ctx, "", types.IssueFilter{
			IssueType: &adviceType,
			Status:    &status,
		})
		if err != nil {
			t.Fatalf("Failed to search advice: %v", err)
		}

		var applicable []*types.Issue
		for _, advice := range allAdvice {
			if matchesAgentScope(advice, agentID) {
				applicable = append(applicable, advice)
			}
		}
		return applicable
	}

	t.Run("global advice appears for all agents", func(t *testing.T) {
		testCases := []string{
			"beads/polecats/quartz",
			"beads/crew/wolf",
			"gastown/polecats/alpha",
			"gastown/crew/decision_notify",
		}

		for _, agentID := range testCases {
			applicable := getApplicableAdvice(agentID, "", "")
			found := false
			for _, a := range applicable {
				if a.ID == globalAdvice.ID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Global advice should appear for agent %s", agentID)
			}
		}
	})

	t.Run("rig-targeted advice only appears in that rig", func(t *testing.T) {
		// beads/polecats/quartz should see beads rig advice
		beadsAgent := getApplicableAdvice("beads/polecats/quartz", "polecat", "beads")
		foundBeads := false
		foundGastown := false
		for _, a := range beadsAgent {
			if a.ID == beadsRigAdvice.ID {
				foundBeads = true
			}
			if a.ID == gastownRigAdvice.ID {
				foundGastown = true
			}
		}
		if !foundBeads {
			t.Error("beads agent should see beads rig advice")
		}
		if foundGastown {
			t.Error("beads agent should NOT see gastown rig advice")
		}

		// gastown/polecats/alpha should see gastown rig advice
		gastownAgent := getApplicableAdvice("gastown/polecats/alpha", "polecat", "gastown")
		foundBeads = false
		foundGastown = false
		for _, a := range gastownAgent {
			if a.ID == beadsRigAdvice.ID {
				foundBeads = true
			}
			if a.ID == gastownRigAdvice.ID {
				foundGastown = true
			}
		}
		if foundBeads {
			t.Error("gastown agent should NOT see beads rig advice")
		}
		if !foundGastown {
			t.Error("gastown agent should see gastown rig advice")
		}
	})

	t.Run("role-targeted advice only appears for that role", func(t *testing.T) {
		// beads/polecats/quartz should see polecat role advice
		polecatAgent := getApplicableAdvice("beads/polecats/quartz", "polecat", "beads")
		foundPolecat := false
		foundCrew := false
		for _, a := range polecatAgent {
			if a.ID == polecatRoleAdvice.ID {
				foundPolecat = true
			}
			if a.ID == crewRoleAdvice.ID {
				foundCrew = true
			}
		}
		if !foundPolecat {
			t.Error("polecat agent should see polecat role advice")
		}
		if foundCrew {
			t.Error("polecat agent should NOT see crew role advice")
		}

		// beads/crew/wolf should see crew role advice
		crewAgent := getApplicableAdvice("beads/crew/wolf", "crew", "beads")
		foundPolecat = false
		foundCrew = false
		for _, a := range crewAgent {
			if a.ID == polecatRoleAdvice.ID {
				foundPolecat = true
			}
			if a.ID == crewRoleAdvice.ID {
				foundCrew = true
			}
		}
		if foundPolecat {
			t.Error("crew agent should NOT see polecat role advice")
		}
		if !foundCrew {
			t.Error("crew agent should see crew role advice")
		}
	})

	t.Run("agent-targeted advice only appears for that agent", func(t *testing.T) {
		// beads/polecats/quartz should see its specific advice
		quartz := getApplicableAdvice("beads/polecats/quartz", "polecat", "beads")
		found := false
		for _, a := range quartz {
			if a.ID == specificAgentAdvice.ID {
				found = true
				break
			}
		}
		if !found {
			t.Error("quartz should see its specific advice")
		}

		// beads/polecats/alpha should NOT see quartz's advice
		alpha := getApplicableAdvice("beads/polecats/alpha", "polecat", "beads")
		found = false
		for _, a := range alpha {
			if a.ID == specificAgentAdvice.ID {
				found = true
				break
			}
		}
		if found {
			t.Error("alpha should NOT see quartz's specific advice")
		}
	})

	t.Run("agent sees all applicable advice (inheritance)", func(t *testing.T) {
		// beads/polecats/quartz should see:
		// - global advice
		// - beads rig advice
		// - polecat role advice
		// - quartz agent advice
		// Total: 4 advice items

		quartz := getApplicableAdvice("beads/polecats/quartz", "polecat", "beads")

		expectedIDs := map[string]bool{
			globalAdvice.ID:        false,
			beadsRigAdvice.ID:      false,
			polecatRoleAdvice.ID:   false,
			specificAgentAdvice.ID: false,
		}

		for _, a := range quartz {
			if _, expected := expectedIDs[a.ID]; expected {
				expectedIDs[a.ID] = true
			}
		}

		for id, found := range expectedIDs {
			if !found {
				t.Errorf("quartz should have received advice %s", id)
			}
		}
	})

	t.Run("agent does not see irrelevant advice", func(t *testing.T) {
		// beads/crew/wolf should NOT see:
		// - gastown rig advice
		// - polecat role advice
		// - quartz agent advice

		wolf := getApplicableAdvice("beads/crew/wolf", "crew", "beads")

		unexpectedIDs := []string{
			gastownRigAdvice.ID,
			polecatRoleAdvice.ID,
			specificAgentAdvice.ID,
		}

		for _, unexpected := range unexpectedIDs {
			for _, a := range wolf {
				if a.ID == unexpected {
					t.Errorf("wolf should NOT see advice %s", unexpected)
				}
			}
		}
	})
}

func TestAdviceDeliveryWithNoAdvice(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// No advice created - verify clean output
	adviceType := types.IssueType("advice")
	status := types.StatusOpen
	results, err := s.SearchIssues(ctx, "", types.IssueFilter{
		IssueType: &adviceType,
		Status:    &status,
	})
	if err != nil {
		t.Fatalf("Failed to search advice: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 advice with empty database, got %d", len(results))
	}
}

func TestAdviceDeliveryPriorityOrder(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// Create advice with different priorities
	highPriority := &types.Issue{
		Title:     "High priority advice",
		IssueType: types.IssueType("advice"),
		Status:    types.StatusOpen,
		Priority:  1,
		CreatedAt: time.Now(),
	}
	if err := s.CreateIssue(ctx, highPriority, "test"); err != nil {
		t.Fatalf("Failed to create high priority advice: %v", err)
	}

	lowPriority := &types.Issue{
		Title:     "Low priority advice",
		IssueType: types.IssueType("advice"),
		Status:    types.StatusOpen,
		Priority:  3,
		CreatedAt: time.Now().Add(-time.Hour), // Created earlier
	}
	if err := s.CreateIssue(ctx, lowPriority, "test"); err != nil {
		t.Fatalf("Failed to create low priority advice: %v", err)
	}

	medPriority := &types.Issue{
		Title:     "Medium priority advice",
		IssueType: types.IssueType("advice"),
		Status:    types.StatusOpen,
		Priority:  2,
		CreatedAt: time.Now(),
	}
	if err := s.CreateIssue(ctx, medPriority, "test"); err != nil {
		t.Fatalf("Failed to create medium priority advice: %v", err)
	}

	// Search advice - should be ordered by priority ASC
	adviceType := types.IssueType("advice")
	status := types.StatusOpen
	results, err := s.SearchIssues(ctx, "", types.IssueFilter{
		IssueType: &adviceType,
		Status:    &status,
	})
	if err != nil {
		t.Fatalf("Failed to search advice: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 advice, got %d", len(results))
	}

	// Verify priority ordering (priority 1 first, then 2, then 3)
	if results[0].Priority != 1 {
		t.Errorf("First advice should be priority 1, got %d", results[0].Priority)
	}
	if results[1].Priority != 2 {
		t.Errorf("Second advice should be priority 2, got %d", results[1].Priority)
	}
	if results[2].Priority != 3 {
		t.Errorf("Third advice should be priority 3, got %d", results[2].Priority)
	}
}

func TestAdviceDeliveryScopeHierarchy(t *testing.T) {
	// Test the scope hierarchy ordering: global < rig < role < agent
	// More specific scopes should take precedence in display

	tests := []struct {
		name                  string
		adviceTargetRig       string
		adviceTargetRole      string
		adviceTargetAgent     string
		expectedScopeCategory string
	}{
		{
			name:                  "global advice",
			adviceTargetRig:       "",
			adviceTargetRole:      "",
			adviceTargetAgent:     "",
			expectedScopeCategory: "global",
		},
		{
			name:                  "rig-level advice",
			adviceTargetRig:       "beads",
			adviceTargetRole:      "",
			adviceTargetAgent:     "",
			expectedScopeCategory: "rig",
		},
		{
			name:                  "role-level advice",
			adviceTargetRig:       "beads",
			adviceTargetRole:      "polecat",
			adviceTargetAgent:     "",
			expectedScopeCategory: "role",
		},
		{
			name:                  "agent-level advice",
			adviceTargetRig:       "",
			adviceTargetRole:      "",
			adviceTargetAgent:     "beads/polecats/quartz",
			expectedScopeCategory: "agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &types.Issue{
				AdviceTargetRig:   tt.adviceTargetRig,
				AdviceTargetRole:  tt.adviceTargetRole,
				AdviceTargetAgent: tt.adviceTargetAgent,
			}

			scope := categorizeScopeForTest(issue)
			if scope != tt.expectedScopeCategory {
				t.Errorf("Expected scope %q, got %q", tt.expectedScopeCategory, scope)
			}
		})
	}
}

// categorizeScopeForTest categorizes advice by scope level
func categorizeScopeForTest(issue *types.Issue) string {
	if issue.AdviceTargetAgent != "" {
		return "agent"
	}
	if issue.AdviceTargetRole != "" {
		return "role"
	}
	if issue.AdviceTargetRig != "" {
		return "rig"
	}
	return "global"
}
