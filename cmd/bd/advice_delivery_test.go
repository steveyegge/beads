package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestAdviceDeliveryPipeline tests the advice matching logic used by gt prime
// to deliver advice to agents. Advice uses labels for targeting:
//   - "global" - applies to all agents
//   - "rig:X" - applies to agents in rig X
//   - "role:Y" - applies to agents with role Y
//   - "agent:Z" - applies to specific agent Z
func TestAdviceDeliveryPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// Setup: Create advice with various label scopes
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
	if err := s.AddLabel(ctx, globalAdvice.ID, "global", "test"); err != nil {
		t.Fatalf("Failed to add global label: %v", err)
	}

	beadsRigAdvice := &types.Issue{
		Title:       "Beads rig: Use go test",
		Description: "Run go test ./... for testing",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, beadsRigAdvice, "test"); err != nil {
		t.Fatalf("Failed to create rig advice: %v", err)
	}
	if err := s.AddLabel(ctx, beadsRigAdvice.ID, "rig:beads", "test"); err != nil {
		t.Fatalf("Failed to add rig label: %v", err)
	}

	gastownRigAdvice := &types.Issue{
		Title:       "Gastown rig: Check mayor",
		Description: "Coordinate with mayor for cross-rig work",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, gastownRigAdvice, "test"); err != nil {
		t.Fatalf("Failed to create gastown rig advice: %v", err)
	}
	if err := s.AddLabel(ctx, gastownRigAdvice.ID, "rig:gastown", "test"); err != nil {
		t.Fatalf("Failed to add rig label: %v", err)
	}

	polecatRoleAdvice := &types.Issue{
		Title:       "Polecat role: Complete before gt done",
		Description: "Finish work before running gt done",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, polecatRoleAdvice, "test"); err != nil {
		t.Fatalf("Failed to create role advice: %v", err)
	}
	if err := s.AddLabel(ctx, polecatRoleAdvice.ID, "role:polecat", "test"); err != nil {
		t.Fatalf("Failed to add role label: %v", err)
	}

	crewRoleAdvice := &types.Issue{
		Title:       "Crew role: Maintain formulas",
		Description: "Crew members maintain workflow formulas",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, crewRoleAdvice, "test"); err != nil {
		t.Fatalf("Failed to create crew role advice: %v", err)
	}
	if err := s.AddLabel(ctx, crewRoleAdvice.ID, "role:crew", "test"); err != nil {
		t.Fatalf("Failed to add role label: %v", err)
	}

	specificAgentAdvice := &types.Issue{
		Title:       "Agent: Focus on CLI",
		Description: "quartz specializes in CLI implementation",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, specificAgentAdvice, "test"); err != nil {
		t.Fatalf("Failed to create agent advice: %v", err)
	}
	if err := s.AddLabel(ctx, specificAgentAdvice.ID, "agent:beads/polecats/quartz", "test"); err != nil {
		t.Fatalf("Failed to add agent label: %v", err)
	}

	// Helper function to get applicable advice for an agent using label-based subscriptions
	getApplicableAdvice := func(agentID string) []*types.Issue {
		adviceType := types.IssueType("advice")
		status := types.StatusOpen
		allAdvice, err := s.SearchIssues(ctx, "", types.IssueFilter{
			IssueType: &adviceType,
			Status:    &status,
		})
		if err != nil {
			t.Fatalf("Failed to search advice: %v", err)
		}

		// Build subscriptions for the agent
		subscriptions := buildAgentSubscriptions(agentID, nil)

		// Get labels for all advice
		issueIDs := make([]string, len(allAdvice))
		for i, a := range allAdvice {
			issueIDs[i] = a.ID
		}
		labelsMap, err := s.GetLabelsForIssues(ctx, issueIDs)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}

		var applicable []*types.Issue
		for _, advice := range allAdvice {
			if matchesSubscriptions(advice, labelsMap[advice.ID], subscriptions) {
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
			applicable := getApplicableAdvice(agentID)
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
		beadsAgent := getApplicableAdvice("beads/polecats/quartz")
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
			t.Error("beads/polecats/quartz should see beads rig advice")
		}
		if foundGastown {
			t.Error("beads/polecats/quartz should NOT see gastown rig advice")
		}

		// gastown/polecats/alpha should see gastown rig advice
		gastownAgent := getApplicableAdvice("gastown/polecats/alpha")
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
			t.Error("gastown/polecats/alpha should NOT see beads rig advice")
		}
		if !foundGastown {
			t.Error("gastown/polecats/alpha should see gastown rig advice")
		}
	})

	t.Run("role-targeted advice appears for matching roles", func(t *testing.T) {
		// beads/polecats/quartz should see polecat role advice
		polecatAgent := getApplicableAdvice("beads/polecats/quartz")
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
			t.Error("beads/polecats/quartz should see polecat role advice")
		}
		if foundCrew {
			t.Error("beads/polecats/quartz should NOT see crew role advice")
		}

		// beads/crew/wolf should see crew role advice
		crewAgent := getApplicableAdvice("beads/crew/wolf")
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
			t.Error("beads/crew/wolf should NOT see polecat role advice")
		}
		if !foundCrew {
			t.Error("beads/crew/wolf should see crew role advice")
		}
	})

	t.Run("agent-targeted advice appears only for that agent", func(t *testing.T) {
		// beads/polecats/quartz should see the agent-specific advice
		quartzAdvice := getApplicableAdvice("beads/polecats/quartz")
		foundSpecific := false
		for _, a := range quartzAdvice {
			if a.ID == specificAgentAdvice.ID {
				foundSpecific = true
				break
			}
		}
		if !foundSpecific {
			t.Error("beads/polecats/quartz should see agent-specific advice")
		}

		// beads/polecats/garnet should NOT see the advice for quartz
		garnetAdvice := getApplicableAdvice("beads/polecats/garnet")
		foundSpecific = false
		for _, a := range garnetAdvice {
			if a.ID == specificAgentAdvice.ID {
				foundSpecific = true
				break
			}
		}
		if foundSpecific {
			t.Error("beads/polecats/garnet should NOT see agent-specific advice for quartz")
		}
	})

	t.Run("closed advice not delivered", func(t *testing.T) {
		// Close the global advice
		if err := s.UpdateIssue(ctx, globalAdvice.ID, map[string]interface{}{
			"status": types.StatusClosed,
		}, "test"); err != nil {
			t.Fatalf("Failed to close advice: %v", err)
		}

		// Should no longer appear
		applicable := getApplicableAdvice("beads/polecats/quartz")
		for _, a := range applicable {
			if a.ID == globalAdvice.ID {
				t.Error("Closed advice should not be delivered")
			}
		}
	})
}

