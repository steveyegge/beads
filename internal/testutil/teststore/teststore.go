// Package teststore provides Dolt-backed test helpers for storage tests.
//
// This package replaces the sqlite-specific test_helpers.go pattern with a
// shared test environment that creates an isolated Dolt store per test.
// All helper methods operate through the storage.Storage interface, making
// tests backend-agnostic.
//
// Tests using this package require the `dolt` binary in PATH. When Dolt is
// not available, tests are skipped automatically via t.Skip.
//
// Usage:
//
//	func TestSomething(t *testing.T) {
//	    env := teststore.NewEnv(t)
//	    issue := env.CreateIssue("fix the widget")
//	    env.AssertReady(issue)
//	}
package teststore

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// doltInitMu serializes Dolt engine creation to avoid data races in the
// go-mysql-server global status variable initialization (upstream issue).
var doltInitMu sync.Mutex

// New creates an isolated Dolt-backed storage.Storage for a single test or benchmark.
//
// The store is initialized with issue_prefix "test" and the standard Gas Town
// custom issue types. Both the store and its temp directory are cleaned up
// automatically when the test completes.
//
// If the `dolt` binary is not found in PATH the test is skipped.
func New(t testing.TB) storage.Storage {
	t.Helper()

	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt binary not in PATH, skipping test")
	}

	tmpDir, err := os.MkdirTemp("", "teststore-*")
	if err != nil {
		t.Fatalf("teststore: failed to create temp dir: %v", err)
	}

	ctx := context.Background()

	cfg := &dolt.Config{
		Path:              tmpDir,
		CommitterName:     "test",
		CommitterEmail:    "test@example.com",
		Database:          "testdb",
		SkipDirtyTracking: true,
	}

	// Serialize Dolt engine creation to avoid upstream race in InitStatusVariables.
	doltInitMu.Lock()
	store, err := dolt.New(ctx, cfg)
	doltInitMu.Unlock()
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("teststore: failed to create Dolt store: %v", err)
	}

	// Set issue_prefix so ID generation works (mirrors bd-166 requirement).
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		_ = store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("teststore: failed to set issue_prefix: %v", err)
	}

	// Register standard Gas Town custom issue types.
	if err := store.SetConfig(ctx, "types.custom", "gate,molecule,convoy,merge-request,slot,agent,role,rig,message"); err != nil {
		_ = store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("teststore: failed to set custom types: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		os.RemoveAll(tmpDir)
	})

	return store
}

// Env provides a test environment with common setup and helpers.
// All operations go through the storage.Storage interface so that tests
// remain backend-agnostic.
type Env struct {
	t     *testing.T
	Store storage.Storage
	Ctx   context.Context
}

// NewEnv creates a new test environment backed by an isolated Dolt store.
// The store is automatically cleaned up when the test completes.
//
// If the `dolt` binary is not found in PATH the test is skipped.
func NewEnv(t *testing.T) *Env {
	t.Helper()
	store := New(t)
	return &Env{
		t:     t,
		Store: store,
		Ctx:   context.Background(),
	}
}

// ---------------------------------------------------------------------------
// Issue creation helpers
// ---------------------------------------------------------------------------

// CreateIssue creates a test issue with the given title and sensible defaults
// (status open, priority 2, type task). Returns the created issue with its
// generated ID populated.
func (e *Env) CreateIssue(title string) *types.Issue {
	e.t.Helper()
	return e.CreateIssueWith(title, types.StatusOpen, 2, types.TypeTask)
}

// CreateIssueWith creates a test issue with the specified attributes.
func (e *Env) CreateIssueWith(title string, status types.Status, priority int, issueType types.IssueType) *types.Issue {
	e.t.Helper()
	issue := &types.Issue{
		Title:     title,
		Status:    status,
		Priority:  priority,
		IssueType: issueType,
	}
	if err := e.Store.CreateIssue(e.Ctx, issue, "test-user"); err != nil {
		e.t.Fatalf("CreateIssue(%q) failed: %v", title, err)
	}
	return issue
}

// CreateIssueWithAssignee creates a test issue assigned to the given user.
func (e *Env) CreateIssueWithAssignee(title, assignee string) *types.Issue {
	e.t.Helper()
	issue := &types.Issue{
		Title:     title,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Assignee:  assignee,
	}
	if err := e.Store.CreateIssue(e.Ctx, issue, "test-user"); err != nil {
		e.t.Fatalf("CreateIssue(%q) failed: %v", title, err)
	}
	return issue
}

