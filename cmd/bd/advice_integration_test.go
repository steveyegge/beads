//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// ============================================================================
// Advice System Integration Tests (bd-wfyf)
// ============================================================================
//
// These tests verify that bd advice commands (show, edit, preview, list) work
// correctly over RPC when BD_DAEMON_HOST is set. Each test starts an RPC
// server with a test store, connects a client, and exercises the same code
// paths the CLI uses when daemonClient is set.
//
// Run with: go test -tags=integration -run TestAdviceIntegration ./cmd/bd/
// ============================================================================

// setupAdviceTestServer creates a test RPC server with store, returns the
// client and a cleanup function. This mimics having BD_DAEMON_HOST set.
func setupAdviceTestServer(t *testing.T) (*rpc.Client, func()) {
	t.Helper()

	tmpDir := makeSocketTempDir(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log := createTestLogger(t)

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, "", "", log)
	if err != nil {
		testStore.Close()
		cancel()
		t.Fatalf("Failed to start RPC server: %v", err)
	}

	select {
	case <-server.WaitReady():
	case <-time.After(5 * time.Second):
		_ = server.Stop()
		testStore.Close()
		cancel()
		t.Fatal("Server did not become ready within 5 seconds")
	}

	client, err := rpc.TryConnect(socketPath)
	if err != nil {
		_ = server.Stop()
		testStore.Close()
		cancel()
		t.Fatalf("Failed to connect to RPC server: %v", err)
	}

	cleanup := func() {
		client.Close()
		_ = server.Stop()
		testStore.Close()
		cancel()
		os.RemoveAll(tmpDir)
	}

	return client, cleanup
}

// createTestAdvice is a helper that creates an advice bead via RPC and returns its ID.
func createTestAdvice(t *testing.T, client *rpc.Client, title, description string, labels []string) string {
	t.Helper()

	createArgs := &rpc.CreateArgs{
		Title:       title,
		Description: description,
		IssueType:   "advice",
		Priority:    2,
		Labels:      labels,
	}

	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create advice %q: %v", title, err)
	}
	if !resp.Success {
		t.Fatalf("Create advice %q failed: %s", title, resp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal created advice: %v", err)
	}

	return issue.ID
}

// ============================================================================
// Test: bd advice show
// ============================================================================

