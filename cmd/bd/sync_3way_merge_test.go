package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// 3-Way Merge Tests
// Tests for sync merge behavior with concurrent local/remote changes, base state
// corruption, and missing base state scenarios.
// =============================================================================

// TestMergeWithConcurrentLocalChanges verifies merge correctly handles local
// changes made during the sync process.
func TestMergeWithConcurrentLocalChanges(t *testing.T) {
	now := time.Now()
	baseTime := now.Add(-2 * time.Hour)
	localTime := now.Add(-1 * time.Hour)
	remoteTime := now

	// Base state (last known sync point)
	base := []*types.Issue{
		{
			ID:        "bd-0001",
			Title:     "Original Title",
			Status:    types.StatusOpen,
			Priority:  1,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		},
		{
			ID:        "bd-0002",
			Title:     "Unchanged Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		},
	}

	// Local state (user made changes after last sync)
	local := []*types.Issue{
		{
			ID:        "bd-0001",
			Title:     "Local Edit",
			Status:    types.StatusInProgress,
			Priority:  1,
			UpdatedAt: localTime,
			CreatedAt: baseTime.Add(-time.Hour),
		},
		{
			ID:        "bd-0002",
			Title:     "Unchanged Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		},
		{
			ID:        "bd-0003",
			Title:     "New Local Issue",
			Status:    types.StatusOpen,
			Priority:  3,
			UpdatedAt: localTime,
			CreatedAt: localTime,
		},
	}

	// Remote state (other user made changes)
	remote := []*types.Issue{
		{
			ID:        "bd-0001",
			Title:     "Remote Edit",
			Status:    types.StatusClosed,
			Priority:  0,
			UpdatedAt: remoteTime, // newer than local
			CreatedAt: baseTime.Add(-time.Hour),
		},
		{
			ID:        "bd-0002",
			Title:     "Unchanged Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		},
		{
			ID:        "bd-0004",
			Title:     "New Remote Issue",
			Status:    types.StatusOpen,
			Priority:  4,
			UpdatedAt: remoteTime,
			CreatedAt: remoteTime,
		},
	}

	// Perform 3-way merge
	result := MergeIssues(base, local, remote)

	// Verify merge results
	if len(result.Merged) != 4 {
		t.Errorf("expected 4 merged issues, got %d", len(result.Merged))
	}

	// Build lookup map for verification
	merged := make(map[string]*types.Issue)
	for _, issue := range result.Merged {
		merged[issue.ID] = issue
	}

	// bd-0001: true conflict, remote wins (newer timestamp)
	if issue, ok := merged["bd-0001"]; !ok {
		t.Error("bd-0001 missing from merge result")
	} else {
		if issue.Title != "Remote Edit" {
			t.Errorf("bd-0001 title: expected 'Remote Edit' (remote wins), got %q", issue.Title)
		}
		if result.Strategy["bd-0001"] != StrategyMerged {
			t.Errorf("bd-0001 strategy: expected %q, got %q", StrategyMerged, result.Strategy["bd-0001"])
		}
	}

	// bd-0002: unchanged
	if issue, ok := merged["bd-0002"]; !ok {
		t.Error("bd-0002 missing from merge result")
	} else {
		if result.Strategy["bd-0002"] != StrategySame {
			t.Errorf("bd-0002 strategy: expected %q, got %q", StrategySame, result.Strategy["bd-0002"])
		}
		_ = issue
	}

	// bd-0003: new local (no base, no remote)
	if _, ok := merged["bd-0003"]; !ok {
		t.Error("bd-0003 (new local) missing from merge result")
	} else {
		if result.Strategy["bd-0003"] != StrategyLocal {
			t.Errorf("bd-0003 strategy: expected %q, got %q", StrategyLocal, result.Strategy["bd-0003"])
		}
	}

	// bd-0004: new remote (no base, no local)
	if _, ok := merged["bd-0004"]; !ok {
		t.Error("bd-0004 (new remote) missing from merge result")
	} else {
		if result.Strategy["bd-0004"] != StrategyRemote {
			t.Errorf("bd-0004 strategy: expected %q, got %q", StrategyRemote, result.Strategy["bd-0004"])
		}
	}

	// Verify conflict count
	if result.Conflicts != 1 {
		t.Errorf("expected 1 conflict (bd-0001), got %d", result.Conflicts)
	}
}

// TestMergeWithConflictingRemoteChanges tests merge when remote has conflicting changes.
func TestMergeWithConflictingRemoteChanges(t *testing.T) {
	now := time.Now()
	baseTime := now.Add(-3 * time.Hour)

	t.Run("same field different values local wins", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-conflict",
			Title:     "Original",
			Status:    types.StatusOpen,
			Priority:  1,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		local := []*types.Issue{{
			ID:        "bd-conflict",
			Title:     "Local Title",
			Status:    types.StatusInProgress,
			Priority:  2,
			UpdatedAt: now, // local is newer
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		remote := []*types.Issue{{
			ID:        "bd-conflict",
			Title:     "Remote Title",
			Status:    types.StatusClosed,
			Priority:  3,
			UpdatedAt: now.Add(-time.Hour), // remote is older
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		result := MergeIssues(base, local, remote)

		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
		}

		merged := result.Merged[0]
		if merged.Title != "Local Title" {
			t.Errorf("expected local title (newer), got %q", merged.Title)
		}
		if merged.Status != types.StatusInProgress {
			t.Errorf("expected local status (newer), got %s", merged.Status)
		}
	})

	t.Run("same field different values remote wins", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-conflict",
			Title:     "Original",
			Status:    types.StatusOpen,
			Priority:  1,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		local := []*types.Issue{{
			ID:        "bd-conflict",
			Title:     "Local Title",
			Status:    types.StatusInProgress,
			Priority:  2,
			UpdatedAt: now.Add(-time.Hour), // local is older
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		remote := []*types.Issue{{
			ID:        "bd-conflict",
			Title:     "Remote Title",
			Status:    types.StatusClosed,
			Priority:  3,
			UpdatedAt: now, // remote is newer
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		result := MergeIssues(base, local, remote)

		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
		}

		merged := result.Merged[0]
		if merged.Title != "Remote Title" {
			t.Errorf("expected remote title (newer), got %q", merged.Title)
		}
		if merged.Status != types.StatusClosed {
			t.Errorf("expected remote status (newer), got %s", merged.Status)
		}
	})
}

// TestBaseStateCorruption tests merge behavior when base state is corrupted.
func TestBaseStateCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	baseStatePath := filepath.Join(tmpDir, syncBaseFileName)

	t.Run("completely corrupted base state", func(t *testing.T) {
		// write garbage to base state file
		if err := os.WriteFile(baseStatePath, []byte("not valid json at all!!!"), 0644); err != nil {
			t.Fatalf("write corrupted base state: %v", err)
		}

		loaded, err := loadBaseState(tmpDir)
		// loadBaseState should handle malformed JSON gracefully
		if err != nil {
			t.Logf("loadBaseState returned error for corrupted file: %v", err)
		}
		if loaded != nil && len(loaded) > 0 {
			t.Logf("loadBaseState returned %d issues from corrupted file", len(loaded))
		}
	})

	t.Run("partially corrupted base state", func(t *testing.T) {
		// write partially valid JSONL
		content := `{"id":"bd-1","title":"Valid"}
not json
{"id":"bd-2","title":"Also Valid"}
`
		if err := os.WriteFile(baseStatePath, []byte(content), 0644); err != nil {
			t.Fatalf("write partial base state: %v", err)
		}

		loaded, err := loadBaseState(tmpDir)
		if err != nil {
			t.Logf("loadBaseState returned error: %v", err)
		}

		// should load the valid issues, skipping malformed lines
		if loaded != nil && len(loaded) != 2 {
			t.Errorf("expected 2 valid issues from partial corruption, got %d", len(loaded))
		}
	})

	t.Run("truncated JSON", func(t *testing.T) {
		// write truncated JSON line
		content := `{"id":"bd-1","title":"Complete"}
{"id":"bd-2","title":"Trunc`
		if err := os.WriteFile(baseStatePath, []byte(content), 0644); err != nil {
			t.Fatalf("write truncated base state: %v", err)
		}

		loaded, err := loadBaseState(tmpDir)
		if err != nil {
			t.Logf("loadBaseState returned error: %v", err)
		}

		// should load at least the valid issue
		if loaded == nil || len(loaded) < 1 {
			t.Error("expected at least 1 valid issue from truncated file")
		}
	})
}

// TestMergeWithMissingBaseState tests merge behavior when there's no base state.
func TestMergeWithMissingBaseState(t *testing.T) {
	now := time.Now()

	t.Run("first sync scenario", func(t *testing.T) {
		// no base state (nil)
		local := []*types.Issue{{
			ID:        "bd-local",
			Title:     "Local Issue",
			Status:    types.StatusOpen,
			Priority:  1,
			UpdatedAt: now,
			CreatedAt: now,
		}}

		remote := []*types.Issue{{
			ID:        "bd-remote",
			Title:     "Remote Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			UpdatedAt: now,
			CreatedAt: now,
		}}

		result := MergeIssues(nil, local, remote)

		if len(result.Merged) != 2 {
			t.Errorf("expected 2 merged issues (both new), got %d", len(result.Merged))
		}
	})

	t.Run("same issue both sides no base", func(t *testing.T) {
		// both have same issue, no base to determine who changed what
		local := []*types.Issue{{
			ID:        "bd-shared",
			Title:     "Local Version",
			Status:    types.StatusOpen,
			Priority:  1,
			UpdatedAt: now.Add(-time.Hour),
			CreatedAt: now.Add(-2 * time.Hour),
		}}

		remote := []*types.Issue{{
			ID:        "bd-shared",
			Title:     "Remote Version",
			Status:    types.StatusClosed,
			Priority:  2,
			UpdatedAt: now, // remote is newer
			CreatedAt: now.Add(-2 * time.Hour),
		}}

		result := MergeIssues(nil, local, remote)

		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
		}

		// without base, LWW should apply - remote wins (newer)
		if result.Merged[0].Title != "Remote Version" {
			t.Errorf("expected remote version (newer), got %q", result.Merged[0].Title)
		}
	})

	t.Run("load missing base state file", func(t *testing.T) {
		tmpDir := t.TempDir()

		loaded, err := loadBaseState(tmpDir)
		if err != nil {
			t.Fatalf("loadBaseState should not error on missing file: %v", err)
		}
		if loaded != nil {
			t.Errorf("expected nil for missing base state, got %d issues", len(loaded))
		}
	})
}

// TestSaveAndLoadBaseState tests base state persistence round-trip.
func TestSaveAndLoadBaseState(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now().Truncate(time.Second) // truncate for JSON precision

	issues := []*types.Issue{
		{
			ID:        "bd-0001",
			Title:     "First Issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
			UpdatedAt: now,
			CreatedAt: now.Add(-time.Hour),
		},
		{
			ID:        "bd-0002",
			Title:     "Second Issue",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeBug,
			UpdatedAt: now,
			CreatedAt: now.Add(-2 * time.Hour),
			Labels:    []string{"bug", "critical"},
		},
	}

	// Save
	if err := saveBaseState(tmpDir, issues); err != nil {
		t.Fatalf("saveBaseState failed: %v", err)
	}

	// Verify file exists
	baseStatePath := filepath.Join(tmpDir, syncBaseFileName)
	if _, err := os.Stat(baseStatePath); os.IsNotExist(err) {
		t.Fatal("base state file not created")
	}

	// Load
	loaded, err := loadBaseState(tmpDir)
	if err != nil {
		t.Fatalf("loadBaseState failed: %v", err)
	}

	// Verify content
	if len(loaded) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(loaded))
	}

	// Check first issue
	if loaded[0].ID != "bd-0001" {
		t.Errorf("first issue ID: expected 'bd-0001', got %q", loaded[0].ID)
	}
	if loaded[0].Title != "First Issue" {
		t.Errorf("first issue title: expected 'First Issue', got %q", loaded[0].Title)
	}

	// Check second issue
	if loaded[1].ID != "bd-0002" {
		t.Errorf("second issue ID: expected 'bd-0002', got %q", loaded[1].ID)
	}
	if len(loaded[1].Labels) != 2 {
		t.Errorf("second issue labels: expected 2, got %d", len(loaded[1].Labels))
	}
}

// TestMergeDeletionConflicts tests merge behavior with deletion conflicts.
func TestMergeDeletionConflicts(t *testing.T) {
	now := time.Now()
	baseTime := now.Add(-2 * time.Hour)

	t.Run("local delete remote unchanged", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-delete",
			Title:     "To Delete",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// local deleted (not present)
		local := []*types.Issue{}

		// remote unchanged
		remote := []*types.Issue{{
			ID:        "bd-delete",
			Title:     "To Delete",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		result := MergeIssues(base, local, remote)

		// local deletion should win (remote unchanged from base)
		if len(result.Merged) != 0 {
			t.Errorf("expected 0 merged issues (deleted), got %d", len(result.Merged))
		}
		if result.Strategy["bd-delete"] != StrategyLocal {
			t.Errorf("expected strategy %q for deletion, got %q",
				StrategyLocal, result.Strategy["bd-delete"])
		}
	})

	t.Run("local delete remote modified", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-delete",
			Title:     "Original",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// local deleted
		local := []*types.Issue{}

		// remote modified
		remote := []*types.Issue{{
			ID:        "bd-delete",
			Title:     "Remote Modified",
			Status:    types.StatusInProgress,
			UpdatedAt: now, // remote updated
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		result := MergeIssues(base, local, remote)

		// remote modification should win over local deletion
		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue (kept), got %d", len(result.Merged))
		}
		if result.Merged[0].Title != "Remote Modified" {
			t.Errorf("expected remote version, got %q", result.Merged[0].Title)
		}
		if result.Strategy["bd-delete"] != StrategyMerged {
			t.Errorf("expected strategy %q for conflict, got %q",
				StrategyMerged, result.Strategy["bd-delete"])
		}
	})

	t.Run("remote delete local modified", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-delete",
			Title:     "Original",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// local modified
		local := []*types.Issue{{
			ID:        "bd-delete",
			Title:     "Local Modified",
			Status:    types.StatusInProgress,
			UpdatedAt: now,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// remote deleted
		remote := []*types.Issue{}

		result := MergeIssues(base, local, remote)

		// local modification should win over remote deletion
		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue (kept), got %d", len(result.Merged))
		}
		if result.Merged[0].Title != "Local Modified" {
			t.Errorf("expected local version, got %q", result.Merged[0].Title)
		}
	})

	t.Run("both delete", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-delete",
			Title:     "To Delete",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// both deleted
		local := []*types.Issue{}
		remote := []*types.Issue{}

		result := MergeIssues(base, local, remote)

		// both deleted - should stay deleted
		if len(result.Merged) != 0 {
			t.Errorf("expected 0 merged issues, got %d", len(result.Merged))
		}
	})
}

// TestMergeWithFieldLevelMerging tests union merge for labels and dependencies.
func TestMergeWithFieldLevelMerging(t *testing.T) {
	now := time.Now()
	baseTime := now.Add(-2 * time.Hour)

	t.Run("labels union merge", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-labels",
			Title:     "Issue with Labels",
			Status:    types.StatusOpen,
			Labels:    []string{"original"},
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		local := []*types.Issue{{
			ID:        "bd-labels",
			Title:     "Issue with Labels - Local",
			Status:    types.StatusOpen,
			Labels:    []string{"original", "local-added"},
			UpdatedAt: now.Add(-time.Hour),
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		remote := []*types.Issue{{
			ID:        "bd-labels",
			Title:     "Issue with Labels - Remote",
			Status:    types.StatusOpen,
			Labels:    []string{"original", "remote-added"},
			UpdatedAt: now,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		result := MergeIssues(base, local, remote)

		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
		}

		merged := result.Merged[0]

		// labels should be union of both
		labelSet := make(map[string]bool)
		for _, label := range merged.Labels {
			labelSet[label] = true
		}

		expectedLabels := []string{"original", "local-added", "remote-added"}
		for _, expected := range expectedLabels {
			if !labelSet[expected] {
				t.Errorf("expected label %q in merged labels %v", expected, merged.Labels)
			}
		}
	})

	t.Run("dependencies union merge", func(t *testing.T) {
		localDep := &types.Dependency{
			IssueID:     "bd-deps",
			DependsOnID: "bd-local-target",
			Type:        types.DepBlocks,
			CreatedAt:   now,
		}
		remoteDep := &types.Dependency{
			IssueID:     "bd-deps",
			DependsOnID: "bd-remote-target",
			Type:        types.DepBlocks,
			CreatedAt:   now,
		}

		base := []*types.Issue{{
			ID:           "bd-deps",
			Title:        "Issue with Deps",
			Status:       types.StatusOpen,
			Dependencies: nil,
			UpdatedAt:    baseTime,
			CreatedAt:    baseTime.Add(-time.Hour),
		}}

		local := []*types.Issue{{
			ID:           "bd-deps",
			Title:        "Issue with Deps - Local",
			Status:       types.StatusOpen,
			Dependencies: []*types.Dependency{localDep},
			UpdatedAt:    now.Add(-time.Hour),
			CreatedAt:    baseTime.Add(-time.Hour),
		}}

		remote := []*types.Issue{{
			ID:           "bd-deps",
			Title:        "Issue with Deps - Remote",
			Status:       types.StatusOpen,
			Dependencies: []*types.Dependency{remoteDep},
			UpdatedAt:    now,
			CreatedAt:    baseTime.Add(-time.Hour),
		}}

		result := MergeIssues(base, local, remote)

		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
		}

		merged := result.Merged[0]

		// dependencies should be union of both
		if len(merged.Dependencies) != 2 {
			t.Errorf("expected 2 dependencies, got %d", len(merged.Dependencies))
		}
	})
}

// TestEndToEnd3WayMerge tests the full 3-way merge flow with database integration.
func TestEndToEnd3WayMerge(t *testing.T) {
	ctx := context.Background()
	tmpDir, cleanup := setupGitRepo(t)
	defer cleanup()

	// Setup beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	dbPath := filepath.Join(beadsDir, "beads.db")

	// Initial JSONL (base state)
	baseContent := `{"id":"bd-1","title":"Original","status":"open","priority":2,"issue_type":"task","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}
`
	if err := os.WriteFile(jsonlPath, []byte(baseContent), 0644); err != nil {
		t.Fatalf("write initial JSONL failed: %v", err)
	}

	// Commit initial state
	_ = exec.Command("git", "add", ".").Run()
	_ = exec.Command("git", "commit", "-m", "initial").Run()

	// Create database
	testStore, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()
	if err := testStore.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("set issue_prefix failed: %v", err)
	}

	// Load base state
	baseIssues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		t.Fatalf("loadIssuesFromJSONL failed: %v", err)
	}
	if len(baseIssues) != 1 {
		t.Fatalf("expected 1 base issue, got %d", len(baseIssues))
	}

	// Simulate local change (create issue in DB)
	localIssue := &types.Issue{
		ID:        "bd-1",
		Title:     "Local Edit",
		Status:    types.StatusInProgress,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	localIssues := []*types.Issue{localIssue}

	// Simulate remote change (modify JSONL)
	remoteContent := `{"id":"bd-1","title":"Remote Edit","status":"closed","priority":0,"issue_type":"task","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-03T00:00:00Z"}
`
	if err := os.WriteFile(jsonlPath, []byte(remoteContent), 0644); err != nil {
		t.Fatalf("write remote JSONL failed: %v", err)
	}

	// Load remote state
	remoteIssues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		t.Fatalf("loadIssuesFromJSONL (remote) failed: %v", err)
	}

	// Perform 3-way merge
	result := MergeIssues(baseIssues, localIssues, remoteIssues)

	// Verify merge result
	if len(result.Merged) != 1 {
		t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
	}

	// Remote should win (newer timestamp)
	merged := result.Merged[0]
	if merged.Title != "Remote Edit" {
		t.Errorf("expected remote title (newer), got %q", merged.Title)
	}
	if merged.Status != types.StatusClosed {
		t.Errorf("expected remote status (newer), got %s", merged.Status)
	}

	// Verify merge was detected as conflict
	if result.Conflicts != 1 {
		t.Errorf("expected 1 conflict, got %d", result.Conflicts)
	}
	if result.Strategy["bd-1"] != StrategyMerged {
		t.Errorf("expected strategy %q, got %q", StrategyMerged, result.Strategy["bd-1"])
	}
}

// TestMergeWithTombstones tests merge behavior with tombstone records.
func TestMergeWithTombstones(t *testing.T) {
	now := time.Now()
	baseTime := now.Add(-2 * time.Hour)
	deletedAt := now.Add(-time.Hour)

	t.Run("tombstone in local state", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-tomb",
			Title:     "Original",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// local has tombstone
		local := []*types.Issue{{
			ID:           "bd-tomb",
			Title:        "Original",
			Status:       types.StatusOpen,
			UpdatedAt:    deletedAt,
			CreatedAt:    baseTime.Add(-time.Hour),
			DeletedAt:    &deletedAt,
			DeletedBy:    "local-user",
			DeleteReason: "resolved",
		}}

		// remote unchanged
		remote := []*types.Issue{{
			ID:        "bd-tomb",
			Title:     "Original",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		result := MergeIssues(base, local, remote)

		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
		}

		// tombstone should be preserved (local changed, remote unchanged)
		if result.Merged[0].DeletedAt == nil {
			t.Error("expected tombstone to be preserved")
		}
	})

	t.Run("tombstone in remote state", func(t *testing.T) {
		base := []*types.Issue{{
			ID:        "bd-tomb",
			Title:     "Original",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// local unchanged
		local := []*types.Issue{{
			ID:        "bd-tomb",
			Title:     "Original",
			Status:    types.StatusOpen,
			UpdatedAt: baseTime,
			CreatedAt: baseTime.Add(-time.Hour),
		}}

		// remote has tombstone
		remote := []*types.Issue{{
			ID:           "bd-tomb",
			Title:        "Original",
			Status:       types.StatusOpen,
			UpdatedAt:    deletedAt,
			CreatedAt:    baseTime.Add(-time.Hour),
			DeletedAt:    &deletedAt,
			DeletedBy:    "remote-user",
			DeleteReason: "duplicate",
		}}

		result := MergeIssues(base, local, remote)

		if len(result.Merged) != 1 {
			t.Fatalf("expected 1 merged issue, got %d", len(result.Merged))
		}

		// tombstone should be preserved (remote changed, local unchanged)
		if result.Merged[0].DeletedAt == nil {
			t.Error("expected tombstone to be preserved from remote")
		}
	})
}

// TestMergeOrderDeterminism verifies merge produces deterministic output order.
func TestMergeOrderDeterminism(t *testing.T) {
	now := time.Now()

	// Create issues in random order
	issues := []*types.Issue{
		{ID: "bd-z", Title: "Z", UpdatedAt: now, CreatedAt: now},
		{ID: "bd-a", Title: "A", UpdatedAt: now, CreatedAt: now},
		{ID: "bd-m", Title: "M", UpdatedAt: now, CreatedAt: now},
	}

	// Run merge multiple times and verify order is consistent
	var lastOrder []string
	for i := 0; i < 5; i++ {
		result := MergeIssues(nil, issues, nil)

		var order []string
		for _, issue := range result.Merged {
			order = append(order, issue.ID)
		}

		if lastOrder != nil {
			for j := range order {
				if order[j] != lastOrder[j] {
					t.Errorf("merge order inconsistent: run %d has %v, previous had %v",
						i, order, lastOrder)
					break
				}
			}
		}
		lastOrder = order
	}

	// Verify order is sorted (implementation detail but important for reproducibility)
	for i := 1; i < len(lastOrder); i++ {
		if lastOrder[i] < lastOrder[i-1] {
			t.Errorf("merge order not sorted: %v", lastOrder)
			break
		}
	}
}
