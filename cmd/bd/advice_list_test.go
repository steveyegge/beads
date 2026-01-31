package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// adviceListTestHelper provides test setup and assertion methods for advice list tests
type adviceListTestHelper struct {
	t      *testing.T
	ctx    context.Context
	store  *sqlite.SQLiteStorage
	advice []*types.Issue
}

func newAdviceListTestHelper(t *testing.T, store *sqlite.SQLiteStorage) *adviceListTestHelper {
	return &adviceListTestHelper{t: t, ctx: context.Background(), store: store}
}

func (h *adviceListTestHelper) createAdvice(title, description, rig, role, agent string, status types.Status) *types.Issue {
	advice := &types.Issue{
		Title:             title,
		Description:       description,
		Priority:          2,
		IssueType:         types.TypeAdvice,
		Status:            status,
		AdviceTargetRig:   rig,
		AdviceTargetRole:  role,
		AdviceTargetAgent: agent,
		CreatedAt:         time.Now(),
	}
	if err := h.store.CreateIssue(h.ctx, advice, "test-user"); err != nil {
		h.t.Fatalf("Failed to create advice: %v", err)
	}
	h.advice = append(h.advice, advice)
	return advice
}

func (h *adviceListTestHelper) searchAdvice(filter types.IssueFilter) []*types.Issue {
	adviceType := types.TypeAdvice
	filter.IssueType = &adviceType
	results, err := h.store.SearchIssues(h.ctx, "", filter)
	if err != nil {
		h.t.Fatalf("Failed to search advice: %v", err)
	}
	return results
}

func (h *adviceListTestHelper) assertCount(count, expected int, desc string) {
	if count != expected {
		h.t.Errorf("Expected %d %s, got %d", expected, desc, count)
	}
}