func TestAdviceIntegration_Show(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	t.Run("show_basic_advice", func(t *testing.T) {
		// Create advice
		createArgs := &rpc.CreateArgs{
			Title:       "Always run tests before commit",
			Description: "Unit and integration tests must pass before any commit is pushed.",
			IssueType:   "advice",
			Priority:    1,
			Labels:      []string{"global"},
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}
		if !resp.Success {
			t.Fatalf("Create failed: %s", resp.Error)
		}

		var created types.Issue
		if err := json.Unmarshal(resp.Data, &created); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		// Show via RPC (same path as advice_show.go with daemonClient)
		showResp, err := client.Show(&rpc.ShowArgs{ID: created.ID})
		if err != nil {
			t.Fatalf("Failed to show advice: %v", err)
		}
		if !showResp.Success {
			t.Fatalf("Show failed: %s", showResp.Error)
		}

		var shown types.Issue
		if err := json.Unmarshal(showResp.Data, &shown); err != nil {
			t.Fatalf("Failed to unmarshal shown issue: %v", err)
		}

		// Verify all fields round-tripped
		if shown.Title != "Always run tests before commit" {
			t.Errorf("Title: expected %q, got %q", "Always run tests before commit", shown.Title)
		}
		if shown.Description != "Unit and integration tests must pass before any commit is pushed." {
			t.Errorf("Description mismatch: got %q", shown.Description)
		}
		if shown.IssueType != types.TypeAdvice {
			t.Errorf("IssueType: expected 'advice', got %q", shown.IssueType)
		}
		if shown.Priority != 1 {
			t.Errorf("Priority: expected 1, got %d", shown.Priority)
		}
		if shown.Status != types.StatusOpen {
			t.Errorf("Status: expected 'open', got %q", shown.Status)
		}
	})

	t.Run("show_advice_with_labels", func(t *testing.T) {
		id := createTestAdvice(t, client, "Beads polecat advice", "For polecats in beads", []string{"rig:beads", "role:polecat"})

		showResp, err := client.Show(&rpc.ShowArgs{ID: id})
		if err != nil {
			t.Fatalf("Failed to show: %v", err)
		}
		if !showResp.Success {
			t.Fatalf("Show failed: %s", showResp.Error)
		}

		var shown types.Issue
		if err := json.Unmarshal(showResp.Data, &shown); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		// Verify labels are present in the response
		hasRig := false
		hasRole := false
		for _, l := range shown.Labels {
			if l == "rig:beads" {
				hasRig = true
			}
			if l == "role:polecat" {
				hasRole = true
			}
		}
		if !hasRig {
			t.Errorf("Expected label 'rig:beads' in %v", shown.Labels)
		}
		if !hasRole {
			t.Errorf("Expected label 'role:polecat' in %v", shown.Labels)
		}
	})

	t.Run("show_advice_with_hook_fields", func(t *testing.T) {
		createArgs := &rpc.CreateArgs{
			Title:               "Run linter before push",
			Description:         "Ensures code style consistency",
			IssueType:           "advice",
			Priority:            2,
			Labels:              []string{"role:polecat"},
			AdviceHookCommand:   "make lint",
			AdviceHookTrigger:   "before-push",
			AdviceHookTimeout:   60,
			AdviceHookOnFailure: "block",
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Failed to create: %v", err)
		}
		if !resp.Success {
			t.Fatalf("Create failed: %s", resp.Error)
		}

		var created types.Issue
		if err := json.Unmarshal(resp.Data, &created); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		// Show and verify hook fields round-trip
		showResp, err := client.Show(&rpc.ShowArgs{ID: created.ID})
		if err != nil {
			t.Fatalf("Failed to show: %v", err)
		}

		var shown types.Issue
		if err := json.Unmarshal(showResp.Data, &shown); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if shown.AdviceHookCommand != "make lint" {
			t.Errorf("HookCommand: expected 'make lint', got %q", shown.AdviceHookCommand)
		}
		if shown.AdviceHookTrigger != "before-push" {
			t.Errorf("HookTrigger: expected 'before-push', got %q", shown.AdviceHookTrigger)
		}
		if shown.AdviceHookTimeout != 60 {
			t.Errorf("HookTimeout: expected 60, got %d", shown.AdviceHookTimeout)
		}
		if shown.AdviceHookOnFailure != "block" {
			t.Errorf("HookOnFailure: expected 'block', got %q", shown.AdviceHookOnFailure)
		}
	})

	t.Run("show_nonexistent_advice_fails", func(t *testing.T) {
		showResp, err := client.Show(&rpc.ShowArgs{ID: "bd-nonexistent999"})
		if err == nil && showResp.Success {
			t.Error("Expected failure showing nonexistent advice")
		}
	})
}

// ============================================================================
// Test: bd advice edit
// ============================================================================

