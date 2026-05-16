// Storage-only HookFiringStore decorator test (be-l7t.1 §5).
//
// Builds a stub that implements ONLY the narrow Storage interface — not
// DoltStorage and none of the capability sub-interfaces. The post-be-l7t.1
// HookFiringStore embeds Storage so this stub must compile and the
// decorator must satisfy Storage. Mutations on the decorator must invoke
// the inner stub. Forward-compat check for the future Postgres backend.
package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// storageOnlyStub implements ONLY the narrow Storage interface. It does
// NOT satisfy DoltStorage or any capability sub-interface. The decorator
// must work against this minimal surface.
type storageOnlyStub struct {
	created    []*types.Issue
	updated    []string
	closed     []string
	deps       []*types.Dependency
	labels     map[string][]string
	comments   []string
	rollbackTx bool
}

func newStorageOnlyStub() *storageOnlyStub {
	return &storageOnlyStub{labels: map[string][]string{}}
}

// — Storage core —

func (s *storageOnlyStub) CreateIssue(_ context.Context, issue *types.Issue, _ string) error {
	s.created = append(s.created, issue)
	return nil
}
func (s *storageOnlyStub) CreateIssues(_ context.Context, issues []*types.Issue, _ string) error {
	s.created = append(s.created, issues...)
	return nil
}
func (s *storageOnlyStub) GetIssue(_ context.Context, id string) (*types.Issue, error) {
	for _, i := range s.created {
		if i.ID == id {
			return i, nil
		}
	}
	return &types.Issue{ID: id}, nil
}
func (s *storageOnlyStub) GetIssueByExternalRef(context.Context, string) (*types.Issue, error) {
	return nil, storage.ErrNotFound
}
func (s *storageOnlyStub) GetIssuesByIDs(context.Context, []string) ([]*types.Issue, error) {
	return nil, nil
}
func (s *storageOnlyStub) UpdateIssue(_ context.Context, id string, _ map[string]interface{}, _ string) error {
	s.updated = append(s.updated, id)
	return nil
}
func (s *storageOnlyStub) ReopenIssue(_ context.Context, id string, _, _ string) error {
	s.updated = append(s.updated, id)
	return nil
}
func (s *storageOnlyStub) UpdateIssueType(_ context.Context, id string, _, _ string) error {
	s.updated = append(s.updated, id)
	return nil
}
func (s *storageOnlyStub) CloseIssue(_ context.Context, id string, _, _, _ string) error {
	s.closed = append(s.closed, id)
	return nil
}
func (s *storageOnlyStub) DeleteIssue(context.Context, string) error { return nil }
func (s *storageOnlyStub) SearchIssues(context.Context, string, types.IssueFilter) ([]*types.Issue, error) {
	return nil, nil
}

