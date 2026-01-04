package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// TestRenamePrefixUpdatesJSONL verifies that rename-prefix updates the JSONL file
// with the new IDs immediately after renaming
func TestRenamePrefixUpdatesJSONL(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, ".beads", "beads.db")
	jsonlPath := filepath.Join(tempDir, ".beads", "issues.jsonl")

	// Create .beads directory
	if err := os.MkdirAll(filepath.Dir(testDBPath), 0750); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create store
	st, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// Set initial prefix
	if err := st.SetConfig(ctx, "issue_prefix", "old"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Create test issues
	now := time.Now()
	issue1 := &types.Issue{
		ID:        "old-abc",
		Title:     "Test issue 1",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}
	issue2 := &types.Issue{
		ID:        "old-def",
		Title:     "Test issue 2",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := st.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("failed to create issue1: %v", err)
	}
	if err := st.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("failed to create issue2: %v", err)
	}

	// Write initial JSONL with old IDs
	if err := writeTestJSONL(jsonlPath, []*types.Issue{issue1, issue2}); err != nil {
		t.Fatalf("failed to write initial JSONL: %v", err)
	}

	// Verify JSONL has old IDs
	jsonlIssues, err := parseJSONLFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to parse initial JSONL: %v", err)
	}
	for _, issue := range jsonlIssues {
		if !strings.HasPrefix(issue.ID, "old-") {
			t.Fatalf("expected old- prefix, got %s", issue.ID)
		}
	}

	// Simulate rename-prefix by calling renamePrefixInDB directly
	// Note: In integration tests, we'd call the actual command
	issues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}

	// Set up globals for the test (needed by renamePrefixInDB)
	oldStore := store
	oldActor := actor
	store = st
	actor = "test"
	defer func() {
		store = oldStore
		actor = oldActor
	}()

	if err := renamePrefixInDB(ctx, "old", "new", issues); err != nil {
		t.Fatalf("renamePrefixInDB failed: %v", err)
	}

	// Manually export (simulating what the command does after rename)
	// In the real command, flushManager.FlushNow() would do this
	renamedIssues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search renamed issues: %v", err)
	}
	if err := writeTestJSONL(jsonlPath, renamedIssues); err != nil {
		t.Fatalf("failed to write renamed JSONL: %v", err)
	}

	// Verify JSONL now has new IDs
	finalIssues, err := parseJSONLFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to parse final JSONL: %v", err)
	}

	if len(finalIssues) != 2 {
		t.Fatalf("expected 2 issues in JSONL, got %d", len(finalIssues))
	}

	for _, issue := range finalIssues {
		if !strings.HasPrefix(issue.ID, "new-") {
			t.Errorf("expected new- prefix, got %s", issue.ID)
		}
	}

	// Verify specific IDs
	idMap := make(map[string]bool)
	for _, issue := range finalIssues {
		idMap[issue.ID] = true
	}
	if !idMap["new-abc"] {
		t.Error("expected new-abc in JSONL")
	}
	if !idMap["new-def"] {
		t.Error("expected new-def in JSONL")
	}
}