func TestAdviceIntegration_Edit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	t.Run("edit_title_and_description", func(t *testing.T) {
		id := createTestAdvice(t, client, "Original title", "Original description", []string{"global"})

		newTitle := "Updated title"
		newDesc := "Updated description with more detail"
		updateArgs := &rpc.UpdateArgs{
			ID:          id,
			Title:       &newTitle,
			Description: &newDesc,
		}

		resp, err := client.Update(updateArgs)
		if err != nil {
			t.Fatalf("Failed to update: %v", err)
		}
		if !resp.Success {
			t.Fatalf("Update failed: %s", resp.Error)
		}

		// Verify via Show
		showResp, err := client.Show(&rpc.ShowArgs{ID: id})
		if err != nil {
			t.Fatalf("Failed to show: %v", err)
		}

		var shown types.Issue
		if err := json.Unmarshal(showResp.Data, &shown); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if shown.Title != "Updated title" {
			t.Errorf("Title: expected 'Updated title', got %q", shown.Title)
		}
		if shown.Description != "Updated description with more detail" {
			t.Errorf("Description mismatch: got %q", shown.Description)
		}
	})

	t.Run("edit_priority", func(t *testing.T) {
		id := createTestAdvice(t, client, "Priority test advice", "Test", []string{"global"})

		newPriority := 1
		updateArgs := &rpc.UpdateArgs{
			ID:       id,
			Priority: &newPriority,
		}

		resp, err := client.Update(updateArgs)
		if err != nil {
			t.Fatalf("Failed to update: %v", err)
		}
		if !resp.Success {
			t.Fatalf("Update failed: %s", resp.Error)
		}

		// Verify
		showResp, err := client.Show(&rpc.ShowArgs{ID: id})
		if err != nil {
			t.Fatalf("Failed to show: %v", err)
		}

		var shown types.Issue
		json.Unmarshal(showResp.Data, &shown)

		if shown.Priority != 1 {
			t.Errorf("Priority: expected 1, got %d", shown.Priority)
		}
	})

	t.Run("edit_add_and_remove_labels", func(t *testing.T) {
		id := createTestAdvice(t, client, "Label edit test", "Test", []string{"global", "testing"})

		// Add a label and remove another
		updateArgs := &rpc.UpdateArgs{
			ID:           id,
			AddLabels:    []string{"rig:beads"},
			RemoveLabels: []string{"testing"},
		}

		resp, err := client.Update(updateArgs)
		if err != nil {
			t.Fatalf("Failed to update: %v", err)
		}
		if !resp.Success {
			t.Fatalf("Update failed: %s", resp.Error)
		}

		// Verify labels changed
		showResp, err := client.Show(&rpc.ShowArgs{ID: id})
		if err != nil {
			t.Fatalf("Failed to show: %v", err)
		}

		var shown types.Issue
		json.Unmarshal(showResp.Data, &shown)

		hasGlobal := false
		hasRigBeads := false
		hasTesting := false
		for _, l := range shown.Labels {
			switch l {
			case "global":
				hasGlobal = true
			case "rig:beads":
				hasRigBeads = true
			case "testing":
				hasTesting = true
			}
		}

		if !hasGlobal {
			t.Errorf("Expected 'global' label to remain, got %v", shown.Labels)
		}
		if !hasRigBeads {
			t.Errorf("Expected 'rig:beads' label to be added, got %v", shown.Labels)
		}
		if hasTesting {
			t.Errorf("Expected 'testing' label to be removed, got %v", shown.Labels)
		}
	})

	t.Run("edit_hook_fields", func(t *testing.T) {
		// Create advice with initial hook
		createArgs := &rpc.CreateArgs{
			Title:               "Hook edit test",
			Description:         "Test editing hooks",
			IssueType:           "advice",
			Priority:            2,
			Labels:              []string{"role:polecat"},
			AdviceHookCommand:   "make test",
			AdviceHookTrigger:   "before-commit",
			AdviceHookTimeout:   30,
			AdviceHookOnFailure: "warn",
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Failed to create: %v", err)
		}
		var created types.Issue
		json.Unmarshal(resp.Data, &created)

		// Update hook fields
		newCommand := "make test && make lint"
		newTrigger := "before-push"
		newTimeout := 120
		newOnFailure := "block"

		updateArgs := &rpc.UpdateArgs{
			ID:                  created.ID,
			AdviceHookCommand:   &newCommand,
			AdviceHookTrigger:   &newTrigger,
			AdviceHookTimeout:   &newTimeout,
			AdviceHookOnFailure: &newOnFailure,
		}

		updateResp, err := client.Update(updateArgs)
		if err != nil {
			t.Fatalf("Failed to update: %v", err)
		}
		if !updateResp.Success {
			t.Fatalf("Update failed: %s", updateResp.Error)
		}

		// Verify hook fields updated
		showResp, err := client.Show(&rpc.ShowArgs{ID: created.ID})
		if err != nil {
			t.Fatalf("Failed to show: %v", err)
		}

		var shown types.Issue
		json.Unmarshal(showResp.Data, &shown)

		if shown.AdviceHookCommand != newCommand {
			t.Errorf("HookCommand: expected %q, got %q", newCommand, shown.AdviceHookCommand)
		}
		if shown.AdviceHookTrigger != newTrigger {
			t.Errorf("HookTrigger: expected %q, got %q", newTrigger, shown.AdviceHookTrigger)
		}
		if shown.AdviceHookTimeout != newTimeout {
			t.Errorf("HookTimeout: expected %d, got %d", newTimeout, shown.AdviceHookTimeout)
		}
		if shown.AdviceHookOnFailure != newOnFailure {
			t.Errorf("HookOnFailure: expected %q, got %q", newOnFailure, shown.AdviceHookOnFailure)
		}
	})

	t.Run("edit_clear_hook_command", func(t *testing.T) {
		createArgs := &rpc.CreateArgs{
			Title:             "Clear hook test",
			Description:       "Test clearing hook",
			IssueType:         "advice",
			Priority:          2,
			Labels:            []string{"global"},
			AdviceHookCommand: "make test",
			AdviceHookTrigger: "before-commit",
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Failed to create: %v", err)
		}
		var created types.Issue
		json.Unmarshal(resp.Data, &created)

		// Clear hook command
		emptyCommand := ""
		updateArgs := &rpc.UpdateArgs{
			ID:                created.ID,
			AdviceHookCommand: &emptyCommand,
		}

		updateResp, err := client.Update(updateArgs)
		if err != nil {
			t.Fatalf("Failed to update: %v", err)
		}
		if !updateResp.Success {
			t.Fatalf("Update failed: %s", updateResp.Error)
		}

		// Verify hook command is cleared
		showResp, err := client.Show(&rpc.ShowArgs{ID: created.ID})
		if err != nil {
			t.Fatalf("Failed to show: %v", err)
		}

		var shown types.Issue
		json.Unmarshal(showResp.Data, &shown)

		if shown.AdviceHookCommand != "" {
			t.Errorf("Expected empty hook command after clear, got %q", shown.AdviceHookCommand)
		}
	})
}

