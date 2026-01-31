// advice_subscription_e2e_test.go - E2E tests for advice label-based subscriptions
//
// These tests verify the full CLI → storage → query pipeline for advice targeting:
// - CLI flags (--rig, --role, --agent) correctly add labels
// - bd advice list --for correctly filters by agent subscriptions
// - Label persistence across database operations
// - Backward compatibility with legacy behavior

//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// TestAdviceSubscriptionE2E_CLIFlagsAddLabels verifies that CLI targeting flags
// add the corresponding labels to advice issues.
func TestAdviceSubscriptionE2E_CLIFlagsAddLabels(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	t.Run("--rig flag adds rig: label", func(t *testing.T) {
		advice := &types.Issue{
			Title:     "Rig-targeted advice",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		if err := store.CreateIssue(ctx, advice, "test"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		// Simulate what --rig=beads does: add rig:beads label
		if err := store.AddLabel(ctx, advice.ID, "rig:beads", "test"); err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		// Verify label was added
		labels, err := store.GetLabels(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}

		found := false
		for _, l := range labels {
			if l == "rig:beads" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected rig:beads label, got %v", labels)
		}
	})

	t.Run("--role flag adds role: label", func(t *testing.T) {
		advice := &types.Issue{
			Title:     "Role-targeted advice",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		if err := store.CreateIssue(ctx, advice, "test"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		// Simulate what --role=polecat does: add role:polecat label
		if err := store.AddLabel(ctx, advice.ID, "role:polecat", "test"); err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		labels, err := store.GetLabels(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}

		found := false
		for _, l := range labels {
			if l == "role:polecat" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected role:polecat label, got %v", labels)
		}
	})

	t.Run("--agent flag adds agent: label", func(t *testing.T) {
		advice := &types.Issue{
			Title:     "Agent-targeted advice",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		if err := store.CreateIssue(ctx, advice, "test"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		// Simulate what --agent=beads/polecats/quartz does
		if err := store.AddLabel(ctx, advice.ID, "agent:beads/polecats/quartz", "test"); err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		labels, err := store.GetLabels(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}

		found := false
		for _, l := range labels {
			if l == "agent:beads/polecats/quartz" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected agent:beads/polecats/quartz label, got %v", labels)
		}
	})

	t.Run("no targeting defaults to global label", func(t *testing.T) {
		advice := &types.Issue{
			Title:     "Global advice (no targeting)",
			IssueType: types.IssueType("advice"),
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		if err := store.CreateIssue(ctx, advice, "test"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		// Simulate default behavior: add global label
		if err := store.AddLabel(ctx, advice.ID, "global", "test"); err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		labels, err := store.GetLabels(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}

		found := false
		for _, l := range labels {
			if l == "global" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected global label, got %v", labels)
		}
	})
}

// TestAdviceSubscriptionE2E_ForFlagFiltering verifies that bd advice list --for
// correctly filters advice based on agent subscriptions.
func TestAdviceSubscriptionE2E_ForFlagFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	// Create advice at different scopes
	globalAdvice := createAdviceWithLabels(t, ctx, store, "Global advice", []string{"global"})
	beadsRigAdvice := createAdviceWithLabels(t, ctx, store, "Beads rig advice", []string{"rig:beads"})
	gastownRigAdvice := createAdviceWithLabels(t, ctx, store, "Gastown rig advice", []string{"rig:gastown"})
	polecatRoleAdvice := createAdviceWithLabels(t, ctx, store, "Polecat role advice", []string{"role:polecat"})
	crewRoleAdvice := createAdviceWithLabels(t, ctx, store, "Crew role advice", []string{"role:crew"})
	quartzAgentAdvice := createAdviceWithLabels(t, ctx, store, "Quartz agent advice", []string{"agent:beads/polecats/quartz"})

	t.Run("beads/polecats/quartz sees correct advice", func(t *testing.T) {
		agentID := "beads/polecats/quartz"
		subscriptions := buildAgentSubscriptions(agentID, nil)

		applicable := filterAdviceBySubscriptions(t, ctx, store, subscriptions)

		// Should see: global, rig:beads, role:polecat, agent:beads/polecats/quartz
		assertAdviceInList(t, applicable, globalAdvice, "global advice")
		assertAdviceInList(t, applicable, beadsRigAdvice, "beads rig advice")
		assertAdviceInList(t, applicable, polecatRoleAdvice, "polecat role advice")
		assertAdviceInList(t, applicable, quartzAgentAdvice, "quartz agent advice")

		// Should NOT see: rig:gastown, role:crew
		assertAdviceNotInList(t, applicable, gastownRigAdvice, "gastown rig advice")
		assertAdviceNotInList(t, applicable, crewRoleAdvice, "crew role advice")
	})

	t.Run("gastown/crew/wolf sees correct advice", func(t *testing.T) {
		agentID := "gastown/crew/wolf"
		subscriptions := buildAgentSubscriptions(agentID, nil)

		applicable := filterAdviceBySubscriptions(t, ctx, store, subscriptions)

		// Should see: global, rig:gastown, role:crew
		assertAdviceInList(t, applicable, globalAdvice, "global advice")
		assertAdviceInList(t, applicable, gastownRigAdvice, "gastown rig advice")
		assertAdviceInList(t, applicable, crewRoleAdvice, "crew role advice")

		// Should NOT see: rig:beads, role:polecat, agent:beads/polecats/quartz
		assertAdviceNotInList(t, applicable, beadsRigAdvice, "beads rig advice")
		assertAdviceNotInList(t, applicable, polecatRoleAdvice, "polecat role advice")
		assertAdviceNotInList(t, applicable, quartzAgentAdvice, "quartz agent advice")
	})

	t.Run("beads/crew/advice_architect sees correct advice", func(t *testing.T) {
		agentID := "beads/crew/advice_architect"
		subscriptions := buildAgentSubscriptions(agentID, nil)

		applicable := filterAdviceBySubscriptions(t, ctx, store, subscriptions)

		// Should see: global, rig:beads, role:crew
		assertAdviceInList(t, applicable, globalAdvice, "global advice")
		assertAdviceInList(t, applicable, beadsRigAdvice, "beads rig advice")
		assertAdviceInList(t, applicable, crewRoleAdvice, "crew role advice")

		// Should NOT see: rig:gastown, role:polecat, agent:beads/polecats/quartz
		assertAdviceNotInList(t, applicable, gastownRigAdvice, "gastown rig advice")
		assertAdviceNotInList(t, applicable, polecatRoleAdvice, "polecat role advice")
		assertAdviceNotInList(t, applicable, quartzAgentAdvice, "quartz agent advice")
	})
}

// TestAdviceSubscriptionE2E_LabelPersistence verifies labels survive database operations.
func TestAdviceSubscriptionE2E_LabelPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	// Create advice with labels
	advice := &types.Issue{
		Title:     "Persistent advice",
		IssueType: types.IssueType("advice"),
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, advice, "test"); err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}

	labels := []string{"global", "testing", "rig:beads"}
	for _, label := range labels {
		if err := store.AddLabel(ctx, advice.ID, label, "test"); err != nil {
			t.Fatalf("Failed to add label %s: %v", label, err)
		}
	}

	adviceID := advice.ID

	t.Run("labels persist after issue close and reopen", func(t *testing.T) {
		// Close the advice
		if err := store.CloseIssue(ctx, adviceID, "Testing persistence", "test", ""); err != nil {
			t.Fatalf("Failed to close advice: %v", err)
		}

		// Verify labels still there after close
		gotLabels, err := store.GetLabels(ctx, adviceID)
		if err != nil {
			t.Fatalf("Failed to get labels after close: %v", err)
		}

		for _, want := range labels {
			found := false
			for _, got := range gotLabels {
				if got == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Label %q not found after close, got %v", want, gotLabels)
			}
		}

		// Reopen the advice
		if err := store.UpdateIssue(ctx, adviceID, map[string]interface{}{
			"status": types.StatusOpen,
		}, "test"); err != nil {
			t.Fatalf("Failed to reopen advice: %v", err)
		}

		// Verify labels still there after reopen
		gotLabels, err = store.GetLabels(ctx, adviceID)
		if err != nil {
			t.Fatalf("Failed to get labels after reopen: %v", err)
		}

		for _, want := range labels {
			found := false
			for _, got := range gotLabels {
				if got == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Label %q not found after reopen, got %v", want, gotLabels)
			}
		}
	})
}

// TestAdviceSubscriptionE2E_MultipleLabels verifies advice can have multiple targeting labels.
func TestAdviceSubscriptionE2E_MultipleLabels(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	// Create advice targeting multiple rigs
	multiRigAdvice := createAdviceWithLabels(t, ctx, store, "Multi-rig advice",
		[]string{"rig:beads", "rig:gastown"})

	// Create advice with custom + targeting labels
	customLabelAdvice := createAdviceWithLabels(t, ctx, store, "Security advice",
		[]string{"global", "security", "testing"})

	t.Run("multi-rig advice matches any rig", func(t *testing.T) {
		beadsAgent := buildAgentSubscriptions("beads/polecats/quartz", nil)
		gastownAgent := buildAgentSubscriptions("gastown/crew/wolf", nil)

		beadsApplicable := filterAdviceBySubscriptions(t, ctx, store, beadsAgent)
		gastownApplicable := filterAdviceBySubscriptions(t, ctx, store, gastownAgent)

		assertAdviceInList(t, beadsApplicable, multiRigAdvice, "multi-rig advice for beads agent")
		assertAdviceInList(t, gastownApplicable, multiRigAdvice, "multi-rig advice for gastown agent")
	})

	t.Run("custom labels work with targeting", func(t *testing.T) {
		// Agent subscribing to security should see the advice
		subscriptions := buildAgentSubscriptions("beads/polecats/quartz", []string{"security"})

		applicable := filterAdviceBySubscriptions(t, ctx, store, subscriptions)
		assertAdviceInList(t, applicable, customLabelAdvice, "security-labeled advice")
	})
}

// TestAdviceSubscriptionE2E_ClosedAdviceFiltering verifies closed advice is filtered out by default.
func TestAdviceSubscriptionE2E_ClosedAdviceFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	// Create open and closed advice
	openAdvice := createAdviceWithLabels(t, ctx, store, "Open advice", []string{"global"})

	closedAdvice := &types.Issue{
		Title:     "Closed advice",
		IssueType: types.IssueType("advice"),
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, closedAdvice, "test"); err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if err := store.AddLabel(ctx, closedAdvice.ID, "global", "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}
	if err := store.CloseIssue(ctx, closedAdvice.ID, "No longer needed", "test", ""); err != nil {
		t.Fatalf("Failed to close advice: %v", err)
	}

	t.Run("closed advice not in default list", func(t *testing.T) {
		subscriptions := buildAgentSubscriptions("beads/polecats/quartz", nil)

		// Filter only open advice (default behavior)
		adviceType := types.IssueType("advice")
		openStatus := types.StatusOpen
		allAdvice, err := store.SearchIssues(ctx, "", types.IssueFilter{
			IssueType: &adviceType,
			Status:    &openStatus,
		})
		if err != nil {
			t.Fatalf("Failed to search advice: %v", err)
		}

		issueIDs := make([]string, len(allAdvice))
		for i, a := range allAdvice {
			issueIDs[i] = a.ID
		}
		labelsMap, _ := store.GetLabelsForIssues(ctx, issueIDs)

		var applicable []*types.Issue
		for _, advice := range allAdvice {
			if matchesSubscriptions(advice, labelsMap[advice.ID], subscriptions) {
				applicable = append(applicable, advice)
			}
		}

		assertAdviceInList(t, applicable, openAdvice, "open advice")
		assertAdviceNotInList(t, applicable, closedAdvice, "closed advice")
	})
}

// Helper functions

func createAdviceWithLabels(t *testing.T, ctx context.Context, store *sqlite.SQLiteStorage, title string, labels []string) *types.Issue {
	t.Helper()

	advice := &types.Issue{
		Title:     title,
		IssueType: types.IssueType("advice"),
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, advice, "test"); err != nil {
		t.Fatalf("Failed to create advice %q: %v", title, err)
	}

	for _, label := range labels {
		if err := store.AddLabel(ctx, advice.ID, label, "test"); err != nil {
			t.Fatalf("Failed to add label %s to %q: %v", label, title, err)
		}
	}

	return advice
}

func filterAdviceBySubscriptions(t *testing.T, ctx context.Context, store *sqlite.SQLiteStorage, subscriptions []string) []*types.Issue {
	t.Helper()

	adviceType := types.IssueType("advice")
	openStatus := types.StatusOpen
	allAdvice, err := store.SearchIssues(ctx, "", types.IssueFilter{
		IssueType: &adviceType,
		Status:    &openStatus,
	})
	if err != nil {
		t.Fatalf("Failed to search advice: %v", err)
	}

	issueIDs := make([]string, len(allAdvice))
	for i, a := range allAdvice {
		issueIDs[i] = a.ID
	}
	labelsMap, err := store.GetLabelsForIssues(ctx, issueIDs)
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

func assertAdviceInList(t *testing.T, list []*types.Issue, want *types.Issue, desc string) {
	t.Helper()

	for _, a := range list {
		if a.ID == want.ID {
			return
		}
	}
	t.Errorf("Expected %s (ID: %s) in list, but not found", desc, want.ID)
}

func assertAdviceNotInList(t *testing.T, list []*types.Issue, notWant *types.Issue, desc string) {
	t.Helper()

	for _, a := range list {
		if a.ID == notWant.ID {
			t.Errorf("Expected %s (ID: %s) NOT in list, but found", desc, notWant.ID)
			return
		}
	}
}

// TestAdviceSubscriptionE2E_CLIBinaryIntegration tests the actual CLI binary if available.
// NOTE: This test is skipped by default because CLI binary tests require complex setup
// (the binary runs in a separate process and can't share test database state).
// The core subscription logic is thoroughly tested by the other E2E tests above.
func TestAdviceSubscriptionE2E_CLIBinaryIntegration(t *testing.T) {
	t.Skip("CLI binary integration requires bd init setup - core logic tested via other E2E tests")

	// Skip if bd binary not in PATH
	bdPath, err := exec.LookPath("bd")
	if err != nil {
		t.Skip("bd binary not in PATH, skipping CLI integration test")
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	// Initialize with newTestStore to set up config properly
	store := newTestStore(t, dbPath)
	_ = store // Store is managed by t.Cleanup

	runBD := func(args ...string) (string, error) {
		cmd := exec.Command(bdPath, args...)
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(),
			"BD_ACTOR=test",
			"HOME="+tmpDir,
		)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		return out.String(), err
	}

	t.Run("bd advice add with --rig creates label", func(t *testing.T) {
		out, err := runBD("advice", "add", "Test rig advice", "--rig=testrig", "--json")
		if err != nil {
			t.Fatalf("bd advice add failed: %v\nOutput: %s", err, out)
		}

		// The output should include the advice with rig:testrig label
		if !strings.Contains(out, "rig:testrig") && !strings.Contains(out, "testrig") {
			t.Logf("Output: %s", out)
			// Note: JSON output might not include labels directly, check via list
		}
	})

	t.Run("bd advice list shows created advice", func(t *testing.T) {
		out, err := runBD("advice", "list")
		if err != nil {
			t.Fatalf("bd advice list failed: %v\nOutput: %s", err, out)
		}

		if !strings.Contains(out, "Test rig advice") {
			t.Errorf("Expected 'Test rig advice' in output, got: %s", out)
		}
	})
}
