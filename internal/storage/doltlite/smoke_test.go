//go:build cgo

package doltlite_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/doltlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestSmokeCreateGetCommit(t *testing.T) {
	ctx := t.Context()
	store, err := doltlite.New(ctx, filepath.Join(t.TempDir(), ".beads"), "beads", "main")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	now := time.Now().UTC()
	issue := &types.Issue{
		ID:          "bd-test",
		Title:       "doltlite smoke",
		Description: "verify doltlite backend",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	got, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Title != issue.Title {
		t.Fatalf("title = %q, want %q", got.Title, issue.Title)
	}

	if err := store.Commit(ctx, "test: doltlite smoke"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestSmokeLabels(t *testing.T) {
	ctx := t.Context()
	store, err := doltlite.New(ctx, filepath.Join(t.TempDir(), ".beads"), "beads", "main")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	now := time.Now().UTC()
	issue := &types.Issue{
		ID:        "bd-label",
		Title:     "doltlite labels",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
		Labels:    []string{"gc:session"},
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := store.AddLabel(ctx, issue.ID, "agent:worker", "test"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetLabels: %v", err)
	}
	got := map[string]bool{}
	for _, label := range labels {
		got[label] = true
	}
	for _, want := range []string{"gc:session", "agent:worker"} {
		if !got[want] {
			t.Fatalf("labels = %v, missing %q", labels, want)
		}
	}
}

func TestSmokeChildIDAndDependencyUseSQLiteDialect(t *testing.T) {
	ctx := t.Context()
	store, err := doltlite.New(ctx, filepath.Join(t.TempDir(), ".beads"), "beads", "main")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	now := time.Now().UTC()
	parent := &types.Issue{
		ID:        "bd-parent",
		Title:     "parent",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}
	child := &types.Issue{
		ID:        "bd-parent.1",
		Title:     "child",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	if err := store.CreateIssue(ctx, child, "test"); err != nil {
		t.Fatalf("CreateIssue child: %v", err)
	}

	next, err := store.GetNextChildID(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetNextChildID: %v", err)
	}
	if next != "bd-parent.2" {
		t.Fatalf("next child ID = %q, want bd-parent.2", next)
	}

	dep := &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: parent.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	deps, err := store.GetDependencyRecords(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetDependencyRecords: %v", err)
	}
	if len(deps) != 1 || deps[0].DependsOnID != parent.ID || deps[0].Type != types.DepParentChild {
		t.Fatalf("deps = %#v, want parent-child to %s", deps, parent.ID)
	}
}

func TestSmokeVersionControl(t *testing.T) {
	ctx := t.Context()
	store, err := doltlite.New(ctx, filepath.Join(t.TempDir(), ".beads"), "beads", "main")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if err := store.Commit(ctx, "test: config"); err != nil {
		t.Fatalf("Commit config: %v", err)
	}

	branch, err := store.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}

	if err := store.Branch(ctx, "feature"); err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if err := store.Checkout(ctx, "feature"); err != nil {
		t.Fatalf("Checkout feature: %v", err)
	}
	branch, err = store.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("CurrentBranch feature: %v", err)
	}
	if branch != "feature" {
		t.Fatalf("branch = %q, want feature", branch)
	}

	branches, err := store.ListBranches(ctx)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) < 2 {
		t.Fatalf("branches = %v, want at least main and feature", branches)
	}

	if err := store.Checkout(ctx, "main"); err != nil {
		t.Fatalf("Checkout main: %v", err)
	}
	if err := store.DeleteBranch(ctx, "feature"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}

	if _, err := store.Status(ctx); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if commits, err := store.Log(ctx, 5); err != nil {
		t.Fatalf("Log: %v", err)
	} else if len(commits) == 0 {
		t.Fatal("Log returned no commits")
	}
	if hash, err := store.GetCurrentCommit(ctx); err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	} else if hash == "" {
		t.Fatal("GetCurrentCommit returned empty hash")
	}
}