// ============================================================================
// Test: bd advice list
// ============================================================================

func TestAdviceIntegration_List(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	t.Run("list_all_advice", func(t *testing.T) {
		// Create several advice beads
		createTestAdvice(t, client, "Advice 1", "First", []string{"global"})
		createTestAdvice(t, client, "Advice 2", "Second", []string{"rig:beads"})
		createTestAdvice(t, client, "Advice 3", "Third", []string{"role:polecat"})

		// Also create a non-advice issue (should not appear)
		nonAdvice := &rpc.CreateArgs{
			Title:     "Regular task",
			IssueType: "task",
			Priority:  2,
		}
		client.Create(nonAdvice)

		// List all advice
		listResp, err := client.List(&rpc.ListArgs{
			IssueType: "advice",
			Status:    "open",
		})
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}
		if !listResp.Success {
			t.Fatalf("List failed: %s", listResp.Error)
		}

		var issues []*types.IssueWithCounts
		if err := json.Unmarshal(listResp.Data, &issues); err != nil {
			// Try plain issue array
			var plainIssues []*types.Issue
			if err2 := json.Unmarshal(listResp.Data, &plainIssues); err2 != nil {
				t.Fatalf("Failed to unmarshal as IssueWithCounts or Issue: %v / %v", err, err2)
			}
			if len(plainIssues) < 3 {
				t.Errorf("Expected at least 3 advice issues, got %d", len(plainIssues))
			}
			for _, issue := range plainIssues {
				if issue.IssueType != types.TypeAdvice {
					t.Errorf("Non-advice issue in list: type=%q title=%q", issue.IssueType, issue.Title)
				}
			}
			return
		}

		if len(issues) < 3 {
			t.Errorf("Expected at least 3 advice issues, got %d", len(issues))
		}

		// Verify all are advice type
		for _, iwc := range issues {
			if iwc.Issue.IssueType != types.TypeAdvice {
				t.Errorf("Non-advice issue in list: type=%q title=%q", iwc.Issue.IssueType, iwc.Issue.Title)
			}
		}
	})

	t.Run("list_filter_by_label", func(t *testing.T) {
		// Create advice with different labels
		createTestAdvice(t, client, "Beads only", "For beads", []string{"rig:beads"})
		createTestAdvice(t, client, "Gastown only", "For gastown", []string{"rig:gastown"})

		// Filter by rig:gastown
		listResp, err := client.List(&rpc.ListArgs{
			IssueType: "advice",
			Labels:    []string{"rig:gastown"},
			Status:    "open",
		})
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}
		if !listResp.Success {
			t.Fatalf("List failed: %s", listResp.Error)
		}

		var issues []*types.Issue
		if err := json.Unmarshal(listResp.Data, &issues); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		// Should contain at least the gastown advice
		found := false
		for _, issue := range issues {
			if issue.Title == "Gastown only" {
				found = true
			}
		}
		if !found {
			titles := make([]string, len(issues))
			for i, issue := range issues {
				titles[i] = issue.Title
			}
			t.Errorf("Expected 'Gastown only' in filtered list, got: %v", titles)
		}
	})

	t.Run("list_labels_any_or_semantics", func(t *testing.T) {
		// Create advice with different custom labels
		createTestAdvice(t, client, "Security advice", "Sec", []string{"security"})
		createTestAdvice(t, client, "Testing advice", "Test", []string{"testing"})
		createTestAdvice(t, client, "Performance advice", "Perf", []string{"performance"})

		// Filter by LabelsAny (OR semantics)
		listResp, err := client.List(&rpc.ListArgs{
			IssueType: "advice",
			LabelsAny: []string{"security", "testing"},
			Status:    "open",
		})
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}
		if !listResp.Success {
			t.Fatalf("List failed: %s", listResp.Error)
		}

		var issues []*types.Issue
		json.Unmarshal(listResp.Data, &issues)

		foundSecurity := false
		foundTesting := false
		for _, issue := range issues {
			if issue.Title == "Security advice" {
				foundSecurity = true
			}
			if issue.Title == "Testing advice" {
				foundTesting = true
			}
		}

		if !foundSecurity {
			t.Error("Expected 'Security advice' in OR-filtered list")
		}
		if !foundTesting {
			t.Error("Expected 'Testing advice' in OR-filtered list")
		}
	})

	t.Run("list_excludes_closed", func(t *testing.T) {
		id := createTestAdvice(t, client, "Will be closed", "To close", []string{"global"})

		// Close the advice
		closeResp, err := client.CloseIssue(&rpc.CloseArgs{
			ID:     id,
			Reason: "No longer needed",
		})
		if err != nil {
			t.Fatalf("Failed to close: %v", err)
		}
		if !closeResp.Success {
			t.Fatalf("Close failed: %s", closeResp.Error)
		}

		// List open advice - closed should not appear
		listResp, err := client.List(&rpc.ListArgs{
			IssueType: "advice",
			Status:    "open",
		})
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}

		var issues []*types.Issue
		json.Unmarshal(listResp.Data, &issues)

		for _, issue := range issues {
			if issue.ID == id {
				t.Errorf("Closed advice %s should not appear in open list", id)
			}
		}
	})
}

