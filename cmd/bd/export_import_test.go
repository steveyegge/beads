package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// exportImportHelper provides test setup and assertion methods
type exportImportHelper struct {
	t     *testing.T
	ctx   context.Context
	store *sqlite.SQLiteStorage
}

func newExportImportHelper(t *testing.T, store *sqlite.SQLiteStorage) *exportImportHelper {
	return &exportImportHelper{t: t, ctx: context.Background(), store: store}
}

func (h *exportImportHelper) createIssue(id, title, desc string, status types.Status, priority int, issueType types.IssueType, assignee string, closedAt *time.Time) *types.Issue {
	now := time.Now()
	issue := &types.Issue{
		ID:          id,
		Title:       title,
		Description: desc,
		Status:      status,
		Priority:    priority,
		IssueType:   issueType,
		Assignee:    assignee,
		CreatedAt:   now,
		UpdatedAt:   now,
		ClosedAt:    closedAt,
	}
	if err := h.store.CreateIssue(h.ctx, issue, "test"); err != nil {
		h.t.Fatalf("Failed to create issue: %v", err)
	}
	return issue
}

func (h *exportImportHelper) createFullIssue(id string, estimatedMinutes int) *types.Issue {
	closedAt := time.Now()
	issue := &types.Issue{
		ID:                 id,
		Title:              "Full issue",
		Description:        "Description",
		Design:             "Design doc",
		AcceptanceCriteria: "Criteria",
		Notes:              "Notes",
		Status:             types.StatusClosed,
		Priority:           1,
		IssueType:          types.TypeFeature,
		Assignee:           "alice",
		EstimatedMinutes:   &estimatedMinutes,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		ClosedAt:           &closedAt,
	}
	if err := h.store.CreateIssue(h.ctx, issue, "test"); err != nil {
		h.t.Fatalf("Failed to create issue: %v", err)
	}
	return issue
}

func (h *exportImportHelper) searchIssues(filter types.IssueFilter) []*types.Issue {
	issues, err := h.store.SearchIssues(h.ctx, "", filter)
	if err != nil {
		h.t.Fatalf("SearchIssues failed: %v", err)
	}
	return issues
}

func (h *exportImportHelper) getIssue(id string) *types.Issue {
	issue, err := h.store.GetIssue(h.ctx, id)
	if err != nil {
		h.t.Fatalf("GetIssue failed: %v", err)
	}
	return issue
}

func (h *exportImportHelper) updateIssue(id string, updates map[string]interface{}) {
	if err := h.store.UpdateIssue(h.ctx, id, updates, "test"); err != nil {
		h.t.Fatalf("UpdateIssue failed: %v", err)
	}
}

func (h *exportImportHelper) assertCount(count, expected int, item string) {
	if count != expected {
		h.t.Errorf("Expected %d %s, got %d", expected, item, count)
	}
}

func (h *exportImportHelper) assertEqual(expected, actual interface{}, field string) {
	if expected != actual {
		h.t.Errorf("%s = %v, want %v", field, actual, expected)
	}
}

func (h *exportImportHelper) assertSorted(issues []*types.Issue) {
	for i := 0; i < len(issues)-1; i++ {
		if issues[i].ID > issues[i+1].ID {
			h.t.Errorf("Issues not sorted by ID: %s > %s", issues[i].ID, issues[i+1].ID)
		}
	}
}

func (h *exportImportHelper) encodeJSONL(issues []*types.Issue) *bytes.Buffer {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			h.t.Fatalf("Failed to encode issue: %v", err)
		}
	}
	return &buf
}