// TestAdviceSubscriptionModel tests the label-based subscription model
func TestAdviceSubscriptionModel(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// Create advice with multiple labels
	testingAdvice := &types.Issue{
		Title:       "Testing best practices",
		Description: "Write tests first",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, testingAdvice, "test"); err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if err := s.AddLabel(ctx, testingAdvice.ID, "testing", "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}
	if err := s.AddLabel(ctx, testingAdvice.ID, "go", "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	securityAdvice := &types.Issue{
		Title:       "Security guidelines",
		Description: "Check for secrets",
		IssueType:   types.IssueType("advice"),
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := s.CreateIssue(ctx, securityAdvice, "test"); err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if err := s.AddLabel(ctx, securityAdvice.ID, "security", "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Test subscription matching
	allAdvice, err := s.SearchIssues(ctx, "", types.IssueFilter{
		IssueType: func() *types.IssueType { t := types.IssueType("advice"); return &t }(),
		Status:    func() *types.Status { s := types.StatusOpen; return &s }(),
	})
	if err != nil {
		t.Fatalf("Failed to search advice: %v", err)
	}

	issueIDs := make([]string, len(allAdvice))
	for i, a := range allAdvice {
		issueIDs[i] = a.ID
	}
	labelsMap, err := s.GetLabelsForIssues(ctx, issueIDs)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	t.Run("subscription matches labels", func(t *testing.T) {
		// Subscribe to testing - should see testing advice
		subscriptions := []string{"testing"}
		matched := 0
		for _, advice := range allAdvice {
			if matchesSubscriptions(advice, labelsMap[advice.ID], subscriptions) {
				matched++
			}
		}
		if matched != 1 {
			t.Errorf("Expected 1 advice matching 'testing' subscription, got %d", matched)
		}

		// Subscribe to security - should see security advice
		subscriptions = []string{"security"}
		matched = 0
		for _, advice := range allAdvice {
			if matchesSubscriptions(advice, labelsMap[advice.ID], subscriptions) {
				matched++
			}
		}
		if matched != 1 {
			t.Errorf("Expected 1 advice matching 'security' subscription, got %d", matched)
		}

		// Subscribe to both - should see both
		subscriptions = []string{"testing", "security"}
		matched = 0
		for _, advice := range allAdvice {
			if matchesSubscriptions(advice, labelsMap[advice.ID], subscriptions) {
				matched++
			}
		}
		if matched != 2 {
			t.Errorf("Expected 2 advice matching 'testing,security' subscriptions, got %d", matched)
		}
	})
}