// ============================================================================
// Test: bd advice preview
// ============================================================================

func TestAdviceIntegration_Preview(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	// Create a variety of advice for preview testing
	createTestAdvice(t, client, "Global: check errors", "Always check for errors", []string{"global"})
	createTestAdvice(t, client, "Beads: use go test", "Use go test ./...", []string{"rig:beads"})
	createTestAdvice(t, client, "Polecat: run gt done", "Complete work properly", []string{"role:polecat"})
	createTestAdvice(t, client, "Beads polecat: specific advice", "Combined targeting", []string{"rig:beads", "role:polecat"})
	createTestAdvice(t, client, "Gastown: different rig", "Not for beads", []string{"rig:gastown"})

	t.Run("preview_for_beads_polecat", func(t *testing.T) {
		// Simulate what bd advice preview --for=beads/polecats/quartz does:
		// 1. Build subscriptions (without store access in daemon mode)
		agentID := "beads/polecats/quartz"
		subscriptions := buildAgentSubscriptionsWithoutStore(agentID, nil)

		// Verify subscriptions are correct
		subSet := make(map[string]bool)
		for _, s := range subscriptions {
			subSet[s] = true
		}

		if !subSet["global"] {
			t.Error("Expected 'global' in subscriptions")
		}
		if !subSet["rig:beads"] {
			t.Error("Expected 'rig:beads' in subscriptions")
		}
		if !subSet["role:polecats"] || !subSet["role:polecat"] {
			t.Errorf("Expected 'role:polecats' and 'role:polecat' in subscriptions, got %v", subscriptions)
		}

		// 2. List all open advice via daemon
		listResp, err := client.List(&rpc.ListArgs{
			IssueType: "advice",
			Status:    "open",
		})
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}

		var issuesWithCounts []*types.IssueWithCounts
		if err := json.Unmarshal(listResp.Data, &issuesWithCounts); err != nil {
			// Fall back to plain issues
			var issues []*types.Issue
			if err2 := json.Unmarshal(listResp.Data, &issues); err2 != nil {
				t.Fatalf("Failed to unmarshal: %v", err2)
			}
			// Build labels map from issues
			labelsMap := make(map[string][]string)
			for _, issue := range issues {
				labelsMap[issue.ID] = issue.Labels
			}

			// 3. Filter with matchesSubscriptions
			var matched []string
			for _, issue := range issues {
				if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
					matched = append(matched, issue.Title)
				}
			}

			// Verify expected matches
			assertContains(t, matched, "Global: check errors")
			assertContains(t, matched, "Beads: use go test")
			assertContains(t, matched, "Polecat: run gt done")
			assertContains(t, matched, "Beads polecat: specific advice")
			assertNotContains(t, matched, "Gastown: different rig")
			return
		}

		// Build labels map from IssueWithCounts
		issues := make([]*types.Issue, len(issuesWithCounts))
		labelsMap := make(map[string][]string)
		for i, iwc := range issuesWithCounts {
			issues[i] = iwc.Issue
			if iwc.Issue != nil {
				labelsMap[iwc.Issue.ID] = iwc.Issue.Labels
			}
		}

		// 3. Filter with matchesSubscriptions
		var matched []string
		for _, issue := range issues {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				matched = append(matched, issue.Title)
			}
		}

		// Global should match
		assertContains(t, matched, "Global: check errors")
		// rig:beads should match (agent is in beads)
		assertContains(t, matched, "Beads: use go test")
		// role:polecat should match
		assertContains(t, matched, "Polecat: run gt done")
		// rig:beads + role:polecat should match
		assertContains(t, matched, "Beads polecat: specific advice")
		// rig:gastown should NOT match (agent is in beads, not gastown)
		assertNotContains(t, matched, "Gastown: different rig")
	})

	t.Run("preview_for_gastown_crew", func(t *testing.T) {
		agentID := "gastown/crew/auth_fixer"
		subscriptions := buildAgentSubscriptionsWithoutStore(agentID, nil)

		listResp, err := client.List(&rpc.ListArgs{
			IssueType: "advice",
			Status:    "open",
		})
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}

		var issues []*types.Issue
		json.Unmarshal(listResp.Data, &issues)

		labelsMap := make(map[string][]string)
		for _, issue := range issues {
			labelsMap[issue.ID] = issue.Labels
		}

		var matched []string
		for _, issue := range issues {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				matched = append(matched, issue.Title)
			}
		}

		// Global should match
		assertContains(t, matched, "Global: check errors")
		// rig:gastown should match
		assertContains(t, matched, "Gastown: different rig")
		// rig:beads should NOT match
		assertNotContains(t, matched, "Beads: use go test")
		// Polecat-targeted should NOT match (crew != polecat)
		assertNotContains(t, matched, "Polecat: run gt done")
	})

	t.Run("preview_scope_grouping", func(t *testing.T) {
		agentID := "beads/polecats/quartz"
		subscriptions := buildAgentSubscriptionsWithoutStore(agentID, nil)

		listResp, _ := client.List(&rpc.ListArgs{
			IssueType: "advice",
			Status:    "open",
		})

		var issues []*types.Issue
		json.Unmarshal(listResp.Data, &issues)

		labelsMap := make(map[string][]string)
		for _, issue := range issues {
			labelsMap[issue.ID] = issue.Labels
		}

		// Build preview items
		var previewItems []*advicePreviewItem
		for _, issue := range issues {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				matchedLabels := findMatchedLabels(labelsMap[issue.ID], subscriptions)
				previewItems = append(previewItems, &advicePreviewItem{
					Issue:         issue,
					MatchedLabels: matchedLabels,
				})
			}
		}

		// Group by scope
		groups := groupByScope(previewItems)

		// Verify groups exist
		scopeMap := make(map[string]bool)
		for _, g := range groups {
			scopeMap[g.Scope] = true
		}

		if !scopeMap["global"] {
			t.Error("Expected 'global' scope group")
		}
		// Rig or role groups should also exist
		if !scopeMap["rig"] && !scopeMap["role"] {
			t.Error("Expected at least one 'rig' or 'role' scope group")
		}
	})
}