// CreateIssueWithID creates a test issue with an explicit ID.
// Useful for testing ID-based filtering (e.g., molecule step exclusion).
func (e *Env) CreateIssueWithID(id, title string) *types.Issue {
	e.t.Helper()
	issue := &types.Issue{
		ID:        id,
		Title:     title,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := e.Store.CreateIssue(e.Ctx, issue, "test-user"); err != nil {
		e.t.Fatalf("CreateIssue(%q, %q) failed: %v", id, title, err)
	}
	return issue
}

// CreateEpic creates an epic issue with priority 1.
func (e *Env) CreateEpic(title string) *types.Issue {
	e.t.Helper()
	return e.CreateIssueWith(title, types.StatusOpen, 1, types.TypeEpic)
}

// CreateBug creates a bug issue with the specified priority.
func (e *Env) CreateBug(title string, priority int) *types.Issue {
	e.t.Helper()
	return e.CreateIssueWith(title, types.StatusOpen, priority, types.TypeBug)
}

// ---------------------------------------------------------------------------
// Dependency helpers
// ---------------------------------------------------------------------------

// AddDep adds a blocking dependency (issue depends on dependsOn).
func (e *Env) AddDep(issue, dependsOn *types.Issue) {
	e.t.Helper()
	e.AddDepType(issue, dependsOn, types.DepBlocks)
}

// AddDepType adds a dependency with the specified type.
func (e *Env) AddDepType(issue, dependsOn *types.Issue, depType types.DependencyType) {
	e.t.Helper()
	dep := &types.Dependency{
		IssueID:     issue.ID,
		DependsOnID: dependsOn.ID,
		Type:        depType,
	}
	if err := e.Store.AddDependency(e.Ctx, dep, "test-user"); err != nil {
		e.t.Fatalf("AddDependency(%s -> %s) failed: %v", issue.ID, dependsOn.ID, err)
	}
}

// AddParentChild adds a parent-child dependency (child belongs to parent).
func (e *Env) AddParentChild(child, parent *types.Issue) {
	e.t.Helper()
	e.AddDepType(child, parent, types.DepParentChild)
}

// ---------------------------------------------------------------------------
// Lifecycle helpers
// ---------------------------------------------------------------------------

// Close closes the issue with the given reason.
func (e *Env) Close(issue *types.Issue, reason string) {
	e.t.Helper()
	if err := e.Store.CloseIssue(e.Ctx, issue.ID, reason, "test-user", ""); err != nil {
		e.t.Fatalf("CloseIssue(%s) failed: %v", issue.ID, err)
	}
}

// ---------------------------------------------------------------------------
// Ready work helpers
// ---------------------------------------------------------------------------

// GetReadyWork returns issues matching the given filter that are not blocked.
func (e *Env) GetReadyWork(filter types.WorkFilter) []*types.Issue {
	e.t.Helper()
	ready, err := e.Store.GetReadyWork(e.Ctx, filter)
	if err != nil {
		e.t.Fatalf("GetReadyWork failed: %v", err)
	}
	return ready
}

// GetReadyIDs returns a set of issue IDs that are ready (open status, not blocked).
func (e *Env) GetReadyIDs() map[string]bool {
	e.t.Helper()
	ready := e.GetReadyWork(types.WorkFilter{Status: types.StatusOpen})
	ids := make(map[string]bool, len(ready))
	for _, issue := range ready {
		ids[issue.ID] = true
	}
	return ids
}

// AssertReady asserts that the issue appears in the ready work list.
func (e *Env) AssertReady(issue *types.Issue) {
	e.t.Helper()
	ids := e.GetReadyIDs()
	if !ids[issue.ID] {
		e.t.Errorf("expected %s (%s) to be ready, but it was blocked", issue.ID, issue.Title)
	}
}

// AssertBlocked asserts that the issue does NOT appear in the ready work list.
func (e *Env) AssertBlocked(issue *types.Issue) {
	e.t.Helper()
	ids := e.GetReadyIDs()
	if ids[issue.ID] {
		e.t.Errorf("expected %s (%s) to be blocked, but it was ready", issue.ID, issue.Title)
	}
}
