package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
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

// startTestNATS starts an embedded NATS server with JetStream for testing.
// Returns the server, JetStream context, and a cleanup function.
func startTestNATS(t *testing.T) (*natsserver.Server, nats.JetStreamContext, func()) {
	t.Helper()
	dir := t.TempDir()
	opts := &natsserver.Options{
		Port:               -1, // random available port
		JetStream:          true,
		JetStreamMaxMemory: 128 << 20,
		JetStreamMaxStore:  128 << 20,
		StoreDir:           dir,
		NoLog:              true,
		NoSigs:             true,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("create test NATS server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("test NATS server failed to start")
	}

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		ns.Shutdown()
		t.Fatalf("connect to test NATS: %v", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		ns.Shutdown()
		t.Fatalf("get JetStream context: %v", err)
	}

	if err := EnsureStreams(js); err != nil {
		nc.Close()
		ns.Shutdown()
		t.Fatalf("create streams: %v", err)
	}

	cleanup := func() {
		nc.Drain()
		nc.Close()
		ns.Shutdown()
	}
	return ns, js, cleanup
}

func TestJetStreamEnabled(t *testing.T) {
	bus := New()
	if bus.JetStreamEnabled() {
		t.Error("expected JetStreamEnabled=false before SetJetStream")
	}

	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus.SetJetStream(js)
	if !bus.JetStreamEnabled() {
		t.Error("expected JetStreamEnabled=true after SetJetStream")
	}

	bus.SetJetStream(nil)
	if bus.JetStreamEnabled() {
		t.Error("expected JetStreamEnabled=false after SetJetStream(nil)")
	}
}

func TestDispatchPublishesToJetStream(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	// Subscribe to the stream to verify messages arrive.
	sub, err := js.SubscribeSync(SubjectHookPrefix+">", nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// Dispatch an event — should be published to JetStream.
	event := &Event{
		Type:      EventSessionStart,
		SessionID: "test-js-publish",
	}
	result, err := bus.Dispatch(context.Background(), event)
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if result.Block {
		t.Error("unexpected block")
	}

	// Read from JetStream.
	msg, err := sub.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("expected JetStream message, got error: %v", err)
	}

	// Verify the subject.
	expectedSubject := SubjectForEvent(EventSessionStart)
	if msg.Subject != expectedSubject {
		t.Errorf("expected subject %q, got %q", expectedSubject, msg.Subject)
	}

	// Verify the payload is valid JSON with the session ID.
	var received Event
	if err := json.Unmarshal(msg.Data, &received); err != nil {
		t.Fatalf("unmarshal JetStream message: %v", err)
	}
	if received.SessionID != "test-js-publish" {
		t.Errorf("expected session_id %q, got %q", "test-js-publish", received.SessionID)
	}
}

func TestDispatchWithRawJSON(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	sub, err := js.SubscribeSync(SubjectHookPrefix+">", nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// Dispatch with Raw JSON — should use Raw bytes instead of marshaling.
	rawJSON := json.RawMessage(`{"custom_field":"test_value","session_id":"raw-session"}`)
	event := &Event{
		Type:      EventPreToolUse,
		SessionID: "raw-session",
		Raw:       rawJSON,
	}
	_, err = bus.Dispatch(context.Background(), event)
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	msg, err := sub.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("expected message: %v", err)
	}

	// Raw bytes should be published as-is.
	if string(msg.Data) != string(rawJSON) {
		t.Errorf("expected raw JSON %q, got %q", string(rawJSON), string(msg.Data))
	}
}

func TestDispatchWithoutJetStreamStillWorks(t *testing.T) {
	bus := New()
	// No SetJetStream — JetStream is nil.
	var handlerCalled bool
	bus.Register(&testHandler{
		id:       "test-handler",
		handles:  []EventType{EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			handlerCalled = true
			return nil
		},
	})

	result, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventStop,
		SessionID: "no-nats",
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if !handlerCalled {
		t.Error("handler should have been called")
	}
	if result.Block {
		t.Error("unexpected block")
	}
}

func TestJetStreamPublishErrorDoesNotAffectResult(t *testing.T) {
	_, js, cleanup := startTestNATS(t)

	bus := New()
	bus.SetJetStream(js)

	// Register a handler that injects content.
	bus.Register(&testHandler{
		id:       "injector",
		handles:  []EventType{EventSessionStart},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			result.Inject = append(result.Inject, "injected content")
			return nil
		},
	})

	// Shut down NATS before dispatch — publish will fail but dispatch should succeed.
	cleanup()

	result, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventSessionStart,
		SessionID: "nats-down",
	})
	if err != nil {
		t.Fatalf("dispatch should succeed even with NATS down: %v", err)
	}
	if len(result.Inject) != 1 || result.Inject[0] != "injected content" {
		t.Errorf("expected injected content, got %v", result.Inject)
	}
}