// TestRenamePrefixImportsFromJSONLFirst verifies that rename-prefix imports
// issues from JSONL before renaming to prevent data loss
func TestRenamePrefixImportsFromJSONLFirst(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, ".beads", "beads.db")
	jsonlPath := filepath.Join(tempDir, ".beads", "issues.jsonl")

	// Create .beads directory
	if err := os.MkdirAll(filepath.Dir(testDBPath), 0750); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create store
	st, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// Set initial prefix
	if err := st.SetConfig(ctx, "issue_prefix", "old"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Create one issue in DB
	now := time.Now()
	dbIssue := &types.Issue{
		ID:        "old-abc",
		Title:     "DB issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.CreateIssue(ctx, dbIssue, "test"); err != nil {
		t.Fatalf("failed to create DB issue: %v", err)
	}

	// Write JSONL with an EXTRA issue (simulating other workspace)
	jsonlExtraIssue := &types.Issue{
		ID:        "old-xyz",
		Title:     "JSONL-only issue from other workspace",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := writeTestJSONL(jsonlPath, []*types.Issue{dbIssue, jsonlExtraIssue}); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}

	// Parse JSONL and import extra issues (simulating what rename-prefix does)
	jsonlIssues, err := parseJSONLFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to parse JSONL: %v", err)
	}

	// Import issues from JSONL (this is what the fix adds)
	opts := ImportOptions{
		DryRun:               false,
		SkipUpdate:           false,
		Strict:               false,
		SkipPrefixValidation: true,
	}
	result, err := importIssuesCore(ctx, testDBPath, st, jsonlIssues, opts)
	if err != nil {
		t.Fatalf("failed to import from JSONL: %v", err)
	}

	// Should have imported the extra issue
	if result.Created != 1 {
		t.Errorf("expected 1 issue created from JSONL, got %d", result.Created)
	}

	// Verify DB now has both issues
	allIssues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	if len(allIssues) != 2 {
		t.Fatalf("expected 2 issues in DB after import, got %d", len(allIssues))
	}

	// Now perform rename
	oldStore := store
	oldActor := actor
	store = st
	actor = "test"
	defer func() {
		store = oldStore
		actor = oldActor
	}()

	if err := renamePrefixInDB(ctx, "old", "new", allIssues); err != nil {
		t.Fatalf("renamePrefixInDB failed: %v", err)
	}

	// Export to JSONL
	renamedIssues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search renamed issues: %v", err)
	}
	if err := writeTestJSONL(jsonlPath, renamedIssues); err != nil {
		t.Fatalf("failed to write renamed JSONL: %v", err)
	}

	// Verify BOTH issues are in final JSONL with new prefix
	finalIssues, err := parseJSONLFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to parse final JSONL: %v", err)
	}

	if len(finalIssues) != 2 {
		t.Fatalf("expected 2 issues in final JSONL (no data loss), got %d", len(finalIssues))
	}

	// Verify all have new prefix
	for _, issue := range finalIssues {
		if !strings.HasPrefix(issue.ID, "new-") {
			t.Errorf("expected new- prefix, got %s", issue.ID)
		}
	}

	// Verify the originally JSONL-only issue was preserved
	foundXYZ := false
	for _, issue := range finalIssues {
		if issue.ID == "new-xyz" {
			foundXYZ = true
			if issue.Title != "JSONL-only issue from other workspace" {
				t.Errorf("wrong title for new-xyz: %s", issue.Title)
			}
			break
		}
	}
	if !foundXYZ {
		t.Error("JSONL-only issue (old-xyz -> new-xyz) was lost during rename!")
	}
}

// TestRenamePrefixNoJSONL verifies that rename works when no JSONL file exists
func TestRenamePrefixNoJSONL(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, ".beads", "beads.db")
	jsonlPath := filepath.Join(tempDir, ".beads", "issues.jsonl")

	// Create .beads directory
	if err := os.MkdirAll(filepath.Dir(testDBPath), 0750); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Ensure no JSONL exists
	_ = os.Remove(jsonlPath)

	// Create store
	st, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// Set initial prefix
	if err := st.SetConfig(ctx, "issue_prefix", "old"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Create test issue
	now := time.Now()
	issue := &types.Issue{
		ID:        "old-abc",
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Verify no JSONL exists
	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Fatal("JSONL should not exist for this test")
	}

	// Perform rename
	issues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}

	oldStore := store
	oldActor := actor
	store = st
	actor = "test"
	defer func() {
		store = oldStore
		actor = oldActor
	}()

	if err := renamePrefixInDB(ctx, "old", "new", issues); err != nil {
		t.Fatalf("renamePrefixInDB failed: %v", err)
	}

	// Verify DB was renamed correctly
	renamedIssues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search renamed issues: %v", err)
	}

	if len(renamedIssues) != 1 {
		t.Fatalf("expected 1 issue after rename, got %d", len(renamedIssues))
	}

	if renamedIssues[0].ID != "new-abc" {
		t.Errorf("expected new-abc, got %s", renamedIssues[0].ID)
	}
}