func (h *exportImportHelper) validateJSONLines(buf *bytes.Buffer, expectedCount int) {
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	h.assertCount(len(lines), expectedCount, "JSONL lines")
	for i, line := range lines {
		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			h.t.Errorf("Line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestExportImport(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := newTestStoreWithPrefix(t, dbPath, "test")

	h := newExportImportHelper(t, store)
	now := time.Now()

	// Create test issues
	h.createIssue("test-1", "First issue", "Description 1", types.StatusOpen, 1, types.TypeBug, "", nil)
	h.createIssue("test-2", "Second issue", "Description 2", types.StatusInProgress, 2, types.TypeFeature, "alice", nil)
	h.createIssue("test-3", "Third issue", "Description 3", types.StatusClosed, 3, types.TypeTask, "", &now)

	// Test export
	t.Run("Export", func(t *testing.T) {
		exported := h.searchIssues(types.IssueFilter{})
		h.assertCount(len(exported), 3, "issues")
		h.assertSorted(exported)
	})

	// Test JSONL format
	t.Run("JSONL Format", func(t *testing.T) {
		exported := h.searchIssues(types.IssueFilter{})
		buf := h.encodeJSONL(exported)
		h.validateJSONLines(buf, 3)
	})

	// Test import into new database
	t.Run("Import", func(t *testing.T) {
		exported := h.searchIssues(types.IssueFilter{})
		newDBPath := filepath.Join(tmpDir, "import-test.db")
		newStore := newTestStoreWithPrefix(t, newDBPath, "test")
		newHelper := newExportImportHelper(t, newStore)
		for _, issue := range exported {
			newHelper.createIssue(issue.ID, issue.Title, issue.Description, issue.Status, issue.Priority, issue.IssueType, issue.Assignee, issue.ClosedAt)
		}
		imported := newHelper.searchIssues(types.IssueFilter{})
		newHelper.assertCount(len(imported), len(exported), "issues")
		for i := range imported {
			newHelper.assertEqual(exported[i].ID, imported[i].ID, "ID")
			newHelper.assertEqual(exported[i].Title, imported[i].Title, "Title")
		}
	})

	// Test update on import
	t.Run("Import Update", func(t *testing.T) {
		issue := h.getIssue("test-1")
		updates := map[string]interface{}{"title": "Updated title", "status": string(types.StatusClosed)}
		h.updateIssue(issue.ID, updates)
		updated := h.getIssue("test-1")
		h.assertEqual("Updated title", updated.Title, "Title")
		h.assertEqual(types.StatusClosed, updated.Status, "Status")
	})

	// Test filtering on export
	t.Run("Export with Filter", func(t *testing.T) {
		status := types.StatusOpen
		filtered := h.searchIssues(types.IssueFilter{Status: &status})
		for _, issue := range filtered {
			if issue.Status != types.StatusOpen {
				t.Errorf("Expected only open issues, got %s", issue.Status)
			}
		}
	})
}

func TestExportEmpty(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.db")
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	// Export from empty database
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("Expected 0 issues, got %d", len(issues))
	}
}

func TestImportInvalidJSON(t *testing.T) {
	t.Parallel()
	invalidJSON := []string{
		`{"id":"test-1"`,            // Incomplete JSON
		`{"id":"test-1","title":}`,  // Invalid syntax
		`not json at all`,           // Not JSON
		`{"id":"","title":"No ID"}`, // Empty ID
	}

	for i, line := range invalidJSON {
		var issue types.Issue
		err := json.Unmarshal([]byte(line), &issue)
		if err == nil && line != invalidJSON[3] { // Empty ID case will unmarshal but fail validation
			t.Errorf("Case %d: Expected unmarshal error for invalid JSON: %s", i, line)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "original.db")
	store := newTestStoreWithPrefix(t, dbPath, "test")
	h := newExportImportHelper(t, store)
	original := h.createFullIssue("test-1", 120)

	// Export to JSONL
	buf := h.encodeJSONL([]*types.Issue{original})

	// Import from JSONL
	var decoded types.Issue
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	// Verify all fields preserved
	h.assertEqual(original.ID, decoded.ID, "ID")
	h.assertEqual(original.Title, decoded.Title, "Title")
	h.assertEqual(original.Description, decoded.Description, "Description")
	if decoded.EstimatedMinutes == nil || *decoded.EstimatedMinutes != *original.EstimatedMinutes {
		t.Errorf("EstimatedMinutes = %v, want %v", decoded.EstimatedMinutes, original.EstimatedMinutes)
	}
}

// TestExportIncludesTombstones verifies that tombstones are included in JSONL export (bd-yk8w)
func TestExportIncludesTombstones(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := newTestStoreWithPrefix(t, dbPath, "test")

	// Create a regular issue
	regularIssue := &types.Issue{
		ID:        "test-abc",
		Title:     "Regular issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, regularIssue, "test"); err != nil {
		t.Fatalf("Failed to create regular issue: %v", err)
	}

	// Create a tombstone issue
	deletedAt := time.Now().Add(-time.Hour)
	tombstone := &types.Issue{
		ID:           "test-def",
		Title:        "(deleted)",
		Status:       types.StatusTombstone,
		Priority:     2,
		IssueType:    types.TypeTask,
		CreatedAt:    time.Now().Add(-48 * time.Hour),
		UpdatedAt:    deletedAt,
		DeletedAt:    &deletedAt,
		DeletedBy:    "alice",
		DeleteReason: "duplicate issue",
		OriginalType: "bug",
	}
	if err := store.CreateIssue(ctx, tombstone, "test"); err != nil {
		t.Fatalf("Failed to create tombstone: %v", err)
	}

	// Export all issues (including tombstones)
	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	// Verify we got both issues
	if len(allIssues) != 2 {
		t.Fatalf("Expected 2 issues (1 regular + 1 tombstone), got %d", len(allIssues))
	}

	// Encode to JSONL
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, issue := range allIssues {
		if err := encoder.Encode(issue); err != nil {
			t.Fatalf("Failed to encode issue: %v", err)
		}
	}

	// Verify JSONL contains both issues
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 JSONL lines, got %d", len(lines))
	}

	// Parse and verify tombstone fields are present
	foundTombstone := false
	for _, line := range lines {
		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			t.Fatalf("Failed to parse JSONL line: %v", err)
		}

		if issue.ID == "test-def" {
			foundTombstone = true
			if issue.Status != types.StatusTombstone {
				t.Errorf("Expected tombstone status, got %q", issue.Status)
			}
			if issue.DeletedBy != "alice" {
				t.Errorf("Expected DeletedBy 'alice', got %q", issue.DeletedBy)
			}
			if issue.DeleteReason != "duplicate issue" {
				t.Errorf("Expected DeleteReason 'duplicate issue', got %q", issue.DeleteReason)
			}
			if issue.OriginalType != "bug" {
				t.Errorf("Expected OriginalType 'bug', got %q", issue.OriginalType)
			}
			if issue.DeletedAt == nil {
				t.Error("Expected DeletedAt to be set")
			}
		}
	}

	if !foundTombstone {
		t.Error("Tombstone not found in JSONL output")
	}
}