func TestAdviceListSuite(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)

	t.Run("AdviceListCommand", func(t *testing.T) {
		h := newAdviceListTestHelper(t, s)

		// Create test advice with various scopes
		globalAdvice := h.createAdvice(
			"Always check hook first",
			"When starting a session, always run gt hook",
			"", "", "", // no targeting = global
			types.StatusOpen,
		)

		rigAdvice := h.createAdvice(
			"Use go test for testing",
			"In beads repo, run go test ./...",
			"beads", "", "", // rig-level
			types.StatusOpen,
		)

		roleAdvice := h.createAdvice(
			"Complete work before gt done",
			"Polecats must finish work before calling gt done",
			"beads", "polecat", "", // role-level
			types.StatusOpen,
		)

		agentAdvice := h.createAdvice(
			"Focus on CLI tasks",
			"quartz specializes in CLI implementation",
			"", "", "beads/polecats/quartz", // agent-level
			types.StatusOpen,
		)

		closedAdvice := h.createAdvice(
			"Deprecated advice",
			"This advice is no longer relevant",
			"", "", "", // global, but closed
			types.StatusClosed,
		)

		t.Run("list all open advice", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})
			// Should have all 4 open advice items (not the closed one)
			h.assertCount(len(results), 4, "open advice")
		})

		t.Run("filter by rig", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Filter in-memory for rig-level advice (matches advice_list.go logic)
			var rigFiltered []*types.Issue
			for _, issue := range results {
				if issue.AdviceTargetRig == "beads" && issue.AdviceTargetRole == "" && issue.AdviceTargetAgent == "" {
					rigFiltered = append(rigFiltered, issue)
				}
			}
			h.assertCount(len(rigFiltered), 1, "rig-level advice for beads")
			if len(rigFiltered) > 0 && rigFiltered[0].ID != rigAdvice.ID {
				t.Errorf("Expected rig advice ID %s, got %s", rigAdvice.ID, rigFiltered[0].ID)
			}
		})

		t.Run("filter by role", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Filter in-memory for role-level advice
			var roleFiltered []*types.Issue
			for _, issue := range results {
				if issue.AdviceTargetRig == "beads" && issue.AdviceTargetRole == "polecat" {
					roleFiltered = append(roleFiltered, issue)
				}
			}
			h.assertCount(len(roleFiltered), 1, "role-level advice for beads/polecat")
			if len(roleFiltered) > 0 && roleFiltered[0].ID != roleAdvice.ID {
				t.Errorf("Expected role advice ID %s, got %s", roleAdvice.ID, roleFiltered[0].ID)
			}
		})

		t.Run("filter by agent", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Filter in-memory for agent-level advice
			var agentFiltered []*types.Issue
			for _, issue := range results {
				if issue.AdviceTargetAgent == "beads/polecats/quartz" {
					agentFiltered = append(agentFiltered, issue)
				}
			}
			h.assertCount(len(agentFiltered), 1, "agent-level advice")
			if len(agentFiltered) > 0 && agentFiltered[0].ID != agentAdvice.ID {
				t.Errorf("Expected agent advice ID %s, got %s", agentAdvice.ID, agentFiltered[0].ID)
			}
		})

		t.Run("for flag - inheritance chain", func(t *testing.T) {
			// Test the matchesAgentScope function logic
			// Agent "beads/polecats/quartz" should match:
			// - Global advice (no targeting)
			// - Rig "beads" advice
			// - Role "polecat" advice (with rig "beads")
			// - Agent "beads/polecats/quartz" advice

			agentID := "beads/polecats/quartz"
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			var applicable []*types.Issue
			for _, issue := range results {
				if matchesAdviceForTest(issue, agentID, "polecat", "beads") {
					applicable = append(applicable, issue)
				}
			}

			// Should match all 4 open advice items
			h.assertCount(len(applicable), 4, "applicable advice for beads/polecats/quartz")
		})

		t.Run("for flag - different agent", func(t *testing.T) {
			// Agent "gastown/crew/wolf" should match:
			// - Global advice (no targeting)
			// - NOT rig "beads" advice
			// - NOT role "polecat" advice
			// - NOT agent "beads/polecats/quartz" advice

			agentID := "gastown/crew/wolf"
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			var applicable []*types.Issue
			for _, issue := range results {
				if matchesAdviceForTest(issue, agentID, "crew", "gastown") {
					applicable = append(applicable, issue)
				}
			}

			// Should only match global advice
			h.assertCount(len(applicable), 1, "applicable advice for gastown/crew/wolf")
			if len(applicable) > 0 && applicable[0].ID != globalAdvice.ID {
				t.Errorf("Expected global advice ID %s, got %s", globalAdvice.ID, applicable[0].ID)
			}
		})

		t.Run("include closed with --all", func(t *testing.T) {
			// No status filter = include all
			results := h.searchAdvice(types.IssueFilter{})
			h.assertCount(len(results), 5, "all advice including closed")

			// Verify closed advice is included
			var foundClosed bool
			for _, issue := range results {
				if issue.ID == closedAdvice.ID {
					foundClosed = true
					break
				}
			}
			if !foundClosed {
				t.Error("Expected to find closed advice in results")
			}
		})

		t.Run("global advice has no targeting fields", func(t *testing.T) {
			if globalAdvice.AdviceTargetRig != "" {
				t.Errorf("Global advice should have empty rig, got %q", globalAdvice.AdviceTargetRig)
			}
			if globalAdvice.AdviceTargetRole != "" {
				t.Errorf("Global advice should have empty role, got %q", globalAdvice.AdviceTargetRole)
			}
			if globalAdvice.AdviceTargetAgent != "" {
				t.Errorf("Global advice should have empty agent, got %q", globalAdvice.AdviceTargetAgent)
			}
		})

		t.Run("advice type is set correctly", func(t *testing.T) {
			for _, advice := range h.advice {
				if advice.IssueType != types.TypeAdvice {
					t.Errorf("Expected advice type, got %s for %s", advice.IssueType, advice.ID)
				}
			}
		})
	})
}

// matchesAdviceForTest replicates the matching logic from advice_list.go for testing
func matchesAdviceForTest(issue *types.Issue, agentID, roleType, rigName string) bool {
	// Global advice applies to everyone
	if issue.AdviceTargetRig == "" && issue.AdviceTargetRole == "" && issue.AdviceTargetAgent == "" {
		return true
	}

	// Agent-specific advice
	if issue.AdviceTargetAgent != "" {
		return issue.AdviceTargetAgent == agentID
	}

	// Role-level targeting
	if issue.AdviceTargetRole != "" {
		return issue.AdviceTargetRole == roleType && issue.AdviceTargetRig == rigName
	}

	// Rig-level targeting
	if issue.AdviceTargetRig != "" {
		return issue.AdviceTargetRig == rigName
	}

	return false
}

