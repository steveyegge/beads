package rpc

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// mockIterServer is an in-process net/rpc server backing client tests.
// Each field controls how the corresponding RPC method behaves.
type mockIterServer struct {
	// IterIssuesStart behaviour
	startErr *RPCError // if non-nil, IterIssuesStart returns this error

	// IterIssuesNext behaviour
	nextErr   *RPCError      // if non-nil, IterIssuesNext returns this error
	nextItems []*types.Issue // items to return from IterIssuesNext
	nextDone  bool           // Done flag on the next reply

	// SearchIssues behaviour (fallback path)
	searchItems []*types.Issue
	searchErr   *RPCError

	// IterClose — no configurable behaviour needed (just succeeds)

	// blockNext: if set, IterIssuesNext blocks until this channel is closed.
	blockNext chan struct{}
}

func (m *mockIterServer) IterIssuesStart(_ *IterIssuesStartArgs, reply *IterStartReply) error {
	if m.startErr != nil {
		reply.RPCError = m.startErr
		return nil
	}
	reply.SessionID = "test-session-id"
	return nil
}

func (m *mockIterServer) IterIssuesNext(_ *IterNextArgs, reply *IterIssuesNextReply) error {
	if m.blockNext != nil {
		<-m.blockNext
	}
	if m.nextErr != nil {
		reply.RPCError = m.nextErr
		return nil
	}
	reply.Items = m.nextItems
	reply.Done = m.nextDone
	return nil
}

func (m *mockIterServer) SearchIssues(_ *SearchIssuesArgs, reply *SearchIssuesReply) error {
	if m.searchErr != nil {
		reply.RPCError = m.searchErr
		return nil
	}
	reply.Issues = m.searchItems
	return nil
}

func (m *mockIterServer) IterClose(_ *IterCloseArgs, _ *IterCloseReply) error {
	return nil
}

// dialMockServer registers srv under "daemonServer", starts serving over a
// net.Pipe, and returns a *daemonClient connected to it.
func dialMockServer(t *testing.T, srv *mockIterServer) *daemonClient {
	t.Helper()
	s := rpc.NewServer()
	if err := s.RegisterName("daemonServer", srv); err != nil {
		t.Fatalf("rpc.RegisterName: %v", err)
	}
	c1, c2 := net.Pipe()
	go s.ServeConn(c2)
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	return &daemonClient{client: rpc.NewClient(c1)}
}

// TestRPCIssueIter_FallbackOnTooMany verifies that when IterIssuesStart returns
// ErrTooManyIterators, IterIssues falls back to SearchIssues and returns a valid
// SliceIter backed by the SearchIssues result.
func TestRPCIssueIter_FallbackOnTooMany(t *testing.T) {
	want := []*types.Issue{
		{ID: "be-001", Title: "First"},
		{ID: "be-002", Title: "Second"},
	}
	srv := &mockIterServer{
		startErr:    &RPCError{Kind: "ErrTooManyIterators", Msg: storage.ErrTooManyIterators.Error()},
		searchItems: want,
	}
	dc := dialMockServer(t, srv)

	it, err := dc.IterIssues(context.Background(), "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("IterIssues returned error: %v", err)
	}
	if it == nil {
		t.Fatal("IterIssues returned nil iter")
	}
	defer func() { _ = it.Close() }()

	var got []*types.Issue
	ctx := context.Background()
	for it.Next(ctx) {
		got = append(got, it.Value())
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iter.Err after drain: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d issues, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w.ID {
			t.Errorf("item %d: got id %q, want %q", i, got[i].ID, w.ID)
		}
	}
}

// TestRPCIssueIter_ErrIterSessionNotFound verifies that when IterIssuesNext
// returns ErrIterSessionNotFound, Next() returns false and Err() wraps the
// sentinel correctly.
func TestRPCIssueIter_ErrIterSessionNotFound(t *testing.T) {
	srv := &mockIterServer{
		nextErr: &RPCError{Kind: "ErrIterSessionNotFound", Msg: storage.ErrIterSessionNotFound.Error()},
	}
	dc := dialMockServer(t, srv)

	it, err := dc.IterIssues(context.Background(), "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("IterIssues returned error: %v", err)
	}
	defer func() { _ = it.Close() }()

	ctx := context.Background()
	if it.Next(ctx) {
		t.Fatal("Next() returned true; expected false on ErrIterSessionNotFound")
	}
	if !errors.Is(it.Err(), storage.ErrIterSessionNotFound) {
		t.Errorf("Err(): got %v, want errors.Is(..., ErrIterSessionNotFound)", it.Err())
	}
}

// TestRPCIssueIter_ContextCancelDuringFetch verifies that cancelling the
// context while Next() is blocked waiting for the server causes Next() to
// return false with it.Err() == context.Canceled.
func TestRPCIssueIter_ContextCancelDuringFetch(t *testing.T) {
	blockCh := make(chan struct{})
	srv := &mockIterServer{blockNext: blockCh}
	dc := dialMockServer(t, srv)

	it, err := dc.IterIssues(context.Background(), "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("IterIssues returned error: %v", err)
	}
	defer func() { _ = it.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context while the server is blocking on IterIssuesNext.
	go func() {
		cancel()
		close(blockCh) // unblock server so it doesn't leak goroutines
	}()

	if it.Next(ctx) {
		t.Fatal("Next() returned true after context cancel")
	}
	if !errors.Is(it.Err(), context.Canceled) {
		t.Errorf("Err(): got %v, want context.Canceled", it.Err())
	}
}