// ============================================================================
// Test: Advice RPC round-trip (full create-show-edit-list-close cycle)
// ============================================================================

func TestAdviceIntegration_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	// Step 1: Create advice with hook
	createArgs := &rpc.CreateArgs{
		Title:               "Run tests before commit",
		Description:         "All unit tests must pass before committing code.",
		IssueType:           "advice",
		Priority:            1,
		Labels:              []string{"global", "rig:beads"},
		AdviceHookCommand:   "make test",
		AdviceHookTrigger:   "before-commit",
		AdviceHookTimeout:   60,
		AdviceHookOnFailure: "block",
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Step 1 - Create failed: %v", err)
	}
	if !createResp.Success {
		t.Fatalf("Step 1 - Create failed: %s", createResp.Error)
	}

	var created types.Issue
	json.Unmarshal(createResp.Data, &created)
	adviceID := created.ID

	if adviceID == "" {
		t.Fatal("Step 1 - Created advice has empty ID")
	}
	if created.IssueType != types.TypeAdvice {
		t.Errorf("Step 1 - Expected type 'advice', got %q", created.IssueType)
	}

	// Step 2: Show - verify all fields
	showResp, err := client.Show(&rpc.ShowArgs{ID: adviceID})
	if err != nil {
		t.Fatalf("Step 2 - Show failed: %v", err)
	}

	var shown types.Issue
	json.Unmarshal(showResp.Data, &shown)

	if shown.Title != "Run tests before commit" {
		t.Errorf("Step 2 - Title: expected %q, got %q", "Run tests before commit", shown.Title)
	}
	if shown.Priority != 1 {
		t.Errorf("Step 2 - Priority: expected 1, got %d", shown.Priority)
	}
	if shown.AdviceHookCommand != "make test" {
		t.Errorf("Step 2 - HookCommand: expected 'make test', got %q", shown.AdviceHookCommand)
	}

	// Step 3: Edit - update title and add a label
	newTitle := "Run ALL tests before commit"
	updateArgs := &rpc.UpdateArgs{
		ID:        adviceID,
		Title:     &newTitle,
		AddLabels: []string{"role:polecat"},
	}

	updateResp, err := client.Update(updateArgs)
	if err != nil {
		t.Fatalf("Step 3 - Update failed: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Step 3 - Update failed: %s", updateResp.Error)
	}

	// Step 4: Show again - verify edit took effect
	showResp2, err := client.Show(&rpc.ShowArgs{ID: adviceID})
	if err != nil {
		t.Fatalf("Step 4 - Show failed: %v", err)
	}

	var shown2 types.Issue
	json.Unmarshal(showResp2.Data, &shown2)

	if shown2.Title != "Run ALL tests before commit" {
		t.Errorf("Step 4 - Title not updated: got %q", shown2.Title)
	}

	// Check that role:polecat was added
	hasPolecatLabel := false
	for _, l := range shown2.Labels {
		if l == "role:polecat" {
			hasPolecatLabel = true
		}
	}
	if !hasPolecatLabel {
		t.Errorf("Step 4 - Expected 'role:polecat' label after edit, got %v", shown2.Labels)
	}

	// Step 5: List - verify it appears in filtered list
	listResp, err := client.List(&rpc.ListArgs{
		IssueType: "advice",
		Status:    "open",
	})
	if err != nil {
		t.Fatalf("Step 5 - List failed: %v", err)
	}

	var allAdvice []*types.Issue
	json.Unmarshal(listResp.Data, &allAdvice)

	found := false
	for _, a := range allAdvice {
		if a.ID == adviceID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Step 5 - Created advice not found in list")
	}

	// Step 6: Close advice
	closeResp, err := client.CloseIssue(&rpc.CloseArgs{
		ID:     adviceID,
		Reason: "Superseded by CI pipeline",
	})
	if err != nil {
		t.Fatalf("Step 6 - Close failed: %v", err)
	}
	if !closeResp.Success {
		t.Fatalf("Step 6 - Close failed: %s", closeResp.Error)
	}

	// Step 7: Verify closed
	showResp3, err := client.Show(&rpc.ShowArgs{ID: adviceID})
	if err != nil {
		t.Fatalf("Step 7 - Show failed: %v", err)
	}

	var shown3 types.Issue
	json.Unmarshal(showResp3.Data, &shown3)

	if shown3.Status != types.StatusClosed {
		t.Errorf("Step 7 - Expected status 'closed', got %q", shown3.Status)
	}

	// Step 8: Verify closed advice excluded from open list
	listResp2, err := client.List(&rpc.ListArgs{
		IssueType: "advice",
		Status:    "open",
	})
	if err != nil {
		t.Fatalf("Step 8 - List failed: %v", err)
	}

	var openAdvice []*types.Issue
	json.Unmarshal(listResp2.Data, &openAdvice)

	for _, a := range openAdvice {
		if a.ID == adviceID {
			t.Error("Step 8 - Closed advice should not appear in open list")
		}
	}
}

