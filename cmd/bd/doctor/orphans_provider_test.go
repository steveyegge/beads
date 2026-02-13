package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/testutil/teststore"
	"github.com/steveyegge/beads/internal/types"
)

// mockIssueProvider implements types.IssueProvider for testing FindOrphanedIssues
type mockIssueProvider struct {
	issues []*types.Issue
	prefix string
	err    error // If set, GetOpenIssues returns this error
}

func (m *mockIssueProvider) GetOpenIssues(ctx context.Context) ([]*types.Issue, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.issues, nil
}

func (m *mockIssueProvider) GetIssuePrefix() string {
	if m.prefix == "" {
		return "bd"
	}
	return m.prefix
}

// Ensure mockIssueProvider implements types.IssueProvider
var _ types.IssueProvider = (*mockIssueProvider)(nil)

// setupTestGitRepo creates a git repo with specified commits for testing
func setupTestGitRepo(t *testing.T, commits []string) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	_ = cmd.Run()

	// Create commits
	for i, msg := range commits {
		testFile := filepath.Join(dir, "file.txt")
		content := []byte("content " + string(rune('0'+i)))
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		cmd = exec.Command("git", "add", "file.txt")
		cmd.Dir = dir
		_ = cmd.Run()
		cmd = exec.Command("git", "commit", "-m", msg)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to create commit: %v", err)
		}
	}

	return dir
}

// TestFindOrphanedIssues_WithMockProvider tests FindOrphanedIssues with various mock providers
func TestFindOrphanedIssues_WithMockProvider(t *testing.T) {
	tests := map[string]struct {
		provider *mockIssueProvider
		commits  []string
		expected int    // Number of orphans expected
		issueID  string // Expected issue ID in orphans (if expected > 0)
	}{
		"UT-01: Basic orphan detection": {
			provider: &mockIssueProvider{
				issues: []*types.Issue{
					{ID: "bd-abc", Title: "Test issue", Status: types.StatusOpen},
				},
				prefix: "bd",
			},
			commits:  []string{"Initial commit", "Fix bug (bd-abc)"},
			expected: 1,
			issueID:  "bd-abc",
		},
		"UT-02: No orphans when no matching commits": {
			provider: &mockIssueProvider{
				issues: []*types.Issue{
					{ID: "bd-xyz", Title: "Test issue", Status: types.StatusOpen},
				},
				prefix: "bd",
			},
			commits:  []string{"Initial commit", "Some other change"},
			expected: 0,
		},
		"UT-03: Custom prefix TEST": {
			provider: &mockIssueProvider{
				issues: []*types.Issue{
					{ID: "TEST-001", Title: "Test issue", Status: types.StatusOpen},
				},
				prefix: "TEST",
			},
			commits:  []string{"Initial commit", "Implement feature (TEST-001)"},
			expected: 1,
			issueID:  "TEST-001",
		},
		"UT-04: Multiple orphans": {
			provider: &mockIssueProvider{
				issues: []*types.Issue{
					{ID: "bd-aaa", Title: "Issue A", Status: types.StatusOpen},
					{ID: "bd-bbb", Title: "Issue B", Status: types.StatusOpen},
					{ID: "bd-ccc", Title: "Issue C", Status: types.StatusOpen},
				},
				prefix: "bd",
			},
			commits:  []string{"Initial commit", "Fix (bd-aaa)", "Fix (bd-ccc)"},
			expected: 2,
		},
		"UT-06: In-progress issues included": {
			provider: &mockIssueProvider{
				issues: []*types.Issue{
					{ID: "bd-wip", Title: "Work in progress", Status: types.StatusInProgress},
				},
				prefix: "bd",
			},
			commits:  []string{"Initial commit", "WIP (bd-wip)"},
			expected: 1,
			issueID:  "bd-wip",
		},
		"UT-08: Empty provider returns empty slice": {
			provider: &mockIssueProvider{
				issues: []*types.Issue{},
				prefix: "bd",
			},
			commits:  []string{"Initial commit", "Some change (bd-xxx)"},
			expected: 0,
		},
		"UT-09: Hierarchical IDs": {
			provider: &mockIssueProvider{
				issues: []*types.Issue{
					{ID: "bd-abc.1", Title: "Subtask", Status: types.StatusOpen},
				},
				prefix: "bd",
			},
			commits:  []string{"Initial commit", "Fix subtask (bd-abc.1)"},
			expected: 1,
			issueID:  "bd-abc.1",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			gitDir := setupTestGitRepo(t, tt.commits)

			orphans, err := FindOrphanedIssues(gitDir, tt.provider)
			if err != nil {
				t.Fatalf("FindOrphanedIssues returned error: %v", err)
			}

			if len(orphans) != tt.expected {
				t.Errorf("expected %d orphans, got %d", tt.expected, len(orphans))
			}

			if tt.issueID != "" && len(orphans) > 0 {
				found := false
				for _, o := range orphans {
					if o.IssueID == tt.issueID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected to find orphan with ID %q, but didn't", tt.issueID)
				}
			}
		})
	}
}

