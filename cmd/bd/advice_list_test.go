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

// createAdvice creates advice with labels for targeting
func (h *adviceListTestHelper) createAdvice(title, description string, labels []string, status types.Status) *types.Issue {
	advice := &types.Issue{
		Title:       title,
		Description: description,
		Priority:    2,
		IssueType:   types.IssueType("advice"),
		Status:      status,
		CreatedAt:   time.Now(),
	}
	if err := h.store.CreateIssue(h.ctx, advice, "test-user"); err != nil {
		h.t.Fatalf("Failed to create advice: %v", err)
	}
	// Add labels for targeting
	for _, label := range labels {
		if err := h.store.AddLabel(h.ctx, advice.ID, label, "test-user"); err != nil {
			h.t.Fatalf("Failed to add label %s: %v", label, err)
		}
	}
	h.advice = append(h.advice, advice)
	return advice
}

func (h *adviceListTestHelper) searchAdvice(filter types.IssueFilter) []*types.Issue {
	adviceType := types.IssueType("advice")
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

		// Create test advice with various scopes using labels
		globalAdvice := h.createAdvice(
			"Always check hook first",
			"When starting a session, always run gt hook",
			[]string{"global"}, // global label
			types.StatusOpen,
		)

		rigAdvice := h.createAdvice(
			"Use go test for testing",
			"In beads repo, run go test ./...",
			[]string{"rig:beads"}, // rig-level
			types.StatusOpen,
		)

		roleAdvice := h.createAdvice(
			"Complete work before gt done",
			"Polecats must finish work before calling gt done",
			[]string{"rig:beads", "role:polecat"}, // role-level
			types.StatusOpen,
		)

		agentAdvice := h.createAdvice(
			"Focus on CLI tasks",
			"quartz specializes in CLI implementation",
			[]string{"agent:beads/polecats/quartz"}, // agent-level
			types.StatusOpen,
		)

		closedAdvice := h.createAdvice(
			"Deprecated advice",
			"This advice is no longer relevant",
			[]string{"global"}, // global, but closed
			types.StatusClosed,
		)

		t.Run("list all open advice", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})
			// Should have all 4 open advice items (not the closed one)
			h.assertCount(len(results), 4, "open advice")
		})

		t.Run("filter by rig label", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Get labels for all issues
			issueIDs := make([]string, len(results))
			for i, issue := range results {
				issueIDs[i] = issue.ID
			}
			labelsMap, _ := s.GetLabelsForIssues(h.ctx, issueIDs)

			// Filter for rig:beads label (but not role or agent labels)
			var rigFiltered []*types.Issue
			for _, issue := range results {
				labels := labelsMap[issue.ID]
				hasRigBeads := false
				hasRole := false
				hasAgent := false
				for _, l := range labels {
					if l == "rig:beads" {
						hasRigBeads = true
					}
					if len(l) > 5 && l[:5] == "role:" {
						hasRole = true
					}
					if len(l) > 6 && l[:6] == "agent:" {
						hasAgent = true
					}
				}
				// Rig-only advice has rig: but not role: or agent:
				if hasRigBeads && !hasRole && !hasAgent {
					rigFiltered = append(rigFiltered, issue)
				}
			}
			h.assertCount(len(rigFiltered), 1, "rig-level advice for beads")
			if len(rigFiltered) > 0 && rigFiltered[0].ID != rigAdvice.ID {
				t.Errorf("Expected rig advice ID %s, got %s", rigAdvice.ID, rigFiltered[0].ID)
			}
		})

		t.Run("filter by role label", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Get labels for all issues
			issueIDs := make([]string, len(results))
			for i, issue := range results {
				issueIDs[i] = issue.ID
			}
			labelsMap, _ := s.GetLabelsForIssues(h.ctx, issueIDs)

			// Filter for role:polecat label
			var roleFiltered []*types.Issue
			for _, issue := range results {
				for _, l := range labelsMap[issue.ID] {
					if l == "role:polecat" {
						roleFiltered = append(roleFiltered, issue)
						break
					}
				}
			}
			h.assertCount(len(roleFiltered), 1, "role-level advice for polecat")
			if len(roleFiltered) > 0 && roleFiltered[0].ID != roleAdvice.ID {
				t.Errorf("Expected role advice ID %s, got %s", roleAdvice.ID, roleFiltered[0].ID)
			}
		})

		t.Run("filter by agent label", func(t *testing.T) {
			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Get labels for all issues
			issueIDs := make([]string, len(results))
			for i, issue := range results {
				issueIDs[i] = issue.ID
			}
			labelsMap, _ := s.GetLabelsForIssues(h.ctx, issueIDs)

			// Filter for agent-level advice
			var agentFiltered []*types.Issue
			for _, issue := range results {
				for _, l := range labelsMap[issue.ID] {
					if l == "agent:beads/polecats/quartz" {
						agentFiltered = append(agentFiltered, issue)
						break
					}
				}
			}
			h.assertCount(len(agentFiltered), 1, "agent-level advice")
			if len(agentFiltered) > 0 && agentFiltered[0].ID != agentAdvice.ID {
				t.Errorf("Expected agent advice ID %s, got %s", agentAdvice.ID, agentFiltered[0].ID)
			}
		})

		t.Run("for flag - subscription matching", func(t *testing.T) {
			// Test that buildAgentSubscriptions + matchesSubscriptions works
			// Agent "beads/polecats/quartz" should match:
			// - Global advice (global label)
			// - Rig "beads" advice (rig:beads label)
			// - Role "polecat" advice (role:polecat label)
			// - Agent "beads/polecats/quartz" advice (agent: label)

			agentID := "beads/polecats/quartz"
			subscriptions := buildAgentSubscriptions(agentID, nil)

			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Get labels for all issues
			issueIDs := make([]string, len(results))
			for i, issue := range results {
				issueIDs[i] = issue.ID
			}
			labelsMap, _ := s.GetLabelsForIssues(h.ctx, issueIDs)

			var applicable []*types.Issue
			for _, issue := range results {
				if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
					applicable = append(applicable, issue)
				}
			}

			// Should match all 4 open advice items
			h.assertCount(len(applicable), 4, "applicable advice for beads/polecats/quartz")
		})

		t.Run("for flag - different agent", func(t *testing.T) {
			// Agent "gastown/crew/wolf" should match:
			// - Global advice (global label)
			// - NOT rig "beads" advice
			// - NOT role "polecat" advice
			// - NOT agent "beads/polecats/quartz" advice

			agentID := "gastown/crew/wolf"
			subscriptions := buildAgentSubscriptions(agentID, nil)

			status := types.StatusOpen
			results := h.searchAdvice(types.IssueFilter{Status: &status})

			// Get labels for all issues
			issueIDs := make([]string, len(results))
			for i, issue := range results {
				issueIDs[i] = issue.ID
			}
			labelsMap, _ := s.GetLabelsForIssues(h.ctx, issueIDs)

			var applicable []*types.Issue
			for _, issue := range results {
				if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
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

		t.Run("global advice has global label", func(t *testing.T) {
			labels, _ := s.GetLabels(h.ctx, globalAdvice.ID)
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

		t.Run("advice type is set correctly", func(t *testing.T) {
			for _, advice := range h.advice {
				if advice.IssueType != types.IssueType("advice") {
					t.Errorf("Expected advice type, got %s for %s", advice.IssueType, advice.ID)
				}
			}
		})
	})
}

func TestAdviceListScopeGrouping(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// Create advice at different scopes using labels
	adviceItems := []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			issue: &types.Issue{
				Title:     "Global 1",
				IssueType: types.IssueType("advice"),
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			},
			labels: []string{"global"},
		},
		{
			issue: &types.Issue{
				Title:     "Rig level",
				IssueType: types.IssueType("advice"),
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			},
			labels: []string{"rig:testrig"},
		},
		{
			issue: &types.Issue{
				Title:     "Role level",
				IssueType: types.IssueType("advice"),
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			},
			labels: []string{"rig:testrig", "role:polecat"},
		},
		{
			issue: &types.Issue{
				Title:     "Agent level",
				IssueType: types.IssueType("advice"),
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			},
			labels: []string{"agent:testrig/polecats/alpha"},
		},
	}

	for _, item := range adviceItems {
		if err := s.CreateIssue(ctx, item.issue, "test"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}
		for _, label := range item.labels {
			if err := s.AddLabel(ctx, item.issue.ID, label, "test"); err != nil {
				t.Fatalf("Failed to add label: %v", err)
			}
		}
	}

	// Verify we can categorize by scope via labels
	adviceType := types.IssueType("advice")
	status := types.StatusOpen
	results, err := s.SearchIssues(ctx, "", types.IssueFilter{
		IssueType: &adviceType,
		Status:    &status,
	})
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	// Get labels for all issues
	issueIDs := make([]string, len(results))
	for i, issue := range results {
		issueIDs[i] = issue.ID
	}
	labelsMap, _ := s.GetLabelsForIssues(ctx, issueIDs)

	var global, rig, role, agent int
	for _, issue := range results {
		labels := labelsMap[issue.ID]
		hasGlobal := false
		hasRig := false
		hasRole := false
		hasAgent := false
		for _, l := range labels {
			if l == "global" {
				hasGlobal = true
			}
			if len(l) > 4 && l[:4] == "rig:" {
				hasRig = true
			}
			if len(l) > 5 && l[:5] == "role:" {
				hasRole = true
			}
			if len(l) > 6 && l[:6] == "agent:" {
				hasAgent = true
			}
		}
		// Categorize by most specific label
		if hasAgent {
			agent++
		} else if hasRole {
			role++
		} else if hasRig {
			rig++
		} else if hasGlobal {
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

// TestMatchesAnyLabel tests the label matching function for subscription filtering
func TestMatchesAnyLabel(t *testing.T) {
	tests := []struct {
		name         string
		issueLabels  []string
		filterLabels []string
		want         bool
	}{
		{
			name:         "no labels match empty filter",
			issueLabels:  []string{"testing", "ci"},
			filterLabels: []string{},
			want:         false,
		},
		{
			name:         "empty labels match nothing",
			issueLabels:  []string{},
			filterLabels: []string{"testing"},
			want:         false,
		},
		{
			name:         "single match",
			issueLabels:  []string{"testing", "ci"},
			filterLabels: []string{"testing"},
			want:         true,
		},
		{
			name:         "no match",
			issueLabels:  []string{"testing", "ci"},
			filterLabels: []string{"security"},
			want:         false,
		},
		{
			name:         "any match succeeds",
			issueLabels:  []string{"testing", "ci"},
			filterLabels: []string{"security", "ci"},
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAnyLabel(tt.issueLabels, tt.filterLabels)
			if got != tt.want {
				t.Errorf("matchesAnyLabel(%v, %v) = %v, want %v",
					tt.issueLabels, tt.filterLabels, got, tt.want)
			}
		})
	}
}

// TestMatchesSubscriptions tests the subscription simulation logic
func TestMatchesSubscriptions(t *testing.T) {
	tests := []struct {
		name          string
		issue         *types.Issue
		issueLabels   []string
		subscriptions []string
		want          bool
	}{
		{
			name:          "global label matches global subscription",
			issue:         &types.Issue{},
			issueLabels:   []string{"global"},
			subscriptions: []string{"global"},
			want:          true,
		},
		{
			name:          "no labels no match",
			issue:         &types.Issue{},
			issueLabels:   []string{},
			subscriptions: []string{"testing"},
			want:          false,
		},
		{
			name:          "rig label matches rig subscription",
			issue:         &types.Issue{},
			issueLabels:   []string{"rig:beads"},
			subscriptions: []string{"rig:beads"},
			want:          true,
		},
		{
			name:          "rig label no match different rig",
			issue:         &types.Issue{},
			issueLabels:   []string{"rig:beads"},
			subscriptions: []string{"rig:gastown"},
			want:          false,
		},
		{
			name:          "role label matches role subscription",
			issue:         &types.Issue{},
			issueLabels:   []string{"role:polecat"},
			subscriptions: []string{"role:polecat"},
			want:          true,
		},
		{
			name:          "agent label matches agent subscription",
			issue:         &types.Issue{},
			issueLabels:   []string{"agent:beads/polecats/quartz"},
			subscriptions: []string{"agent:beads/polecats/quartz"},
			want:          true,
		},
		{
			name:          "explicit label match",
			issue:         &types.Issue{},
			issueLabels:   []string{"testing", "ci"},
			subscriptions: []string{"testing"},
			want:          true,
		},
		{
			name:          "multiple subscriptions any match",
			issue:         &types.Issue{},
			issueLabels:   []string{"security"},
			subscriptions: []string{"testing", "security", "global"},
			want:          true,
		},
		{
			name:          "multiple labels one matches",
			issue:         &types.Issue{},
			issueLabels:   []string{"global", "testing"},
			subscriptions: []string{"testing"},
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesSubscriptions(tt.issue, tt.issueLabels, tt.subscriptions)
			if got != tt.want {
				t.Errorf("matchesSubscriptions() = %v, want %v", got, tt.want)
			}
		})
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

// TestBuildAgentSubscriptions tests the auto-subscription generation for agents
func TestBuildAgentSubscriptions(t *testing.T) {
	tests := []struct {
		name     string
		agentID  string
		existing []string
		want     []string
	}{
		{
			name:     "full agent path generates all subscriptions",
			agentID:  "beads/polecats/quartz",
			existing: nil,
			want:     []string{"global", "agent:beads/polecats/quartz", "rig:beads", "role:polecats", "role:polecat"},
		},
		{
			name:     "crew agent gets crew subscriptions",
			agentID:  "beads/crew/wolf",
			existing: nil,
			want:     []string{"global", "agent:beads/crew/wolf", "rig:beads", "role:crew"},
		},
		{
			name:     "existing subscriptions preserved",
			agentID:  "beads/polecats/quartz",
			existing: []string{"testing"},
			want:     []string{"testing", "global", "agent:beads/polecats/quartz", "rig:beads", "role:polecats", "role:polecat"},
		},
		{
			name:     "single-part agent ID",
			agentID:  "standalone",
			existing: nil,
			want:     []string{"global", "agent:standalone", "rig:standalone"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAgentSubscriptions(tt.agentID, tt.existing)

			// Check all expected subscriptions are present
			gotSet := make(map[string]bool)
			for _, s := range got {
				gotSet[s] = true
			}

			for _, want := range tt.want {
				if !gotSet[want] {
					t.Errorf("buildAgentSubscriptions(%q) missing subscription %q, got %v", tt.agentID, want, got)
				}
			}
		})
	}
}