// TestCloseReasonRoundTrip verifies that close_reason is preserved through JSONL export/import (bd-lxzx)
func TestCloseReasonRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := newTestStoreWithPrefix(t, dbPath, "test")

	// Create an issue and close it with a reason
	issue := &types.Issue{
		ID:        "test-close-reason",
		Title:     "Issue to close",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Close the issue with a reason
	closeReason := "Completed: all tests passing"
	if err := store.CloseIssue(ctx, issue.ID, closeReason, "test-actor", ""); err != nil {
		t.Fatalf("Failed to close issue: %v", err)
	}

	// Verify close_reason was stored
	closed, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get closed issue: %v", err)
	}
	if closed.CloseReason != closeReason {
		t.Fatalf("CloseReason not stored: got %q, want %q", closed.CloseReason, closeReason)
	}

	// Export to JSONL
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, i := range issues {
		if err := encoder.Encode(i); err != nil {
			t.Fatalf("Failed to encode issue: %v", err)
		}
	}

	// Parse the JSONL and verify close_reason is present
	var decoded types.Issue
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("Failed to decode JSONL: %v", err)
	}

	if decoded.CloseReason != closeReason {
		t.Errorf("close_reason not preserved in JSONL: got %q, want %q", decoded.CloseReason, closeReason)
	}

	// Import into a new database and verify close_reason is preserved
	newDBPath := filepath.Join(tmpDir, "import-test.db")
	newStore := newTestStoreWithPrefix(t, newDBPath, "test")

	// Re-create the issue in new database (simulating import)
	decoded.ContentHash = "" // Clear so it gets recomputed
	if err := newStore.CreateIssue(ctx, &decoded, "test"); err != nil {
		t.Fatalf("Failed to import issue: %v", err)
	}

	// Verify the imported issue has close_reason
	imported, err := newStore.GetIssue(ctx, decoded.ID)
	if err != nil {
		t.Fatalf("Failed to get imported issue: %v", err)
	}

	if imported.CloseReason != closeReason {
		t.Errorf("close_reason not preserved after import: got %q, want %q", imported.CloseReason, closeReason)
	}
	if imported.Status != types.StatusClosed {
		t.Errorf("Status not preserved: got %q, want %q", imported.Status, types.StatusClosed)
	}
}