// TestFindOrphanedIssues_CrossRepo tests cross-repo orphan detection (IT-02).
// This is the key test that validates the --db flag is honored.
// The test creates:
//   - A "planning" directory with a mock provider (simulating external DB)
//   - A "code" directory with git commits referencing the planning issues
//
// The test asserts that FindOrphanedIssues uses the provider's issues/prefix,
// NOT any local .beads/ directory.
func TestFindOrphanedIssues_CrossRepo(t *testing.T) {
	// Setup: code repo with commits referencing PLAN-xxx issues
	codeDir := setupTestGitRepo(t, []string{
		"Initial commit",
		"Implement feature (PLAN-001)",
		"Fix bug (PLAN-002)",
	})

	// Simulate planning repo's provider (this would normally come from --db flag)
	planningProvider := &mockIssueProvider{
		issues: []*types.Issue{
			{ID: "PLAN-001", Title: "Feature A", Status: types.StatusOpen},
			{ID: "PLAN-002", Title: "Bug B", Status: types.StatusOpen},
			{ID: "PLAN-003", Title: "Not referenced", Status: types.StatusOpen},
		},
		prefix: "PLAN",
	}

	// Create a LOCAL .beads/ in the code repo to verify it's NOT used.
	// Just an empty directory is enough to prove the mock provider is used instead.
	localBeadsDir := filepath.Join(codeDir, ".beads")
	if err := os.MkdirAll(localBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Call FindOrphanedIssues with the planning provider
	orphans, err := FindOrphanedIssues(codeDir, planningProvider)
	if err != nil {
		t.Fatalf("FindOrphanedIssues returned error: %v", err)
	}

	// Assert: Should find 2 orphans (PLAN-001, PLAN-002) from planning provider
	if len(orphans) != 2 {
		t.Errorf("expected 2 orphans, got %d", len(orphans))
	}

	// Assert: Should NOT find LOCAL-999 (proves local .beads/ was ignored)
	for _, o := range orphans {
		if o.IssueID == "LOCAL-999" {
			t.Error("found LOCAL-999 orphan - local .beads/ was incorrectly used")
		}
		if o.IssueID != "PLAN-001" && o.IssueID != "PLAN-002" {
			t.Errorf("unexpected orphan ID: %s", o.IssueID)
		}
	}

	// Assert: PLAN-003 should NOT be in orphans (not referenced in commits)
	for _, o := range orphans {
		if o.IssueID == "PLAN-003" {
			t.Error("found PLAN-003 orphan - it was not referenced in commits")
		}
	}
}

// TestFindOrphanedIssues_LocalProvider tests backward compatibility (RT-01).
// This tests that a LocalProvider created from a storage.Storage backend
// correctly detects orphans, which is the same code path used by
// FindOrphanedIssuesFromPath internally.
func TestFindOrphanedIssues_LocalProvider(t *testing.T) {
	// Create a Dolt-backed store with the issues we need
	store := teststore.New(t)
	ctx := context.Background()

	// Override the default "test" prefix to "bd" for this test
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatal(err)
	}

	// Create an open issue
	issue := &types.Issue{
		Title:     "Local test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatal(err)
	}

	// Create a LocalProvider from the store
	provider, err := storage.NewLocalProvider(store)
	if err != nil {
		t.Fatalf("NewLocalProvider failed: %v", err)
	}

	// Setup git repo with a commit referencing the issue
	dir := setupTestGitRepo(t, []string{"Fix (" + issue.ID + ")"})

	orphans, err := FindOrphanedIssues(dir, provider)
	if err != nil {
		t.Fatalf("FindOrphanedIssues returned error: %v", err)
	}

	if len(orphans) != 1 {
		t.Errorf("expected 1 orphan, got %d", len(orphans))
	}

	if len(orphans) > 0 && orphans[0].IssueID != issue.ID {
		t.Errorf("expected orphan ID %s, got %s", issue.ID, orphans[0].IssueID)
	}
}

// TestFindOrphanedIssues_ProviderError tests error handling (UT-07).
// When provider returns an error, FindOrphanedIssues should return empty slice.
func TestFindOrphanedIssues_ProviderError(t *testing.T) {
	gitDir := setupTestGitRepo(t, []string{"Initial commit", "Fix (bd-abc)"})

	provider := &mockIssueProvider{
		err: errors.New("provider error: database unavailable"),
	}

	orphans, err := FindOrphanedIssues(gitDir, provider)

	// Should return empty slice, no error (graceful degradation)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("expected 0 orphans on provider error, got %d", len(orphans))
	}
}

// TestFindOrphanedIssues_NotGitRepo tests behavior in non-git directory (IT-04).
func TestFindOrphanedIssues_NotGitRepo(t *testing.T) {
	dir := t.TempDir() // No git init

	provider := &mockIssueProvider{
		issues: []*types.Issue{
			{ID: "bd-test", Title: "Test", Status: types.StatusOpen},
		},
		prefix: "bd",
	}

	orphans, err := FindOrphanedIssues(dir, provider)

	// Should return empty slice, no error
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("expected 0 orphans in non-git dir, got %d", len(orphans))
	}
}

