package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

type graphApplyFakeStore struct {
	storage.DoltStorage
	issues map[string]*types.Issue
	deps   []*types.Dependency
	nextID int
}

func newGraphApplyFakeStore() *graphApplyFakeStore {
	return &graphApplyFakeStore{
		issues: make(map[string]*types.Issue),
	}
}

func (s *graphApplyFakeStore) CreateIssue(_ context.Context, issue *types.Issue, _ string) error {
	cp := *issue
	if cp.ID == "" {
		cp.ID = s.nextIssueID(&cp)
	}
	issue.ID = cp.ID
	s.issues[cp.ID] = &cp
	return nil
}

func (s *graphApplyFakeStore) GetIssue(_ context.Context, id string) (*types.Issue, error) {
	issue, ok := s.issues[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	cp := *issue
	return &cp, nil
}

func (s *graphApplyFakeStore) GetDependenciesWithMetadata(_ context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	var out []*types.IssueWithDependencyMetadata
	for _, dep := range s.deps {
		if dep.IssueID != issueID {
			continue
		}
		issue, ok := s.issues[dep.DependsOnID]
		if !ok {
			continue
		}
		cp := *issue
		out = append(out, &types.IssueWithDependencyMetadata{
			Issue:          cp,
			DependencyType: dep.Type,
		})
	}
	return out, nil
}

func (s *graphApplyFakeStore) RunInTransaction(ctx context.Context, _ string, fn func(storage.Transaction) error) error {
	txStore := s.clone()
	tx := &graphApplyFakeTx{store: txStore}
	if err := fn(tx); err != nil {
		return err
	}
	s.issues = txStore.issues
	s.deps = txStore.deps
	s.nextID = txStore.nextID
	return nil
}

func (s *graphApplyFakeStore) clone() *graphApplyFakeStore {
	cp := &graphApplyFakeStore{
		issues: make(map[string]*types.Issue, len(s.issues)),
		deps:   make([]*types.Dependency, 0, len(s.deps)),
		nextID: s.nextID,
	}
	for id, issue := range s.issues {
		issueCopy := *issue
		cp.issues[id] = &issueCopy
	}
	for _, dep := range s.deps {
		depCopy := *dep
		cp.deps = append(cp.deps, &depCopy)
	}
	return cp
}

func (s *graphApplyFakeStore) nextIssueID(issue *types.Issue) string {
	s.nextID++
	if issue.Ephemeral {
		return fmt.Sprintf("ga-wisp-%d", s.nextID)
	}
	return fmt.Sprintf("ga-%d", s.nextID)
}

type graphApplyFakeTx struct {
	storage.Transaction
	store *graphApplyFakeStore
}

func (tx *graphApplyFakeTx) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	for _, issue := range issues {
		if err := tx.store.CreateIssue(ctx, issue, actor); err != nil {
			return err
		}
	}
	return nil
}

func (tx *graphApplyFakeTx) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return tx.AddDependencyWithOptions(ctx, dep, actor, storage.DependencyAddOptions{})
}

func (tx *graphApplyFakeTx) AddDependencyWithOptions(_ context.Context, dep *types.Dependency, _ string, opts storage.DependencyAddOptions) error {
	for _, existing := range tx.store.deps {
		if existing.IssueID == dep.IssueID && existing.DependsOnID == dep.DependsOnID {
			if existing.Type == dep.Type {
				return nil
			}
			return fmt.Errorf("dependency already exists with type %s", existing.Type)
		}
	}
	if isGraphApplyFakeBlocking(dep.Type) && !opts.SkipCycleCheck && tx.hasBlockingPath(dep.DependsOnID, dep.IssueID) {
		return fmt.Errorf("adding dependency would create a cycle")
	}
	depCopy := *dep
	tx.store.deps = append(tx.store.deps, &depCopy)
	return nil
}

func (tx *graphApplyFakeTx) AddLabel(context.Context, string, string, string) error {
	return nil
}

func (tx *graphApplyFakeTx) UpdateIssue(_ context.Context, id string, updates map[string]interface{}, _ string) error {
	issue, ok := tx.store.issues[id]
	if !ok {
		return storage.ErrNotFound
	}
	if assignee, ok := updates["assignee"].(string); ok {
		issue.Assignee = assignee
	}
	return nil
}

