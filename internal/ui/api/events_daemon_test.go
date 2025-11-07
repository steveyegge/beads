package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

type stubWatchEventsClient struct {
	stream       chan rpc.IssueEvent
	cancelCalled int32
	err          error
}

func (s *stubWatchEventsClient) WatchEvents(ctx context.Context, args *rpc.WatchEventsArgs) (<-chan rpc.IssueEvent, func(), error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	if s.stream == nil {
		s.stream = make(chan rpc.IssueEvent, 1)
	}
	cancel := func() {
		atomic.StoreInt32(&s.cancelCalled, 1)
	}
	return s.stream, cancel, nil
}

func TestNewDaemonEventSourceNilClient(t *testing.T) {
	if src := NewDaemonEventSource(nil); src != nil {
		t.Fatalf("expected nil source when client missing")
	}
}

func TestDaemonEventSourceSubscribe(t *testing.T) {
	client := &stubWatchEventsClient{
		stream: make(chan rpc.IssueEvent, 1),
	}

	source := NewDaemonEventSource(client)
	if source == nil {
		t.Fatalf("expected event source instance")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := source.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}

	issue := rpc.IssueEventRecord{
		ID:        "ui-1",
		Title:     "Updated issue",
		Status:    types.StatusInProgress,
		IssueType: types.TypeBug,
		Priority:  2,
		Labels:    []string{"ui", "critical"},
		UpdatedAt: "",
	}

	client.stream <- rpc.IssueEvent{
		Type:  rpc.IssueEventUpdated,
		Issue: issue,
	}
	close(client.stream)

	select {
	case evt := <-events:
		if evt.Type != EventTypeUpdated {
			t.Fatalf("event type = %s, want %s", evt.Type, EventTypeUpdated)
		}
		if evt.Issue.ID != "ui-1" || evt.Issue.Title != "Updated issue" {
			t.Fatalf("unexpected issue summary: %+v", evt.Issue)
		}
		if evt.Issue.UpdatedAt == "" {
			t.Fatalf("expected synthesized UpdatedAt timestamp")
		}
		if len(evt.Issue.Labels) != 2 || evt.Issue.Labels[0] != "ui" {
			t.Fatalf("labels not copied: %+v", evt.Issue.Labels)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for daemon event")
	}

	cancel()

	// Ensure cleanup invoked cancel function.
	if !eventually(func() bool { return atomic.LoadInt32(&client.cancelCalled) == 1 }, 500*time.Millisecond) {
		t.Fatalf("expected cancel callback to be invoked")
	}
}

func eventually(fn func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fn()
}
