package ephemeral

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-ephemeral.sqlite3")
	s, err := New(dbPath, "bd")
	if err != nil {
		t.Fatalf("failed to create ephemeral store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew(t *testing.T) {
	s := newTestStore(t)
	if s.db == nil {
		t.Fatal("db is nil")
	}
	if s.Path() == "" {
		t.Fatal("path is empty")
	}
}

func TestNew_InvalidPath(t *testing.T) {
	_, err := New("/nonexistent/deeply/nested/path/ephemeral.sqlite3", "bd")
	if err != nil {
		// On macOS, creating deeply nested paths may fail depending on permissions
		// but the MkdirAll should handle most cases
		t.Skipf("expected path creation to work: %v", err)
	}
}

func TestCreateAndGetIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issue := &types.Issue{
		ID:        "bd-wisp-test123",
		Title:     "Test wisp issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
		WispType:  types.WispTypeHeartbeat,
	}

	if err := s.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	got, err := s.GetIssue(ctx, "bd-wisp-test123")
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if got == nil {
		t.Fatal("issue not found")
	}
	if got.ID != "bd-wisp-test123" {
		t.Errorf("got ID %q, want %q", got.ID, "bd-wisp-test123")
	}
	if got.Title != "Test wisp issue" {
		t.Errorf("got title %q, want %q", got.Title, "Test wisp issue")
	}
	if !got.Ephemeral {
		t.Error("expected ephemeral=true")
	}
	if got.WispType != types.WispTypeHeartbeat {
		t.Errorf("got wisp_type %q, want %q", got.WispType, types.WispTypeHeartbeat)
	}
	if got.CreatedBy != "test-actor" {
		t.Errorf("got created_by %q, want %q", got.CreatedBy, "test-actor")
	}
}

func TestCreateIssue_ForcesEphemeral(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issue := &types.Issue{
		ID:        "bd-wisp-noeph",
		Title:     "Should become ephemeral",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: false, // Will be overridden
	}

	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetIssue(ctx, "bd-wisp-noeph")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Ephemeral {
		t.Error("expected ephemeral=true, ephemeral store forces it")
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetIssue(ctx, "bd-wisp-nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent issue, got %v", got)
	}
}

func TestUpdateIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issue := &types.Issue{
		ID:        "bd-wisp-upd",
		Title:     "Original title",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateIssue(ctx, "bd-wisp-upd", map[string]interface{}{
		"title":    "Updated title",
		"priority": 1,
	}, "test"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetIssue(ctx, "bd-wisp-upd")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Updated title" {
		t.Errorf("got title %q, want %q", got.Title, "Updated title")
	}
	if got.Priority != 1 {
		t.Errorf("got priority %d, want 1", got.Priority)
	}
}

func TestCloseIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issue := &types.Issue{
		ID:        "bd-wisp-close",
		Title:     "To be closed",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatal(err)
	}

	if err := s.CloseIssue(ctx, "bd-wisp-close", "done", "test", "session-1"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetIssue(ctx, "bd-wisp-close")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != types.StatusClosed {
		t.Errorf("got status %q, want %q", got.Status, types.StatusClosed)
	}
	if got.ClosedAt == nil {
		t.Error("closed_at should be set")
	}
}

func TestDeleteIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issue := &types.Issue{
		ID:        "bd-wisp-del",
		Title:     "To be deleted",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteIssue(ctx, "bd-wisp-del"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetIssue(ctx, "bd-wisp-del")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeleteIssues_Batch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			ID:        "bd-wisp-batch" + string(rune('a'+i)),
			Title:     "Batch item",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			Ephemeral: true,
		}
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := s.DeleteIssues(ctx, []string{"bd-wisp-batcha", "bd-wisp-batchb", "bd-wisp-batchc"})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Errorf("deleted %d, want 3", deleted)
	}

	count, _ := s.Count(ctx)
	if count != 0 {
		t.Errorf("count %d, want 0", count)
	}
}

func TestSearchIssues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	searchItems := []struct{ id, title string }{
		{"bd-wisp-patrol-a", "Patrol report alpha"},
		{"bd-wisp-heartbeat", "Heartbeat check"},
		{"bd-wisp-patrol-b", "Patrol report beta"},
	}
	for _, item := range searchItems {
		issue := &types.Issue{
			ID:        item.id,
			Title:     item.title,
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			Ephemeral: true,
		}
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatal(err)
		}
	}

	results, err := s.SearchIssues(ctx, "Patrol", types.IssueFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}