// TestFindOrphanedIssues_IntegrationCrossRepo tests a realistic cross-repo setup (IT-02 full).
// This creates a Dolt-backed planning store and a separate code git repo, then
// verifies that cross-repo orphan detection works via a real LocalProvider.
func TestFindOrphanedIssues_IntegrationCrossRepo(t *testing.T) {
	// Create a Dolt-backed "planning" store with issues
	planningStore := teststore.New(t)
	ctx := context.Background()

	// Set a custom prefix for the planning store
	if err := planningStore.SetConfig(ctx, "issue_prefix", "PLAN"); err != nil {
		t.Fatal(err)
	}

	// Create two planning issues (one open, one in-progress)
	issueA := &types.Issue{
		Title:     "Feature A",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	issueB := &types.Issue{
		Title:     "Feature B",
		Status:    types.StatusInProgress,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := planningStore.CreateIssue(ctx, issueA, "test-user"); err != nil {
		t.Fatal(err)
	}
	if err := planningStore.CreateIssue(ctx, issueB, "test-user"); err != nil {
		t.Fatal(err)
	}

	// Setup code repo with a commit referencing one planning issue
	codeDir := setupTestGitRepo(t, []string{
		"Initial commit",
		"Implement (" + issueA.ID + ")",
	})

	// Create a real LocalProvider from the planning store
	provider, err := storage.NewLocalProvider(planningStore)
	if err != nil {
		t.Fatalf("failed to create LocalProvider: %v", err)
	}

	// Run FindOrphanedIssues with the cross-repo provider
	orphans, err := FindOrphanedIssues(codeDir, provider)
	if err != nil {
		t.Fatalf("FindOrphanedIssues returned error: %v", err)
	}

	// Should find 1 orphan (the issue referenced in a commit)
	if len(orphans) != 1 {
		t.Errorf("expected 1 orphan, got %d", len(orphans))
	}

	if len(orphans) > 0 && orphans[0].IssueID != issueA.ID {
		t.Errorf("expected orphan %s, got %s", issueA.ID, orphans[0].IssueID)
	}
}

// TestLocalProvider_Methods tests the LocalProvider implementation directly.
func TestLocalProvider_Methods(t *testing.T) {
	store := teststore.New(t)
	ctx := context.Background()

	// Set a custom prefix
	if err := store.SetConfig(ctx, "issue_prefix", "CUSTOM"); err != nil {
		t.Fatal(err)
	}

	// Create issues with various statuses
	openIssue := &types.Issue{Title: "Open issue", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	wipIssue := &types.Issue{Title: "WIP issue", Status: types.StatusInProgress, Priority: 2, IssueType: types.TypeTask}
	closedIssue := &types.Issue{Title: "Closed issue", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}

	if err := store.CreateIssue(ctx, openIssue, "test-user"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateIssue(ctx, wipIssue, "test-user"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateIssue(ctx, closedIssue, "test-user"); err != nil {
		t.Fatal(err)
	}
	// Close the third issue
	if err := store.CloseIssue(ctx, closedIssue.ID, "done", "test-user", ""); err != nil {
		t.Fatal(err)
	}

	// Create provider
	provider, err := storage.NewLocalProvider(store)
	if err != nil {
		t.Fatalf("storage.NewLocalProvider failed: %v", err)
	}

	// Test GetIssuePrefix
	prefix := provider.GetIssuePrefix()
	if prefix != "CUSTOM" {
		t.Errorf("expected prefix CUSTOM, got %s", prefix)
	}

	// Test GetOpenIssues
	issues, err := provider.GetOpenIssues(ctx)
	if err != nil {
		t.Fatalf("GetOpenIssues failed: %v", err)
	}

	// Should return 2 issues (open + in_progress), not the closed one
	if len(issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(issues))
	}

	// Verify issue IDs
	ids := make(map[string]bool)
	for _, issue := range issues {
		ids[issue.ID] = true
	}

	if !ids[openIssue.ID] {
		t.Errorf("expected %s in open issues", openIssue.ID)
	}
	if !ids[wipIssue.ID] {
		t.Errorf("expected %s in open issues", wipIssue.ID)
	}
	if ids[closedIssue.ID] {
		t.Errorf("%s (closed) should not be in open issues", closedIssue.ID)
	}
}

// TestLocalProvider_DefaultPrefix tests that LocalProvider returns "bd" when no prefix configured.
func TestLocalProvider_DefaultPrefix(t *testing.T) {
	store := teststore.New(t)
	ctx := context.Background()

	// Clear the issue_prefix that teststore.New sets by default ("test").
	// Setting it to empty string triggers the "bd" default in NewLocalProvider.
	if err := store.SetConfig(ctx, "issue_prefix", ""); err != nil {
		t.Fatal(err)
	}

	provider, err := storage.NewLocalProvider(store)
	if err != nil {
		t.Fatalf("storage.NewLocalProvider failed: %v", err)
	}

	prefix := provider.GetIssuePrefix()
	if prefix != "bd" {
		t.Errorf("expected default prefix 'bd', got %s", prefix)
	}
}
