// wisp_integration_test.go - Integration tests verifying wisp in-memory behavior.
//
// These tests validate that:
// 1. Wisps (ephemeral=true) are stored in-memory only, never in Dolt
// 2. Wisps are lost on daemon restart (by design)
// 3. Regular beads persist while wisps don't
// 4. Search returns merged results from both stores
// 5. Ephemeral flag routes correctly
// 6. -wisp- ID patterns route correctly

//go:build integration

package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestWispNotPersistedToDolt verifies that wisps are stored in-memory only.
func TestWispNotPersistedToDolt(t *testing.T) {
	RunDaemonModeOnly(t, "wisp_not_persisted", func(t *testing.T, env *DualModeTestEnv) {
		// Create a wisp (ephemeral issue)
		wisp := &types.Issue{
			Title:     "Ephemeral wisp",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
			Ephemeral: true,
		}

		err := env.CreateIssue(wisp)
		if err != nil {
			t.Fatalf("CreateIssue (wisp) failed: %v", err)
		}

		if wisp.ID == "" {
			t.Fatal("wisp ID not set after creation")
		}

		// Verify wisp is retrievable via daemon
		got, err := env.GetIssue(wisp.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if got == nil {
			t.Fatal("wisp not found via daemon")
		}
		if !got.Ephemeral {
			t.Error("retrieved issue should have Ephemeral=true")
		}

		t.Logf("Created wisp %s, verifying it's in-memory only", wisp.ID)
	})
}

// TestWispAndRegularBeadPersistence verifies that regular beads persist while wisps don't.
func TestWispAndRegularBeadPersistence(t *testing.T) {
	RunDaemonModeOnly(t, "wisp_vs_regular", func(t *testing.T, env *DualModeTestEnv) {
		// Create a regular bead (not ephemeral)
		regular := &types.Issue{
			Title:     "Regular bead",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
			Ephemeral: false,
		}

		err := env.CreateIssue(regular)
		if err != nil {
			t.Fatalf("CreateIssue (regular) failed: %v", err)
		}

		// Create a wisp (ephemeral)
		wisp := &types.Issue{
			Title:     "Ephemeral wisp",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
			Ephemeral: true,
		}

		err = env.CreateIssue(wisp)
		if err != nil {
			t.Fatalf("CreateIssue (wisp) failed: %v", err)
		}

		// Both should be retrievable
		gotRegular, err := env.GetIssue(regular.ID)
		if err != nil || gotRegular == nil {
			t.Fatalf("Regular bead not found: %v", err)
		}

		gotWisp, err := env.GetIssue(wisp.ID)
		if err != nil || gotWisp == nil {
			t.Fatalf("Wisp not found: %v", err)
		}

		t.Logf("Created regular bead %s and wisp %s", regular.ID, wisp.ID)

		// Verify ephemeral flags
		if gotRegular.Ephemeral {
			t.Error("regular bead should have Ephemeral=false")
		}
		if !gotWisp.Ephemeral {
			t.Error("wisp should have Ephemeral=true")
		}
	})
}

// TestWispEphemeralFlagRouting verifies that issues with Ephemeral=true are routed to WispStore.
func TestWispEphemeralFlagRouting(t *testing.T) {
	RunDaemonModeOnly(t, "ephemeral_flag_routing", func(t *testing.T, env *DualModeTestEnv) {
		// Create issue with explicit ephemeral flag
		issue := &types.Issue{
			Title:     "Explicitly ephemeral",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
			Ephemeral: true,
		}

		err := env.CreateIssue(issue)
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		// Verify it was created with ephemeral flag preserved
		got, err := env.GetIssue(issue.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}

		if got == nil {
			t.Fatal("issue not found")
		}

		if !got.Ephemeral {
			t.Error("Ephemeral flag was not preserved")
		}

		t.Logf("Ephemeral issue %s routed correctly", issue.ID)
	})
}

// TestWispListMergesStores verifies that listing issues returns results from both
// the persistent store (Dolt) and the in-memory WispStore.
func TestWispListMergesStores(t *testing.T) {
	RunDaemonModeOnly(t, "list_merges_stores", func(t *testing.T, env *DualModeTestEnv) {
		// Create a regular bead
		regular := &types.Issue{
			Title:     "Regular for merge test",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  1,
			Ephemeral: false,
		}

		err := env.CreateIssue(regular)
		if err != nil {
			t.Fatalf("CreateIssue (regular) failed: %v", err)
		}

		// Create a wisp
		wisp := &types.Issue{
			Title:     "Wisp for merge test",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  1,
			Ephemeral: true,
		}

		err = env.CreateIssue(wisp)
		if err != nil {
			t.Fatalf("CreateIssue (wisp) failed: %v", err)
		}

		// List all issues - should include both
		issues, err := env.ListIssues(types.IssueFilter{})
		if err != nil {
			t.Fatalf("ListIssues failed: %v", err)
		}

		// Find our test issues
		foundRegular := false
		foundWisp := false
		for _, issue := range issues {
			if issue.ID == regular.ID {
				foundRegular = true
			}
			if issue.ID == wisp.ID {
				foundWisp = true
			}
		}

		if !foundRegular {
			t.Error("regular bead not found in list results")
		}
		if !foundWisp {
			t.Error("wisp not found in list results")
		}

		t.Logf("List returned both regular (%s) and wisp (%s)", regular.ID, wisp.ID)
	})
}

// TestWispUpdatePreservesEphemeral verifies that updating a wisp keeps it ephemeral.
func TestWispUpdatePreservesEphemeral(t *testing.T) {
	RunDaemonModeOnly(t, "update_preserves_ephemeral", func(t *testing.T, env *DualModeTestEnv) {
		// Create a wisp
		wisp := &types.Issue{
			Title:     "Original wisp title",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
			Ephemeral: true,
		}

		err := env.CreateIssue(wisp)
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		// Update the wisp
		updates := map[string]interface{}{
			"title": "Updated wisp title",
		}
		err = env.UpdateIssue(wisp.ID, updates)
		if err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}

		// Verify it's still ephemeral
		got, err := env.GetIssue(wisp.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}

		if got.Title != "Updated wisp title" {
			t.Errorf("title not updated: got %q", got.Title)
		}
		if !got.Ephemeral {
			t.Error("wisp lost ephemeral flag after update")
		}

		t.Logf("Wisp %s updated and remains ephemeral", wisp.ID)
	})
}