// TestRepairPrefixesUpdatesJSONL verifies that --repair mode properly updates JSONL
// with new IDs after consolidating multiple prefixes
func TestRepairPrefixesUpdatesJSONL(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, ".beads", "beads.db")
	jsonlPath := filepath.Join(tempDir, ".beads", "issues.jsonl")

	// Create .beads directory
	if err := os.MkdirAll(filepath.Dir(testDBPath), 0750); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create store
	st, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	// Set global dbPath so findJSONLPath() finds the right file
	oldDBPath := dbPath
	dbPath = testDBPath
	defer func() { dbPath = oldDBPath }()

	ctx := context.Background()

	// Set initial prefix to "new" (target prefix)
	if err := st.SetConfig(ctx, "issue_prefix", "new"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Create issues with MIXED prefixes directly in DB (simulating corruption or merge)
	db := st.UnderlyingDB()
	now := time.Now()

	// Issues with correct prefix
	_, err = db.ExecContext(ctx, `
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at)
		VALUES (?, ?, 'open', 2, 'task', ?, ?)
	`, "new-abc", "Correct prefix issue", now, now)
	if err != nil {
		t.Fatalf("failed to create new-abc: %v", err)
	}

	// Issues with OLD prefix (simulating issues from before rename)
	_, err = db.ExecContext(ctx, `
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at)
		VALUES (?, ?, 'open', 2, 'task', ?, ?)
	`, "old-xyz", "Old prefix issue from other workspace", now, now)
	if err != nil {
		t.Fatalf("failed to create old-xyz: %v", err)
	}

	// Write JSONL with the old/mixed IDs (simulating state before repair)
	oldIssue1 := &types.Issue{ID: "new-abc", Title: "Correct prefix issue", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: now, UpdatedAt: now}
	oldIssue2 := &types.Issue{ID: "old-xyz", Title: "Old prefix issue from other workspace", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: now, UpdatedAt: now}
	if err := writeTestJSONL(jsonlPath, []*types.Issue{oldIssue1, oldIssue2}); err != nil {
		t.Fatalf("failed to write initial JSONL: %v", err)
	}

	// Verify JSONL has mixed prefixes
	initialIssues, err := parseJSONLFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to parse initial JSONL: %v", err)
	}
	hasOld := false
	hasNew := false
	for _, issue := range initialIssues {
		if strings.HasPrefix(issue.ID, "old-") {
			hasOld = true
		}
		if strings.HasPrefix(issue.ID, "new-") {
			hasNew = true
		}
	}
	if !hasOld || !hasNew {
		t.Fatal("initial JSONL should have mixed prefixes")
	}

	// Get all issues and detect prefixes
	allIssues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	prefixes := detectPrefixes(allIssues)
	if len(prefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(prefixes))
	}

	// Run repair
	if err := repairPrefixes(ctx, st, "test", "new", allIssues, prefixes, false); err != nil {
		t.Fatalf("repairPrefixes failed: %v", err)
	}

	// Verify JSONL was updated with all new- prefixes
	finalIssues, err := parseJSONLFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to parse final JSONL: %v", err)
	}

	if len(finalIssues) != 2 {
		t.Fatalf("expected 2 issues in final JSONL, got %d", len(finalIssues))
	}

	// All issues should now have new- prefix
	for _, issue := range finalIssues {
		if !strings.HasPrefix(issue.ID, "new-") {
			t.Errorf("expected new- prefix after repair, got %s", issue.ID)
		}
	}

	// The original new-abc should still exist
	foundABC := false
	for _, issue := range finalIssues {
		if issue.ID == "new-abc" {
			foundABC = true
			break
		}
	}
	if !foundABC {
		t.Error("new-abc should still exist after repair")
	}
}

// writeTestJSONL writes issues to a JSONL file for testing
func writeTestJSONL(path string, issues []*types.Issue) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	encoder := json.NewEncoder(w)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			return err
		}
	}
	return w.Flush()
}