func TestAdviceListScopeGrouping(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// Create advice at different scopes
	adviceItems := []*types.Issue{
		{
			Title:     "Global 1",
			IssueType: types.TypeAdvice,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		},
		{
			Title:           "Rig level",
			IssueType:       types.TypeAdvice,
			Status:          types.StatusOpen,
			AdviceTargetRig: "testrig",
			CreatedAt:       time.Now(),
		},
		{
			Title:            "Role level",
			IssueType:        types.TypeAdvice,
			Status:           types.StatusOpen,
			AdviceTargetRig:  "testrig",
			AdviceTargetRole: "polecat",
			CreatedAt:        time.Now(),
		},
		{
			Title:             "Agent level",
			IssueType:         types.TypeAdvice,
			Status:            types.StatusOpen,
			AdviceTargetAgent: "testrig/polecats/alpha",
			CreatedAt:         time.Now(),
		},
	}

	for _, advice := range adviceItems {
		if err := s.CreateIssue(ctx, advice, "test"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}
	}

	// Verify we can categorize by scope
	adviceType := types.TypeAdvice
	status := types.StatusOpen
	results, err := s.SearchIssues(ctx, "", types.IssueFilter{
		IssueType: &adviceType,
		Status:    &status,
	})
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	var global, rig, role, agent int
	for _, issue := range results {
		if issue.AdviceTargetAgent != "" {
			agent++
		} else if issue.AdviceTargetRole != "" {
			role++
		} else if issue.AdviceTargetRig != "" {
			rig++
		} else {
			global++
		}
	}

	if global != 1 {
		t.Errorf("Expected 1 global advice, got %d", global)
	}
	if rig != 1 {
		t.Errorf("Expected 1 rig advice, got %d", rig)
	}
	if role != 1 {
		t.Errorf("Expected 1 role advice, got %d", role)
	}
	if agent != 1 {
		t.Errorf("Expected 1 agent advice, got %d", agent)
	}
}

func TestSingularizeRole(t *testing.T) {
	tests := []struct {
		plural   string
		expected string
	}{
		{"polecats", "polecat"},
		{"crews", "crew"},
		{"dogs", "dog"},
		{"witness", "witnes"}, // edge case - already singular but ends in 's'
		{"polecat", "polecat"}, // no trailing 's'
		{"", ""},
	}

	for _, tt := range tests {
		result := singularize(tt.plural)
		if result != tt.expected {
			t.Errorf("singularize(%q) = %q, want %q", tt.plural, result, tt.expected)
		}
	}
}

func TestMatchesAgentScope(t *testing.T) {
	tests := []struct {
		name      string
		issue     *types.Issue
		agentID   string
		wantMatch bool
	}{
		{
			name: "global matches any agent",
			issue: &types.Issue{
				Title:     "Global advice",
				IssueType: types.TypeAdvice,
			},
			agentID:   "beads/polecats/alpha",
			wantMatch: true,
		},
		{
			name: "rig matches agent in same rig",
			issue: &types.Issue{
				Title:           "Rig advice",
				IssueType:       types.TypeAdvice,
				AdviceTargetRig: "beads",
			},
			agentID:   "beads/polecats/alpha",
			wantMatch: true,
		},
		{
			name: "rig does not match agent in different rig",
			issue: &types.Issue{
				Title:           "Rig advice",
				IssueType:       types.TypeAdvice,
				AdviceTargetRig: "gastown",
			},
			agentID:   "beads/polecats/alpha",
			wantMatch: false,
		},
		{
			name: "role matches agent with same role",
			issue: &types.Issue{
				Title:            "Role advice",
				IssueType:        types.TypeAdvice,
				AdviceTargetRig:  "beads",
				AdviceTargetRole: "polecat",
			},
			agentID:   "beads/polecats/alpha",
			wantMatch: true,
		},
		{
			name: "role does not match agent with different role",
			issue: &types.Issue{
				Title:            "Role advice",
				IssueType:        types.TypeAdvice,
				AdviceTargetRig:  "beads",
				AdviceTargetRole: "crew",
			},
			agentID:   "beads/polecats/alpha",
			wantMatch: false,
		},
		{
			name: "agent matches exact agent ID",
			issue: &types.Issue{
				Title:             "Agent advice",
				IssueType:         types.TypeAdvice,
				AdviceTargetAgent: "beads/polecats/alpha",
			},
			agentID:   "beads/polecats/alpha",
			wantMatch: true,
		},
		{
			name: "agent does not match different agent",
			issue: &types.Issue{
				Title:             "Agent advice",
				IssueType:         types.TypeAdvice,
				AdviceTargetAgent: "beads/polecats/beta",
			},
			agentID:   "beads/polecats/alpha",
			wantMatch: false,
		},
		{
			name: "crew role matches crew agent",
			issue: &types.Issue{
				Title:            "Crew advice",
				IssueType:        types.TypeAdvice,
				AdviceTargetRig:  "beads",
				AdviceTargetRole: "crew",
			},
			agentID:   "beads/crew/advice_architect",
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAgentScope(tt.issue, tt.agentID)
			if got != tt.wantMatch {
				t.Errorf("matchesAgentScope() = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}