func (s *storageOnlyStub) AddDependency(_ context.Context, dep *types.Dependency, _ string) error {
	s.deps = append(s.deps, dep)
	return nil
}
func (s *storageOnlyStub) RemoveDependency(context.Context, string, string, string) error { return nil }
func (s *storageOnlyStub) GetDependencies(context.Context, string) ([]*types.Issue, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetDependents(context.Context, string) ([]*types.Issue, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetDependenciesWithMetadata(context.Context, string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetDependentsWithMetadata(context.Context, string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetDependencyTree(context.Context, string, int, bool, bool) ([]*types.TreeNode, error) {
	return nil, nil
}

func (s *storageOnlyStub) AddLabel(_ context.Context, issueID, label, _ string) error {
	s.labels[issueID] = append(s.labels[issueID], label)
	return nil
}
func (s *storageOnlyStub) RemoveLabel(context.Context, string, string, string) error { return nil }
func (s *storageOnlyStub) GetLabels(context.Context, string) ([]string, error)       { return nil, nil }
func (s *storageOnlyStub) GetIssuesByLabel(context.Context, string) ([]*types.Issue, error) {
	return nil, nil
}

func (s *storageOnlyStub) GetReadyWork(context.Context, types.WorkFilter) ([]*types.Issue, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetBlockedIssues(context.Context, types.WorkFilter) ([]*types.BlockedIssue, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetEpicsEligibleForClosure(context.Context) ([]*types.EpicStatus, error) {
	return nil, nil
}

func (s *storageOnlyStub) ListWisps(context.Context, types.WispFilter) ([]*types.Issue, error) {
	return nil, nil
}

func (s *storageOnlyStub) AddIssueComment(_ context.Context, issueID, _, _ string) (*types.Comment, error) {
	s.comments = append(s.comments, issueID)
	return &types.Comment{IssueID: issueID}, nil
}
func (s *storageOnlyStub) GetIssueComments(context.Context, string) ([]*types.Comment, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetEvents(context.Context, string, int) ([]*types.Event, error) {
	return nil, nil
}
func (s *storageOnlyStub) GetAllEventsSince(context.Context, time.Time) ([]*types.Event, error) {
	return nil, nil
}

// — Iter* stubs (be-jaavsb / be-yinl4d) —
//
// All Iter* methods return an empty SliceIter; the storage-only stub does
// not retain an issue corpus so the iterators have nothing to walk. The
// tests that exercise the decorator do not iterate, they just need the
// stub to satisfy the Storage interface.

func (s *storageOnlyStub) IterIssues(context.Context, string, types.IssueFilter) (storage.Iter[types.Issue], error) {
	return storage.NewSliceIter[types.Issue](nil), nil
}
func (s *storageOnlyStub) IterDependentsWithMetadata(context.Context, string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	return storage.NewSliceIter[types.IssueWithDependencyMetadata](nil), nil
}
func (s *storageOnlyStub) IterDependenciesWithMetadata(context.Context, string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	return storage.NewSliceIter[types.IssueWithDependencyMetadata](nil), nil
}
func (s *storageOnlyStub) IterIssueComments(context.Context, string) (storage.Iter[types.Comment], error) {
	return storage.NewSliceIter[types.Comment](nil), nil
}
func (s *storageOnlyStub) IterEvents(context.Context, string, int) (storage.Iter[types.Event], error) {
	return storage.NewSliceIter[types.Event](nil), nil
}
func (s *storageOnlyStub) IterAllEventsSince(context.Context, time.Time) (storage.Iter[types.Event], error) {
	return storage.NewSliceIter[types.Event](nil), nil
}
func (s *storageOnlyStub) IterReadyWork(context.Context, types.WorkFilter) (storage.Iter[types.Issue], error) {
	return storage.NewSliceIter[types.Issue](nil), nil
}
func (s *storageOnlyStub) IterBlockedIssues(context.Context, types.WorkFilter) (storage.Iter[types.BlockedIssue], error) {
	return storage.NewSliceIter[types.BlockedIssue](nil), nil
}
func (s *storageOnlyStub) IterWisps(context.Context, types.WispFilter) (storage.Iter[types.Issue], error) {
	return storage.NewSliceIter[types.Issue](nil), nil
}

func (s *storageOnlyStub) GetStatistics(context.Context) (*types.Statistics, error) { return nil, nil }
func (s *storageOnlyStub) SetConfig(context.Context, string, string) error          { return nil }
func (s *storageOnlyStub) GetConfig(context.Context, string) (string, error)        { return "", nil }
func (s *storageOnlyStub) GetAllConfig(context.Context) (map[string]string, error) {
	return nil, nil
}
func (s *storageOnlyStub) SetLocalMetadata(context.Context, string, string) error { return nil }
func (s *storageOnlyStub) GetLocalMetadata(context.Context, string) (string, error) {
	return "", nil
}

func (s *storageOnlyStub) RunInTransaction(_ context.Context, _ string, fn func(tx storage.Transaction) error) error {
	tx := &storageOnlyStubTx{stub: s}
	if err := fn(tx); err != nil {
		return err
	}
	if s.rollbackTx {
		return errSimulatedRollback
	}
	return nil
}

func (s *storageOnlyStub) MergeSlotCreate(context.Context, string) (*types.Issue, error) {
	return nil, nil
}
func (s *storageOnlyStub) MergeSlotCheck(context.Context) (*storage.MergeSlotStatus, error) {
	return nil, nil
}
func (s *storageOnlyStub) MergeSlotAcquire(context.Context, string, string, bool) (*storage.MergeSlotResult, error) {
	return nil, nil
}
func (s *storageOnlyStub) MergeSlotRelease(context.Context, string, string) error { return nil }

func (s *storageOnlyStub) SlotSet(context.Context, string, string, string, string) error { return nil }
func (s *storageOnlyStub) SlotGet(context.Context, string, string) (string, error) {
	return "", nil
}
func (s *storageOnlyStub) SlotClear(context.Context, string, string, string) error { return nil }

func (s *storageOnlyStub) Close() error { return nil }

// — Transaction stub —

type storageOnlyStubTx struct {
	stub *storageOnlyStub
}

func (t *storageOnlyStubTx) CreateIssue(_ context.Context, issue *types.Issue, _ string) error {
	t.stub.created = append(t.stub.created, issue)
	return nil
}
func (t *storageOnlyStubTx) CreateIssues(_ context.Context, issues []*types.Issue, _ string) error {
	t.stub.created = append(t.stub.created, issues...)
	return nil
}
func (t *storageOnlyStubTx) UpdateIssue(_ context.Context, id string, _ map[string]interface{}, _ string) error {
	t.stub.updated = append(t.stub.updated, id)
	return nil
}
func (t *storageOnlyStubTx) CloseIssue(_ context.Context, id string, _, _, _ string) error {
	t.stub.closed = append(t.stub.closed, id)
	return nil
}
func (t *storageOnlyStubTx) DeleteIssue(context.Context, string) error { return nil }
func (t *storageOnlyStubTx) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return t.stub.GetIssue(ctx, id)
}
func (t *storageOnlyStubTx) SearchIssues(context.Context, string, types.IssueFilter) ([]*types.Issue, error) {
	return nil, nil
}

func (t *storageOnlyStubTx) AddDependency(_ context.Context, dep *types.Dependency, _ string) error {
	t.stub.deps = append(t.stub.deps, dep)
	return nil
}
func (t *storageOnlyStubTx) AddDependencyWithOptions(_ context.Context, dep *types.Dependency, _ string, _ storage.DependencyAddOptions) error {
	t.stub.deps = append(t.stub.deps, dep)
	return nil
}
func (t *storageOnlyStubTx) RemoveDependency(context.Context, string, string, string) error {
	return nil
}
func (t *storageOnlyStubTx) GetDependencyRecords(context.Context, string) ([]*types.Dependency, error) {
	return nil, nil
}

func (t *storageOnlyStubTx) AddLabel(_ context.Context, issueID, label, _ string) error {
	t.stub.labels[issueID] = append(t.stub.labels[issueID], label)
	return nil
}
func (t *storageOnlyStubTx) RemoveLabel(context.Context, string, string, string) error { return nil }
func (t *storageOnlyStubTx) GetLabels(context.Context, string) ([]string, error) {
	return nil, nil
}
func (t *storageOnlyStubTx) SetConfig(context.Context, string, string) error     { return nil }
func (t *storageOnlyStubTx) GetConfig(context.Context, string) (string, error)   { return "", nil }
func (t *storageOnlyStubTx) SetMetadata(context.Context, string, string) error   { return nil }
func (t *storageOnlyStubTx) GetMetadata(context.Context, string) (string, error) { return "", nil }
func (t *storageOnlyStubTx) SetLocalMetadata(context.Context, string, string) error {
	return nil
}
func (t *storageOnlyStubTx) GetLocalMetadata(context.Context, string) (string, error) {
	return "", nil
}
func (t *storageOnlyStubTx) AddComment(_ context.Context, issueID, _, _ string) error {
	t.stub.comments = append(t.stub.comments, issueID)
	return nil
}
func (t *storageOnlyStubTx) ImportIssueComment(_ context.Context, issueID, _, _ string, _ time.Time) (*types.Comment, error) {
	t.stub.comments = append(t.stub.comments, issueID)
	return &types.Comment{IssueID: issueID}, nil
}
func (t *storageOnlyStubTx) GetIssueComments(context.Context, string) ([]*types.Comment, error) {
	return nil, nil
}

var errSimulatedRollback = mustErrf("simulated rollback")

func mustErrf(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

// — Tests —

// TestHookFiringStoreCompilesAgainstStorageOnly is the load-bearing
// assertion: post-be-l7t.1, the decorator embeds Storage (not DoltStorage),
// so a stub implementing ONLY Storage must satisfy NewHookFiringStore.
// If a future change re-widens the decorator's embed back to DoltStorage,
// this test stops compiling.
func TestHookFiringStoreCompilesAgainstStorageOnly(t *testing.T) {
	stub := newStorageOnlyStub()
	// Compile-time: stub satisfies Storage. (If not, the next line errors.)
	var s storage.Storage = stub
	// Compile-time: HookFiringStore wraps any Storage.
	dec := storage.NewHookFiringStore(s, nil)
	// Decorator itself satisfies Storage.
	var _ storage.Storage = dec
	// And decorator does NOT satisfy DoltStorage (Storage-only stub can't).
	if _, ok := interface{}(dec).(storage.DoltStorage); ok {
		t.Fatal("HookFiringStore unexpectedly satisfies DoltStorage when wrapping a Storage-only stub")
	}
}

func TestHookFiringStoreUnwrapReturnsStorageOnlyInner(t *testing.T) {
	stub := newStorageOnlyStub()
	dec := storage.NewHookFiringStore(stub, nil)
	inner := storage.UnwrapStore(dec)
	if inner == nil {
		t.Fatal("UnwrapStore returned nil")
	}
	// The unwrapped store must be the original stub (pointer equality).
	if inner != storage.Storage(stub) {
		t.Fatal("UnwrapStore did not return the original inner store")
	}
}

func TestHookFiringStoreMutationsForwardToInner(t *testing.T) {
	stub := newStorageOnlyStub()
	dec := storage.NewHookFiringStore(stub, nil)

	ctx := context.Background()
	if err := dec.CreateIssue(ctx, &types.Issue{ID: "test-1"}, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := dec.UpdateIssue(ctx, "test-1", map[string]interface{}{"title": "x"}, "tester"); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if err := dec.CloseIssue(ctx, "test-1", "done", "tester", ""); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if len(stub.created) != 1 || stub.created[0].ID != "test-1" {
		t.Errorf("expected one created issue test-1, got %+v", stub.created)
	}
	if len(stub.updated) != 1 || stub.updated[0] != "test-1" {
		t.Errorf("expected one updated id test-1, got %+v", stub.updated)
	}
	if len(stub.closed) != 1 || stub.closed[0] != "test-1" {
		t.Errorf("expected one closed id test-1, got %+v", stub.closed)
	}
}

func TestHookFiringStoreTransactionForwardsToInner(t *testing.T) {
	stub := newStorageOnlyStub()
	dec := storage.NewHookFiringStore(stub, nil)

	ctx := context.Background()
	err := dec.RunInTransaction(ctx, "test commit", func(tx storage.Transaction) error {
		return tx.CreateIssue(ctx, &types.Issue{ID: "tx-1"}, "tester")
	})
	if err != nil {
		t.Fatalf("RunInTransaction: %v", err)
	}
	if len(stub.created) != 1 || stub.created[0].ID != "tx-1" {
		t.Errorf("expected one created issue tx-1, got %+v", stub.created)
	}
}
