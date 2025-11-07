package api

import (
	"context"
	"testing"
	"time"
)

func TestLocalEventDispatcherPublish(t *testing.T) {
	t.Parallel()

	dispatcher := NewLocalEventDispatcher(2)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ch1, err := dispatcher.Subscribe(ctx1)
	if err != nil {
		t.Fatalf("Subscribe ctx1: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch2, err := dispatcher.Subscribe(ctx2)
	if err != nil {
		t.Fatalf("Subscribe ctx2: %v", err)
	}

	event := IssueEvent{
		Type: EventTypeUpdated,
		Issue: IssueSummary{
			ID:        "ui-100",
			Title:     "Updated issue",
			Status:    string(EventTypeUpdated),
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}

	dispatcher.Publish(event)

	assertEvent := func(ch <-chan IssueEvent) {
		select {
		case received := <-ch:
			if received.Issue.ID != event.Issue.ID || received.Type != event.Type {
				t.Fatalf("unexpected event: %+v", received)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timed out waiting for dispatched event")
		}
	}

	assertEvent(ch1)
	assertEvent(ch2)

	cancel1()
	cancel2()

	select {
	case _, ok := <-ch1:
		if ok {
			t.Fatalf("expected channel 1 to be closed")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for channel 1 close")
	}
	select {
	case _, ok := <-ch2:
		if ok {
			t.Fatalf("expected channel 2 to be closed")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for channel 2 close")
	}
}
