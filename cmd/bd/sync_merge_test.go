package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// setupTestStore creates a test storage with issue_prefix configured
func setupTestStore(t *testing.T, dbPath string) *sqlite.SQLiteStorage {
	t.Helper()
	
	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	
	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}
	
	return store
}

// TestDBNeedsExport_InSync verifies dbNeedsExport returns false when DB and JSONL are in sync
func TestDBNeedsExport_InSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "beads.db")
	jsonlPath := filepath.Join(tmpDir, "issues.jsonl")

	store := setupTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Create an issue in DB
	issue := &types.Issue{
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeBug,
	}
	err := store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Export to JSONL
	if err := exportToJSONLWithStore(ctx, store, jsonlPath); err != nil {
		t.Fatalf("Failed to export: %v", err)
	}

	// Wait a moment to ensure DB mtime isn't newer
	time.Sleep(10 * time.Millisecond)

	// Touch JSONL to make it newer than DB
	now := time.Now()
	if err := os.Chtimes(jsonlPath, now, now); err != nil {
		t.Fatalf("Failed to touch JSONL: %v", err)
	}

	// DB and JSONL should be in sync
	needsExport, err := dbNeedsExport(ctx, store, jsonlPath)
	if err != nil {
		t.Fatalf("dbNeedsExport failed: %v", err)
	}

	if needsExport {
		t.Errorf("Expected needsExport=false (DB and JSONL in sync), got true")
	}
}

// TestDBNeedsExport_DBNewer verifies dbNeedsExport returns true when DB is modified
func TestDBNeedsExport_DBNewer(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "beads.db")
	jsonlPath := filepath.Join(tmpDir, "issues.jsonl")

	store := setupTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Create and export issue
	issue1 := &types.Issue{
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeBug,
	}
	err := store.CreateIssue(ctx, issue1, "test-user")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	if err := exportToJSONLWithStore(ctx, store, jsonlPath); err != nil {
		t.Fatalf("Failed to export: %v", err)
	}

	// Wait and modify DB
	time.Sleep(10 * time.Millisecond)
	issue2 := &types.Issue{
		Title:     "Another Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue2, "test-user")
	if err != nil {
		t.Fatalf("Failed to create second issue: %v", err)
	}

	// DB is newer, should need export
	needsExport, err := dbNeedsExport(ctx, store, jsonlPath)
	if err != nil {
		t.Fatalf("dbNeedsExport failed: %v", err)
	}

	if !needsExport {
		t.Errorf("Expected needsExport=true (DB modified), got false")
	}
}

// TestDBNeedsExport_CountMismatch verifies dbNeedsExport returns true when counts differ
func TestDBNeedsExport_CountMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "beads.db")
	jsonlPath := filepath.Join(tmpDir, "issues.jsonl")

	store := setupTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Create and export issue
	issue1 := &types.Issue{
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeBug,
	}
	err := store.CreateIssue(ctx, issue1, "test-user")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	if err := exportToJSONLWithStore(ctx, store, jsonlPath); err != nil {
		t.Fatalf("Failed to export: %v", err)
	}

	// Add another issue to DB but don't export
	issue2 := &types.Issue{
		Title:     "Another Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue2, "test-user")
	if err != nil {
		t.Fatalf("Failed to create second issue: %v", err)
	}

	// Make JSONL appear newer (but counts differ)
	time.Sleep(10 * time.Millisecond)
	now := time.Now().Add(1 * time.Hour) // Way in the future
	if err := os.Chtimes(jsonlPath, now, now); err != nil {
		t.Fatalf("Failed to touch JSONL: %v", err)
	}

	// Counts mismatch, should need export
	needsExport, err := dbNeedsExport(ctx, store, jsonlPath)
	if err != nil {
		t.Fatalf("dbNeedsExport failed: %v", err)
	}

	if !needsExport {
		t.Errorf("Expected needsExport=true (count mismatch), got false")
	}
}

// TestDBNeedsExport_NoJSONL verifies dbNeedsExport returns true when JSONL doesn't exist
func TestDBNeedsExport_NoJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "beads.db")
	jsonlPath := filepath.Join(tmpDir, "issues.jsonl")

	store := setupTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Create issue but don't export
	issue := &types.Issue{
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeBug,
	}
	err := store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// JSONL doesn't exist, should need export
	needsExport, err := dbNeedsExport(ctx, store, jsonlPath)
	if err != nil {
		t.Fatalf("dbNeedsExport failed: %v", err)
	}

	if !needsExport {
		t.Fatalf("Expected needsExport=true (JSONL missing), got false")
	}
}