// ============================================================================
// Test: Compound label matching over RPC
// ============================================================================

func TestAdviceIntegration_CompoundLabels_AND(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	// Create advice with compound labels (AND within group: g0:role:polecat, g0:rig:beads)
	createTestAdvice(t, client, "AND group advice", "Requires both", []string{"g0:role:polecat", "g0:rig:beads"})

	listResp, _ := client.List(&rpc.ListArgs{
		IssueType: "advice",
		Status:    "open",
	})

	var issues []*types.Issue
	json.Unmarshal(listResp.Data, &issues)

	labelsMap := make(map[string][]string)
	for _, issue := range issues {
		labelsMap[issue.ID] = issue.Labels
	}

	// Agent with both rig:beads and role:polecat should match
	beadsPolecat := buildAgentSubscriptionsWithoutStore("beads/polecats/quartz", nil)
	matched := false
	for _, issue := range issues {
		if issue.Title == "AND group advice" && matchesSubscriptions(issue, labelsMap[issue.ID], beadsPolecat) {
			matched = true
		}
	}
	if !matched {
		t.Error("Expected beads/polecats/quartz to match AND group advice")
	}

	// Agent with only rig:gastown should NOT match (missing role:polecat AND rig:beads)
	gastownCrew := buildAgentSubscriptionsWithoutStore("gastown/crew/worker", nil)
	for _, issue := range issues {
		if issue.Title == "AND group advice" && matchesSubscriptions(issue, labelsMap[issue.ID], gastownCrew) {
			t.Error("gastown/crew/worker should NOT match AND group advice (rig mismatch)")
		}
	}
}

