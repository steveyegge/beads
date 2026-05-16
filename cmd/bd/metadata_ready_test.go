//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestGetReadyWork_MetadataSuite(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	store := newTestStore(t, tmpDir)
	ctx := context.Background()

	// Create all test data up front with unique metadata keys per subtest.
	allIssues := []*types.Issue{
		// --- FieldMatch data ---
		{ID: "mr-fm-1", Title: "Platform task (fm)", Priority: 2, IssueType: types.TypeTask, Status: types.StatusOpen,
			Metadata: json.RawMessage(`{"mr_fm_team":"platform"}`)},
		{ID: "mr-fm-2", Title: "Frontend task (fm)", Priority: 2, IssueType: types.TypeTask, Status: types.StatusOpen,
			Metadata: json.RawMessage(`{"mr_fm_team":"frontend"}`)},
		// --- HasMetadataKey data ---
		{ID: "mr-hmk-1", Title: "Has team (hmk)", Priority: 2, IssueType: types.TypeTask, Status: types.StatusOpen,
			Metadata: json.RawMessage(`{"mr_hmk_team":"platform"}`)},
		{ID: "mr-hmk-2", Title: "No metadata (hmk)", Priority: 2, IssueType: types.TypeTask, Status: types.StatusOpen},
		// --- NoMatch data ---
		{ID: "mr-nm-1", Title: "Platform task (nm)", Priority: 2, IssueType: types.TypeTask, Status: types.StatusOpen,
			Metadata: json.RawMessage(`{"mr_nm_team":"platform"}`)},
	}
	for _, issue := range allIssues {
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("CreateIssue(%s): %v", issue.ID, err)
		}
	}

	t.Run("FieldMatch", func(t *testing.T) {
		results, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:         "open",
			MetadataFields: map[string]string{"mr_fm_team": "platform"},
		})
		if err != nil {
			t.Fatalf("GetReadyWork: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].ID != "mr-fm-1" {
			t.Errorf("expected issue mr-fm-1, got %s", results[0].ID)
		}
	})

	t.Run("HasMetadataKey", func(t *testing.T) {
		results, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:         "open",
			HasMetadataKey: "mr_hmk_team",
		})
		if err != nil {
			t.Fatalf("GetReadyWork: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].ID != "mr-hmk-1" {
			t.Errorf("expected issue mr-hmk-1, got %s", results[0].ID)
		}
	})

	t.Run("FieldNoMatch", func(t *testing.T) {
		results, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:         "open",
			MetadataFields: map[string]string{"mr_nm_team": "backend"},
		})
		if err != nil {
			t.Fatalf("GetReadyWork: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("FieldInvalidKey", func(t *testing.T) {
		_, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:         "open",
			MetadataFields: map[string]string{"'; DROP TABLE issues; --": "val"},
		})
		if err == nil {
			t.Fatal("expected error for invalid metadata key, got nil")
		}
	})
}