func (tx *graphApplyFakeTx) hasBlockingPath(fromID, toID string) bool {
	seen := make(map[string]bool)
	var visit func(string) bool
	visit = func(id string) bool {
		if id == toID {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, dep := range tx.store.deps {
			if dep.IssueID != id || !isGraphApplyFakeBlocking(dep.Type) {
				continue
			}
			if visit(dep.DependsOnID) {
				return true
			}
		}
		return false
	}
	return visit(fromID)
}

func isGraphApplyFakeBlocking(depType types.DependencyType) bool {
	return depType == types.DepBlocks || depType == types.DepConditionalBlocks
}

func withGraphApplyFakeStore(t *testing.T) (context.Context, *graphApplyFakeStore) {
	t.Helper()
	ctx := context.Background()
	fakeStore := newGraphApplyFakeStore()
	oldStore, oldCtx, oldActor := store, rootCtx, actor
	store, rootCtx, actor = fakeStore, ctx, "graph-apply-test"
	t.Cleanup(func() {
		store, rootCtx, actor = oldStore, oldCtx, oldActor
	})
	return ctx, fakeStore
}

func TestExecuteGraphApplyUnitRejectsMixedLocalExternalBlockingCycle(t *testing.T) {
	ctx, fakeStore := withGraphApplyFakeStore(t)
	if err := fakeStore.CreateIssue(ctx, &types.Issue{
		ID:        "ga-existing",
		Title:     "Existing",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}, actor); err != nil {
		t.Fatalf("CreateIssue(existing): %v", err)
	}

	plan := &GraphApplyPlan{
		Nodes: []GraphApplyNode{
			{Key: "a", Title: "A", Type: "task"},
			{Key: "b", Title: "B", Type: "task"},
		},
		Edges: []GraphApplyEdge{
			{FromID: "ga-existing", ToKey: "a", Type: "blocks"},
			{FromKey: "b", ToID: "ga-existing", Type: "blocks"},
			{FromKey: "a", ToKey: "b", Type: "blocks"},
		},
	}

	_, err := executeGraphApply(ctx, plan, GraphApplyOptions{})
	if err == nil {
		t.Fatal("expected mixed local/external blocking cycle to be rejected")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %q, want cycle rejection", err.Error())
	}
}

func TestExecuteGraphApplyUnitRejectsBlockingChildToParentDuplicate(t *testing.T) {
	ctx, _ := withGraphApplyFakeStore(t)

	plan := &GraphApplyPlan{
		Nodes: []GraphApplyNode{
			{Key: "root", Title: "Root", Type: "epic"},
			{Key: "child", Title: "Child", Type: "task", ParentKey: "root"},
		},
		Edges: []GraphApplyEdge{
			{FromKey: "child", ToKey: "root"},
		},
	}

	_, err := executeGraphApply(ctx, plan, GraphApplyOptions{})
	if err == nil {
		t.Fatal("expected default blocking child-to-parent duplicate to be rejected")
	}
	if !strings.Contains(err.Error(), "parent-child") {
		t.Fatalf("error = %q, want parent-child duplicate rejection", err.Error())
	}
}

func TestExecuteGraphApplyUnitAllowsExplicitParentChildDuplicate(t *testing.T) {
	ctx, fakeStore := withGraphApplyFakeStore(t)

	plan := &GraphApplyPlan{
		Nodes: []GraphApplyNode{
			{Key: "root", Title: "Root", Type: "epic"},
			{Key: "child", Title: "Child", Type: "task", ParentKey: "root"},
		},
		Edges: []GraphApplyEdge{
			{FromKey: "child", ToKey: "root", Type: string(types.DepParentChild)},
		},
	}

	result, err := executeGraphApply(ctx, plan, GraphApplyOptions{})
	if err != nil {
		t.Fatalf("executeGraphApply: %v", err)
	}
	deps, err := fakeStore.GetDependenciesWithMetadata(ctx, result.IDs["child"])
	if err != nil {
		t.Fatalf("GetDependenciesWithMetadata(child): %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("dependency count = %d, want 1", len(deps))
	}
	if deps[0].ID != result.IDs["root"] {
		t.Fatalf("dependency target = %s, want %s", deps[0].ID, result.IDs["root"])
	}
	if deps[0].DependencyType != types.DepParentChild {
		t.Fatalf("dependency type = %s, want %s", deps[0].DependencyType, types.DepParentChild)
	}
}