// =============================================================================
// 3-Way Merge Tests (Phase 2)
// =============================================================================

// makeTestIssue creates a test issue with specified fields
func makeTestIssue(id, title string, status types.Status, priority int, updatedAt time.Time) *types.Issue {
	return &types.Issue{
		ID:        id,
		Title:     title,
		Status:    status,
		Priority:  priority,
		IssueType: types.TypeTask,
		UpdatedAt: updatedAt,
		CreatedAt: updatedAt.Add(-time.Hour), // Created 1 hour before update
	}
}

// TestMergeIssue_NoBase_LocalOnly tests first sync with only local issue
func TestMergeIssue_NoBase_LocalOnly(t *testing.T) {
	local := makeTestIssue("bd-1234", "Local Issue", types.StatusOpen, 1, time.Now())

	merged, strategy := MergeIssue(nil, local, nil)

	if strategy != StrategyLocal {
		t.Errorf("Expected strategy=%s, got %s", StrategyLocal, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	if merged.ID != "bd-1234" {
		t.Errorf("Expected ID=bd-1234, got %s", merged.ID)
	}
}

// TestMergeIssue_NoBase_RemoteOnly tests first sync with only remote issue
func TestMergeIssue_NoBase_RemoteOnly(t *testing.T) {
	remote := makeTestIssue("bd-5678", "Remote Issue", types.StatusOpen, 2, time.Now())

	merged, strategy := MergeIssue(nil, nil, remote)

	if strategy != StrategyRemote {
		t.Errorf("Expected strategy=%s, got %s", StrategyRemote, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	if merged.ID != "bd-5678" {
		t.Errorf("Expected ID=bd-5678, got %s", merged.ID)
	}
}

// TestMergeIssue_NoBase_BothExist_LocalNewer tests first sync where both have same issue, local is newer
func TestMergeIssue_NoBase_BothExist_LocalNewer(t *testing.T) {
	now := time.Now()
	local := makeTestIssue("bd-1234", "Local Title", types.StatusOpen, 1, now.Add(time.Hour))
	remote := makeTestIssue("bd-1234", "Remote Title", types.StatusOpen, 2, now)

	merged, strategy := MergeIssue(nil, local, remote)

	if strategy != StrategyMerged {
		t.Errorf("Expected strategy=%s, got %s", StrategyMerged, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	if merged.Title != "Local Title" {
		t.Errorf("Expected local title (newer), got %s", merged.Title)
	}
}

// TestMergeIssue_NoBase_BothExist_RemoteNewer tests first sync where both have same issue, remote is newer
func TestMergeIssue_NoBase_BothExist_RemoteNewer(t *testing.T) {
	now := time.Now()
	local := makeTestIssue("bd-1234", "Local Title", types.StatusOpen, 1, now)
	remote := makeTestIssue("bd-1234", "Remote Title", types.StatusOpen, 2, now.Add(time.Hour))

	merged, strategy := MergeIssue(nil, local, remote)

	if strategy != StrategyMerged {
		t.Errorf("Expected strategy=%s, got %s", StrategyMerged, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	if merged.Title != "Remote Title" {
		t.Errorf("Expected remote title (newer), got %s", merged.Title)
	}
}

// TestMergeIssue_NoBase_BothExist_SameTime tests first sync where both have same timestamp (remote wins)
func TestMergeIssue_NoBase_BothExist_SameTime(t *testing.T) {
	now := time.Now()
	local := makeTestIssue("bd-1234", "Local Title", types.StatusOpen, 1, now)
	remote := makeTestIssue("bd-1234", "Remote Title", types.StatusOpen, 2, now)

	merged, strategy := MergeIssue(nil, local, remote)

	if strategy != StrategyMerged {
		t.Errorf("Expected strategy=%s, got %s", StrategyMerged, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	// Remote wins on tie (per design.md Decision 3)
	if merged.Title != "Remote Title" {
		t.Errorf("Expected remote title (tie goes to remote), got %s", merged.Title)
	}
}

// TestMergeIssue_NoChanges tests 3-way merge with no changes anywhere
func TestMergeIssue_NoChanges(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Same Title", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "Same Title", types.StatusOpen, 1, now)
	remote := makeTestIssue("bd-1234", "Same Title", types.StatusOpen, 1, now)

	merged, strategy := MergeIssue(base, local, remote)

	if strategy != StrategySame {
		t.Errorf("Expected strategy=%s, got %s", StrategySame, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
}

// TestMergeIssue_OnlyLocalChanged tests 3-way merge where only local changed
func TestMergeIssue_OnlyLocalChanged(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Original Title", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "Updated Title", types.StatusOpen, 1, now.Add(time.Hour))
	remote := makeTestIssue("bd-1234", "Original Title", types.StatusOpen, 1, now)

	merged, strategy := MergeIssue(base, local, remote)

	if strategy != StrategyLocal {
		t.Errorf("Expected strategy=%s, got %s", StrategyLocal, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	if merged.Title != "Updated Title" {
		t.Errorf("Expected updated title, got %s", merged.Title)
	}
}

// TestMergeIssue_OnlyRemoteChanged tests 3-way merge where only remote changed
func TestMergeIssue_OnlyRemoteChanged(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Original Title", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "Original Title", types.StatusOpen, 1, now)
	remote := makeTestIssue("bd-1234", "Updated Title", types.StatusOpen, 1, now.Add(time.Hour))

	merged, strategy := MergeIssue(base, local, remote)

	if strategy != StrategyRemote {
		t.Errorf("Expected strategy=%s, got %s", StrategyRemote, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	if merged.Title != "Updated Title" {
		t.Errorf("Expected updated title, got %s", merged.Title)
	}
}

// TestMergeIssue_BothMadeSameChange tests 3-way merge where both made identical change
func TestMergeIssue_BothMadeSameChange(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Original Title", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "Same Update", types.StatusClosed, 2, now.Add(time.Hour))
	remote := makeTestIssue("bd-1234", "Same Update", types.StatusClosed, 2, now.Add(time.Hour))

	merged, strategy := MergeIssue(base, local, remote)

	if strategy != StrategySame {
		t.Errorf("Expected strategy=%s, got %s", StrategySame, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	if merged.Title != "Same Update" {
		t.Errorf("Expected 'Same Update', got %s", merged.Title)
	}
}

// TestMergeIssue_TrueConflict_LocalNewer tests true conflict where local is newer
func TestMergeIssue_TrueConflict_LocalNewer(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Original", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "Local Update", types.StatusInProgress, 1, now.Add(2*time.Hour))
	remote := makeTestIssue("bd-1234", "Remote Update", types.StatusClosed, 2, now.Add(time.Hour))

	merged, strategy := MergeIssue(base, local, remote)

	if strategy != StrategyMerged {
		t.Errorf("Expected strategy=%s, got %s", StrategyMerged, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	// Local is newer, should win
	if merged.Title != "Local Update" {
		t.Errorf("Expected local title (newer), got %s", merged.Title)
	}
	if merged.Status != types.StatusInProgress {
		t.Errorf("Expected local status, got %s", merged.Status)
	}
}

// TestMergeIssue_TrueConflict_RemoteNewer tests true conflict where remote is newer
func TestMergeIssue_TrueConflict_RemoteNewer(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Original", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "Local Update", types.StatusInProgress, 1, now.Add(time.Hour))
	remote := makeTestIssue("bd-1234", "Remote Update", types.StatusClosed, 2, now.Add(2*time.Hour))

	merged, strategy := MergeIssue(base, local, remote)

	if strategy != StrategyMerged {
		t.Errorf("Expected strategy=%s, got %s", StrategyMerged, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue, got nil")
	}
	// Remote is newer, should win
	if merged.Title != "Remote Update" {
		t.Errorf("Expected remote title (newer), got %s", merged.Title)
	}
	if merged.Status != types.StatusClosed {
		t.Errorf("Expected remote status, got %s", merged.Status)
	}
}

// TestMergeIssue_LocalDeleted_RemoteUnchanged tests local deletion when remote unchanged
func TestMergeIssue_LocalDeleted_RemoteUnchanged(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "To Delete", types.StatusOpen, 1, now)
	remote := makeTestIssue("bd-1234", "To Delete", types.StatusOpen, 1, now)

	merged, strategy := MergeIssue(base, nil, remote)

	if strategy != StrategyLocal {
		t.Errorf("Expected strategy=%s (honor local deletion), got %s", StrategyLocal, strategy)
	}
	if merged != nil {
		t.Errorf("Expected nil (deleted), got issue %s", merged.ID)
	}
}

// TestMergeIssue_LocalDeleted_RemoteChanged tests local deletion but remote changed
func TestMergeIssue_LocalDeleted_RemoteChanged(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Original", types.StatusOpen, 1, now)
	remote := makeTestIssue("bd-1234", "Remote Updated", types.StatusClosed, 2, now.Add(time.Hour))

	merged, strategy := MergeIssue(base, nil, remote)

	if strategy != StrategyMerged {
		t.Errorf("Expected strategy=%s (conflict: deleted vs updated), got %s", StrategyMerged, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue (remote changed), got nil")
	}
	if merged.Title != "Remote Updated" {
		t.Errorf("Expected remote title (changed wins over delete), got %s", merged.Title)
	}
}

// TestMergeIssue_RemoteDeleted_LocalUnchanged tests remote deletion when local unchanged
func TestMergeIssue_RemoteDeleted_LocalUnchanged(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "To Delete", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "To Delete", types.StatusOpen, 1, now)

	merged, strategy := MergeIssue(base, local, nil)

	if strategy != StrategyRemote {
		t.Errorf("Expected strategy=%s (honor remote deletion), got %s", StrategyRemote, strategy)
	}
	if merged != nil {
		t.Errorf("Expected nil (deleted), got issue %s", merged.ID)
	}
}

// TestMergeIssue_RemoteDeleted_LocalChanged tests remote deletion but local changed
func TestMergeIssue_RemoteDeleted_LocalChanged(t *testing.T) {
	now := time.Now()
	base := makeTestIssue("bd-1234", "Original", types.StatusOpen, 1, now)
	local := makeTestIssue("bd-1234", "Local Updated", types.StatusClosed, 2, now.Add(time.Hour))

	merged, strategy := MergeIssue(base, local, nil)

	if strategy != StrategyMerged {
		t.Errorf("Expected strategy=%s (conflict: updated vs deleted), got %s", StrategyMerged, strategy)
	}
	if merged == nil {
		t.Fatal("Expected merged issue (local changed), got nil")
	}
	if merged.Title != "Local Updated" {
		t.Errorf("Expected local title (changed wins over delete), got %s", merged.Title)
	}
}

// TestMergeIssues_Empty tests merging empty sets
func TestMergeIssues_Empty(t *testing.T) {
	result, err := MergeIssues(nil, nil, nil)
	if err != nil {
		t.Fatalf("MergeIssues failed: %v", err)
	}
	if len(result.Merged) != 0 {
		t.Errorf("Expected 0 merged issues, got %d", len(result.Merged))
	}
	if result.Conflicts != 0 {
		t.Errorf("Expected 0 conflicts, got %d", result.Conflicts)
	}
}

// TestMergeIssues_MultipleIssues tests merging multiple issues with different scenarios
func TestMergeIssues_MultipleIssues(t *testing.T) {
	now := time.Now()

	// Base state
	base := []*types.Issue{
		makeTestIssue("bd-0001", "Unchanged", types.StatusOpen, 1, now),
		makeTestIssue("bd-0002", "Will change locally", types.StatusOpen, 1, now),
		makeTestIssue("bd-0003", "Will change remotely", types.StatusOpen, 1, now),
		makeTestIssue("bd-0004", "To delete locally", types.StatusOpen, 1, now),
	}

	// Local state
	local := []*types.Issue{
		makeTestIssue("bd-0001", "Unchanged", types.StatusOpen, 1, now),
		makeTestIssue("bd-0002", "Changed locally", types.StatusInProgress, 1, now.Add(time.Hour)),
		makeTestIssue("bd-0003", "Will change remotely", types.StatusOpen, 1, now),
		// bd-0004 deleted locally
		makeTestIssue("bd-0005", "New local issue", types.StatusOpen, 1, now), // New issue
	}

	// Remote state
	remote := []*types.Issue{
		makeTestIssue("bd-0001", "Unchanged", types.StatusOpen, 1, now),
		makeTestIssue("bd-0002", "Will change locally", types.StatusOpen, 1, now),
		makeTestIssue("bd-0003", "Changed remotely", types.StatusClosed, 2, now.Add(time.Hour)),
		makeTestIssue("bd-0004", "To delete locally", types.StatusOpen, 1, now), // Unchanged from base
		makeTestIssue("bd-0006", "New remote issue", types.StatusOpen, 1, now),  // New issue
	}

	result, err := MergeIssues(base, local, remote)
	if err != nil {
		t.Fatalf("MergeIssues failed: %v", err)
	}

	// Should have 5 issues:
	// - bd-0001: same
	// - bd-0002: local changed
	// - bd-0003: remote changed
	// - bd-0004: deleted (not in merged)
	// - bd-0005: new local
	// - bd-0006: new remote
	if len(result.Merged) != 5 {
		t.Errorf("Expected 5 merged issues, got %d", len(result.Merged))
	}

	// Verify strategies
	expectedStrategies := map[string]string{
		"bd-0001": StrategySame,
		"bd-0002": StrategyLocal,
		"bd-0003": StrategyRemote,
		"bd-0004": StrategyLocal, // Deleted locally
		"bd-0005": StrategyLocal,
		"bd-0006": StrategyRemote,
	}

	for id, expected := range expectedStrategies {
		if got := result.Strategy[id]; got != expected {
			t.Errorf("Issue %s: expected strategy=%s, got %s", id, expected, got)
		}
	}

	// Verify bd-0004 is not in merged (deleted)
	for _, issue := range result.Merged {
		if issue.ID == "bd-0004" {
			t.Errorf("bd-0004 should be deleted, but found in merged")
		}
	}
}

// TestBaseState_LoadSave tests loading and saving base state
func TestBaseState_LoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now().Truncate(time.Second) // Truncate for JSON round-trip

	issues := []*types.Issue{
		{
			ID:        "bd-0001",
			Title:     "Test Issue 1",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
			UpdatedAt: now,
			CreatedAt: now.Add(-time.Hour),
		},
		{
			ID:        "bd-0002",
			Title:     "Test Issue 2",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeBug,
			UpdatedAt: now,
			CreatedAt: now.Add(-time.Hour),
		},
	}

	// Save base state
	if err := saveBaseState(tmpDir, issues); err != nil {
		t.Fatalf("saveBaseState failed: %v", err)
	}

	// Verify file exists
	baseStatePath := filepath.Join(tmpDir, syncBaseFileName)
	if _, err := os.Stat(baseStatePath); os.IsNotExist(err) {
		t.Fatalf("Base state file not created")
	}

	// Load base state
	loaded, err := loadBaseState(tmpDir)
	if err != nil {
		t.Fatalf("loadBaseState failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("Expected 2 issues, got %d", len(loaded))
	}

	// Verify issue content
	if loaded[0].ID != "bd-0001" || loaded[0].Title != "Test Issue 1" {
		t.Errorf("First issue mismatch: got ID=%s, Title=%s", loaded[0].ID, loaded[0].Title)
	}
	if loaded[1].ID != "bd-0002" || loaded[1].Title != "Test Issue 2" {
		t.Errorf("Second issue mismatch: got ID=%s, Title=%s", loaded[1].ID, loaded[1].Title)
	}
}

// TestBaseState_LoadMissing tests loading when no base state exists
func TestBaseState_LoadMissing(t *testing.T) {
	tmpDir := t.TempDir()

	loaded, err := loadBaseState(tmpDir)
	if err != nil {
		t.Fatalf("loadBaseState failed: %v", err)
	}

	if loaded != nil {
		t.Errorf("Expected nil for missing base state, got %d issues", len(loaded))
	}
}

// TestIssueEqual tests the issueEqual helper function
func TestIssueEqual(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		a, b     *types.Issue
		expected bool
	}{
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "a nil",
			a:        nil,
			b:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			expected: false,
		},
		{
			name:     "b nil",
			a:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			b:        nil,
			expected: false,
		},
		{
			name:     "identical",
			a:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			b:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			expected: true,
		},
		{
			name:     "different ID",
			a:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			b:        makeTestIssue("bd-5678", "Test", types.StatusOpen, 1, now),
			expected: false,
		},
		{
			name:     "different title",
			a:        makeTestIssue("bd-1234", "Test A", types.StatusOpen, 1, now),
			b:        makeTestIssue("bd-1234", "Test B", types.StatusOpen, 1, now),
			expected: false,
		},
		{
			name:     "different status",
			a:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			b:        makeTestIssue("bd-1234", "Test", types.StatusClosed, 1, now),
			expected: false,
		},
		{
			name:     "different priority",
			a:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			b:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 2, now),
			expected: false,
		},
		{
			name:     "different updated_at",
			a:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now),
			b:        makeTestIssue("bd-1234", "Test", types.StatusOpen, 1, now.Add(time.Hour)),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := issueEqual(tc.a, tc.b)
			if result != tc.expected {
				t.Errorf("issueEqual returned %v, expected %v", result, tc.expected)
			}
		})
	}
}