func TestAdviceIntegration_CompoundLabels_OR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use a fresh server to avoid label cache interference
	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	// Create advice with OR groups: g0:role:polecat OR g1:role:crew
	createTestAdvice(t, client, "OR group advice", "Either polecat or crew", []string{"g0:role:polecat", "g1:role:crew"})

	listResp, _ := client.List(&rpc.ListArgs{
		IssueType: "advice",
		Status:    "open",
	})

	var issues []*types.Issue
	json.Unmarshal(listResp.Data, &issues)

	labelsMap := make(map[string][]string)
	for _, issue := range issues {
		labelsMap[issue.ID] = issue.Labels
	}

	// Polecat should match
	polecatSubs := buildAgentSubscriptionsWithoutStore("beads/polecats/quartz", nil)
	polecatMatched := false
	for _, issue := range issues {
		if issue.Title == "OR group advice" && matchesSubscriptions(issue, labelsMap[issue.ID], polecatSubs) {
			polecatMatched = true
		}
	}
	if !polecatMatched {
		t.Error("Expected polecat to match OR group advice")
	}

	// Crew should also match
	crewSubs := buildAgentSubscriptionsWithoutStore("beads/crew/worker", nil)
	crewMatched := false
	for _, issue := range issues {
		if issue.Title == "OR group advice" && matchesSubscriptions(issue, labelsMap[issue.ID], crewSubs) {
			crewMatched = true
		}
	}
	if !crewMatched {
		t.Error("Expected crew to match OR group advice")
	}
}

// ============================================================================
// Test: ID resolution over RPC
// ============================================================================

func TestAdviceIntegration_IDResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, cleanup := setupAdviceTestServer(t)
	defer cleanup()

	t.Run("resolve_partial_id", func(t *testing.T) {
		id := createTestAdvice(t, client, "ID resolution test", "Testing partial ID", []string{"global"})

		// Extract short ID (hash part after prefix-)
		parts := strings.Split(id, "-")
		if len(parts) < 2 {
			t.Fatalf("Unexpected ID format: %s", id)
		}
		shortID := parts[len(parts)-1]

		// Resolve partial ID via RPC (same as advice_show.go)
		resolveResp, err := client.ResolveID(&rpc.ResolveIDArgs{ID: shortID})
		if err != nil {
			t.Fatalf("Failed to resolve ID: %v", err)
		}
		if !resolveResp.Success {
			t.Fatalf("Resolve failed: %s", resolveResp.Error)
		}

		var resolvedID string
		json.Unmarshal(resolveResp.Data, &resolvedID)

		if resolvedID != id {
			t.Errorf("Resolved ID: expected %q, got %q", id, resolvedID)
		}
	})

	t.Run("resolve_full_id", func(t *testing.T) {
		id := createTestAdvice(t, client, "Full ID test", "Full ID", []string{"global"})

		resolveResp, err := client.ResolveID(&rpc.ResolveIDArgs{ID: id})
		if err != nil {
			t.Fatalf("Failed to resolve: %v", err)
		}

		var resolvedID string
		json.Unmarshal(resolveResp.Data, &resolvedID)

		if resolvedID != id {
			t.Errorf("Full ID resolution: expected %q, got %q", id, resolvedID)
		}
	})
}

// ============================================================================
// Helpers
// ============================================================================

func assertContains(t *testing.T, items []string, expected string) {
	t.Helper()
	for _, item := range items {
		if item == expected {
			return
		}
	}
	t.Errorf("Expected %q in %v", expected, items)
}

func assertNotContains(t *testing.T, items []string, unexpected string) {
	t.Helper()
	for _, item := range items {
		if item == unexpected {
			t.Errorf("Did not expect %q in %v", unexpected, items)
			return
		}
	}
}