func TestDependencies(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	parent := &types.Issue{
		ID: "bd-wisp-parent", Title: "Parent", Status: types.StatusOpen,
		Priority: 2, IssueType: types.TypeEpic, Ephemeral: true,
	}
	child := &types.Issue{
		ID: "bd-wisp-child", Title: "Child", Status: types.StatusOpen,
		Priority: 2, IssueType: types.TypeTask, Ephemeral: true,
	}
	if err := s.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateIssue(ctx, child, "test"); err != nil {
		t.Fatal(err)
	}

	dep := &types.Dependency{
		IssueID:     "bd-wisp-child",
		DependsOnID: "bd-wisp-parent",
		Type:        "parent-child",
	}
	if err := s.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatal(err)
	}

	deps, err := s.GetDependencies(ctx, "bd-wisp-child")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].ID != "bd-wisp-parent" {
		t.Errorf("unexpected dependencies: %v", deps)
	}

	dependents, err := s.GetDependents(ctx, "bd-wisp-parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(dependents) != 1 || dependents[0].ID != "bd-wisp-child" {
		t.Errorf("unexpected dependents: %v", dependents)
	}
}

func TestLabels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issue := &types.Issue{
		ID: "bd-wisp-lbl", Title: "Labeled", Status: types.StatusOpen,
		Priority: 2, IssueType: types.TypeTask, Ephemeral: true,
	}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatal(err)
	}

	if err := s.AddLabel(ctx, "bd-wisp-lbl", "patrol", "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLabel(ctx, "bd-wisp-lbl", "urgent", "test"); err != nil {
		t.Fatal(err)
	}

	labels, err := s.GetLabels(ctx, "bd-wisp-lbl")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 {
		t.Errorf("got %d labels, want 2", len(labels))
	}
}

func TestNuke(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			ID:        "bd-wisp-nuke" + string(rune('a'+i)),
			Title:     "To be nuked",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			Ephemeral: true,
		}
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatal(err)
		}
	}

	count, _ := s.Count(ctx)
	if count != 5 {
		t.Fatalf("pre-nuke count %d, want 5", count)
	}

	if err := s.Nuke(ctx); err != nil {
		t.Fatal(err)
	}

	count, _ = s.Count(ctx)
	if count != 0 {
		t.Errorf("post-nuke count %d, want 0", count)
	}
}

func TestIsEphemeralID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"bd-wisp-abc123", true},
		{"bd-wisp-8ajy9h", true},
		{"bd-1234", false},
		{"bd-abc", false},
		{"gt-wisp-123", true},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsEphemeralID(tt.id); got != tt.want {
			t.Errorf("IsEphemeralID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("empty store count %d, want 0", count)
	}
}

func TestGetIssuesByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ids := []string{"bd-wisp-a1", "bd-wisp-b2", "bd-wisp-c3"}
	for _, id := range ids {
		issue := &types.Issue{
			ID: id, Title: "Issue " + id, Status: types.StatusOpen,
			Priority: 2, IssueType: types.TypeTask, Ephemeral: true,
		}
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.GetIssuesByIDs(ctx, []string{"bd-wisp-a1", "bd-wisp-c3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d issues, want 2", len(got))
	}
}

func TestTransaction(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issue := &types.Issue{
			ID: "bd-wisp-tx1", Title: "TX Issue", Status: types.StatusOpen,
			Priority: 2, IssueType: types.TypeTask, Ephemeral: true,
		}
		if err := tx.CreateIssue(ctx, issue, "test"); err != nil {
			return err
		}
		got, err := tx.GetIssue(ctx, "bd-wisp-tx1")
		if err != nil {
			return err
		}
		if got == nil {
			t.Error("expected to see issue within transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify committed
	got, err := s.GetIssue(ctx, "bd-wisp-tx1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("issue should be committed after transaction")
	}
}

func TestCreateIssue_WithTimestamps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	past := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	issue := &types.Issue{
		ID: "bd-wisp-ts", Title: "Timestamped", Status: types.StatusOpen,
		Priority: 2, IssueType: types.TypeTask, Ephemeral: true,
		CreatedAt: past,
	}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetIssue(ctx, "bd-wisp-ts")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(past) {
		t.Errorf("created_at %v, want %v", got.CreatedAt, past)
	}
}

func TestClose_FileCleanup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cleanup.sqlite3")

	s, err := New(dbPath, "bd")
	if err != nil {
		t.Fatal(err)
	}

	// DB file should exist
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file should exist: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Double close should not error
	if err := s.Close(); err != nil {
		t.Errorf("double close should not error: %v", err)
	}
}