func TestGetReadyWork_WispBackedHonorsUnassignedAndRoutedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prefix    string
		ephemeral bool
		noHistory bool
	}{
		{name: "Ephemeral", prefix: "mr-eph", ephemeral: true},
		{name: "NoHistory", prefix: "mr-nh", noHistory: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			store := newTestStore(t, tmpDir)
			ctx := context.Background()

			issues := []*types.Issue{
				{
					ID:        tc.prefix + "-matching",
					Title:     "Matching wisp-backed task",
					Priority:  2,
					IssueType: types.TypeTask,
					Status:    types.StatusOpen,
					Ephemeral: tc.ephemeral,
					NoHistory: tc.noHistory,
					Metadata:  json.RawMessage(`{"gc.routed_to":"gascity/workflows.codex-min"}`),
				},
				{
					ID:        tc.prefix + "-wrong-route",
					Title:     "Wrong routed target",
					Priority:  2,
					IssueType: types.TypeTask,
					Status:    types.StatusOpen,
					Ephemeral: tc.ephemeral,
					NoHistory: tc.noHistory,
					Metadata:  json.RawMessage(`{"gc.routed_to":"gascity/workflows.claude-max"}`),
				},
				{
					ID:        tc.prefix + "-assigned",
					Title:     "Already assigned",
					Priority:  2,
					IssueType: types.TypeTask,
					Status:    types.StatusOpen,
					Assignee:  "gascity/workflows.other",
					Ephemeral: tc.ephemeral,
					NoHistory: tc.noHistory,
					Metadata:  json.RawMessage(`{"gc.routed_to":"gascity/workflows.codex-min"}`),
				},
			}
			for _, issue := range issues {
				if err := store.CreateIssue(ctx, issue, "test"); err != nil {
					t.Fatalf("CreateIssue(%s): %v", issue.ID, err)
				}
			}

			results, err := store.GetReadyWork(ctx, types.WorkFilter{
				Status:           types.StatusOpen,
				IncludeEphemeral: true,
				Unassigned:       true,
				MetadataFields:   map[string]string{"gc.routed_to": "gascity/workflows.codex-min"},
			})
			if err != nil {
				t.Fatalf("GetReadyWork: %v", err)
			}

			wantID := tc.prefix + "-matching"
			if len(results) != 1 {
				t.Fatalf("expected 1 matching wisp-backed result, got %d: %v", len(results), readyIssueIDs(results))
			}
			if results[0].ID != wantID {
				t.Fatalf("expected %s, got %s", wantID, results[0].ID)
			}
		})
	}
}

func TestGetReadyWork_DefaultIncludesNoHistoryButNotEphemeral(t *testing.T) {
	tmpDir := t.TempDir()
	store := newTestStore(t, tmpDir)
	ctx := context.Background()

	issues := []*types.Issue{
		{
			ID:        "mr-vis-persistent",
			Title:     "Persistent task",
			Priority:  2,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Metadata:  json.RawMessage(`{"gc.routed_to":"gascity/workflows.codex-min"}`),
		},
		{
			ID:        "mr-vis-no-history",
			Title:     "No-history task",
			Priority:  2,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			NoHistory: true,
			Metadata:  json.RawMessage(`{"gc.routed_to":"gascity/workflows.codex-min"}`),
		},
		{
			ID:        "mr-vis-ephemeral",
			Title:     "Ephemeral task",
			Priority:  2,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Ephemeral: true,
			Metadata:  json.RawMessage(`{"gc.routed_to":"gascity/workflows.codex-min"}`),
		},
	}
	for _, issue := range issues {
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("CreateIssue(%s): %v", issue.ID, err)
		}
	}

	filter := types.WorkFilter{
		Status:         types.StatusOpen,
		Unassigned:     true,
		MetadataFields: map[string]string{"gc.routed_to": "gascity/workflows.codex-min"},
	}
	defaultResults, err := store.GetReadyWork(ctx, filter)
	if err != nil {
		t.Fatalf("GetReadyWork default: %v", err)
	}
	if got := readyIssueIDs(defaultResults); !sameStringSet(got, []string{"mr-vis-persistent", "mr-vis-no-history"}) {
		t.Fatalf("default ready IDs = %v, want persistent plus no-history only", got)
	}

	filter.IncludeEphemeral = true
	allResults, err := store.GetReadyWork(ctx, filter)
	if err != nil {
		t.Fatalf("GetReadyWork include ephemeral: %v", err)
	}
	if got := readyIssueIDs(allResults); !sameStringSet(got, []string{"mr-vis-persistent", "mr-vis-no-history", "mr-vis-ephemeral"}) {
		t.Fatalf("include-ephemeral ready IDs = %v, want persistent, no-history, and ephemeral", got)
	}
}

func readyIssueIDs(issues []*types.Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[string]int, len(want))
	for _, v := range want {
		counts[v]++
	}
	for _, v := range got {
		counts[v]--
		if counts[v] < 0 {
			return false
		}
	}
	return true
}
