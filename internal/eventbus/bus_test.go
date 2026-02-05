package eventbus

import (
	"context"
	"fmt"
	"testing"
)

// testHandler is a configurable handler for testing.
type testHandler struct {
	id       string
	handles  []EventType
	priority int
	fn       func(ctx context.Context, event *Event, result *Result) error
}

func (h *testHandler) ID() string           { return h.id }
func (h *testHandler) Handles() []EventType { return h.handles }
func (h *testHandler) Priority() int         { return h.priority }

func (h *testHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	if h.fn != nil {
		return h.fn(ctx, event, result)
	}
	return nil
}

func TestNew(t *testing.T) {
	bus := New()
	if bus == nil {
		t.Fatal("New() returned nil")
	}
}

func TestDispatchNoHandlers(t *testing.T) {
	bus := New()
	result, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventSessionStart,
		SessionID: "test-session",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Block {
		t.Error("expected block=false with no handlers")
	}
}

func TestDispatchNilEvent(t *testing.T) {
	bus := New()
	_, err := bus.Dispatch(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}
}

func TestDispatchMatchingHandlers(t *testing.T) {
	bus := New()
	var called []string

	bus.Register(&testHandler{
		id:       "session-handler",
		handles:  []EventType{EventSessionStart, EventSessionEnd},
		priority: 10,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			called = append(called, "session-handler")
			return nil
		},
	})

	bus.Register(&testHandler{
		id:       "tool-handler",
		handles:  []EventType{EventPreToolUse},
		priority: 10,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			called = append(called, "tool-handler")
			return nil
		},
	})

	// Dispatch SessionStart — only session-handler should fire.
	_, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventSessionStart,
		SessionID: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(called) != 1 || called[0] != "session-handler" {
		t.Errorf("expected [session-handler], got %v", called)
	}
}

func TestDispatchPriorityOrder(t *testing.T) {
	bus := New()
	var order []string

	bus.Register(&testHandler{
		id:       "low-priority",
		handles:  []EventType{EventStop},
		priority: 100,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			order = append(order, "low")
			return nil
		},
	})

	bus.Register(&testHandler{
		id:       "high-priority",
		handles:  []EventType{EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			order = append(order, "high")
			return nil
		},
	})

	bus.Register(&testHandler{
		id:       "medium-priority",
		handles:  []EventType{EventStop},
		priority: 50,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			order = append(order, "medium")
			return nil
		},
	})

	_, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventStop,
		SessionID: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"high", "medium", "low"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d handlers, got %d", len(expected), len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("position %d: expected %q, got %q", i, v, order[i])
		}
	}
}

func TestDispatchResultAggregation(t *testing.T) {
	bus := New()

	bus.Register(&testHandler{
		id:       "gate-check",
		handles:  []EventType{EventPreToolUse},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			result.Block = true
			result.Reason = "blocked by gate"
			return nil
		},
	})

	bus.Register(&testHandler{
		id:       "injector",
		handles:  []EventType{EventPreToolUse},
		priority: 10,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			result.Inject = append(result.Inject, "injected message")
			result.Warnings = append(result.Warnings, "warning from injector")
			return nil
		},
	})

	result, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventPreToolUse,
		SessionID: "test",
		ToolName:  "Write",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Block {
		t.Error("expected block=true")
	}
	if result.Reason != "blocked by gate" {
		t.Errorf("expected reason %q, got %q", "blocked by gate", result.Reason)
	}
	if len(result.Inject) != 1 || result.Inject[0] != "injected message" {
		t.Errorf("unexpected inject: %v", result.Inject)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != "warning from injector" {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}
}

func TestDispatchHandlerErrorDoesNotStopChain(t *testing.T) {
	bus := New()
	var called []string

	bus.Register(&testHandler{
		id:       "failing-handler",
		handles:  []EventType{EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			called = append(called, "failing")
			return fmt.Errorf("handler error")
		},
	})

	bus.Register(&testHandler{
		id:       "working-handler",
		handles:  []EventType{EventStop},
		priority: 10,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			called = append(called, "working")
			return nil
		},
	})

	result, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventStop,
		SessionID: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(called) != 2 {
		t.Errorf("expected both handlers called, got %v", called)
	}
}

func TestDispatchContextCancellation(t *testing.T) {
	bus := New()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	bus.Register(&testHandler{
		id:       "should-not-run",
		handles:  []EventType{EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			t.Error("handler should not have been called")
			return nil
		},
	})

	_, err := bus.Dispatch(ctx, &Event{
		Type:      EventStop,
		SessionID: "test",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestRegisterMultipleEventTypes(t *testing.T) {
	bus := New()
	callCount := 0

	bus.Register(&testHandler{
		id:       "multi-handler",
		handles:  []EventType{EventSessionStart, EventSessionEnd, EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			callCount++
			return nil
		},
	})

	events := []EventType{EventSessionStart, EventSessionEnd, EventStop, EventPreToolUse}
	for _, et := range events {
		bus.Dispatch(context.Background(), &Event{Type: et, SessionID: "test"})
	}

	// Should be called 3 times (SessionStart, SessionEnd, Stop) but not PreToolUse.
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}
