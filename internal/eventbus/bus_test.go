package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
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
		JetStreamMaxMemory: 256 << 20,
		JetStreamMaxStore:  256 << 20,
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

func TestDispatchConcurrentSafety(t *testing.T) {
	bus := New()

	var callCount [3]atomic.Int64
	for i := 0; i < 3; i++ {
		idx := i
		bus.Register(&testHandler{
			id:       fmt.Sprintf("handler-%d", idx),
			handles:  []EventType{EventSessionStart, EventStop, EventPreToolUse},
			priority: idx * 10,
			fn: func(ctx context.Context, event *Event, result *Result) error {
				callCount[idx].Add(1)
				return nil
			},
		})
	}

	// Dispatch 50 events concurrently across different types.
	const goroutines = 50
	done := make(chan struct{}, goroutines)
	eventTypes := []EventType{EventSessionStart, EventStop, EventPreToolUse}

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_, err := bus.Dispatch(context.Background(), &Event{
				Type:      eventTypes[i%len(eventTypes)],
				SessionID: fmt.Sprintf("session-%d", i),
			})
			if err != nil {
				t.Errorf("goroutine %d: dispatch error: %v", i, err)
			}
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Each handler should have been called exactly 50 times (all 3 handle all 3 event types).
	for i := range callCount {
		if count := callCount[i].Load(); count != goroutines {
			t.Errorf("handler-%d: expected %d calls, got %d", i, goroutines, count)
		}
	}
}

// Decision event tests (od-k3o.15.1)

func TestDecisionEventTypes(t *testing.T) {
	// Verify all decision event type constants have expected string values.
	tests := []struct {
		et   EventType
		want string
	}{
		{EventDecisionCreated, "DecisionCreated"},
		{EventDecisionResponded, "DecisionResponded"},
		{EventDecisionEscalated, "DecisionEscalated"},
		{EventDecisionExpired, "DecisionExpired"},
	}
	for _, tt := range tests {
		if string(tt.et) != tt.want {
			t.Errorf("expected %q, got %q", tt.want, string(tt.et))
		}
	}
}

func TestDispatchConcurrentRegisterAndDispatch(t *testing.T) {
	bus := New()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Concurrently register handlers and dispatch events.
	const workers = 20
	done := make(chan struct{}, workers*2)

	// Half the workers register handlers.
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			bus.Register(&testHandler{
				id:       fmt.Sprintf("concurrent-%d", i),
				handles:  []EventType{EventStop},
				priority: i,
			})
		}(i)
	}

	// Half the workers dispatch events.
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			// This may or may not see all registered handlers — that's fine.
			// The test verifies no races/panics.
			_, err := bus.Dispatch(ctx, &Event{
				Type:      EventStop,
				SessionID: fmt.Sprintf("race-%d", i),
			})
			if err != nil {
				t.Errorf("dispatch %d: %v", i, err)
			}
		}(i)
	}

	for i := 0; i < workers*2; i++ {
		<-done
	}

	// After all registrations, verify we have the right count.
	if len(bus.Handlers()) != workers {
		t.Errorf("expected %d handlers, got %d", workers, len(bus.Handlers()))
	}
}

func TestIsDecisionEvent(t *testing.T) {
	decisionEvents := []EventType{
		EventDecisionCreated, EventDecisionResponded,
		EventDecisionEscalated, EventDecisionExpired,
	}
	for _, et := range decisionEvents {
		if !et.IsDecisionEvent() {
			t.Errorf("expected %s to be a decision event", et)
		}
	}

	hookEvents := []EventType{
		EventSessionStart, EventStop, EventPreToolUse, EventNotification,
	}
	for _, et := range hookEvents {
		if et.IsDecisionEvent() {
			t.Errorf("expected %s to NOT be a decision event", et)
		}
	}
}

func TestDecisionEventPayloadSerialization(t *testing.T) {
	payload := DecisionEventPayload{
		DecisionID:  "od-test.decision-1",
		Question:    "Which approach?",
		Urgency:     "high",
		RequestedBy: "agent-1",
		Options:     3,
		ChosenIndex: 1,
		ChosenLabel: "Option B",
		ResolvedBy:  "user@example.com",
		Rationale:   "Better for performance",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DecisionEventPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.DecisionID != payload.DecisionID {
		t.Errorf("DecisionID: got %q, want %q", decoded.DecisionID, payload.DecisionID)
	}
	if decoded.Question != payload.Question {
		t.Errorf("Question: got %q, want %q", decoded.Question, payload.Question)
	}
	if decoded.ChosenIndex != payload.ChosenIndex {
		t.Errorf("ChosenIndex: got %d, want %d", decoded.ChosenIndex, payload.ChosenIndex)
	}
	if decoded.ChosenLabel != payload.ChosenLabel {
		t.Errorf("ChosenLabel: got %q, want %q", decoded.ChosenLabel, payload.ChosenLabel)
	}
}

func TestDecisionEventPayloadOmitEmpty(t *testing.T) {
	// Verify omitempty fields are absent when zero.
	payload := DecisionEventPayload{
		DecisionID: "test-id",
		Question:   "Test?",
		Options:    2,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// These omitempty fields should be absent.
	for _, key := range []string{"chosen_index", "chosen_label", "resolved_by", "rationale"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %q to be omitted when zero", key)
		}
	}

	// These required fields should be present.
	for _, key := range []string{"decision_id", "question", "option_count"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected %q to be present", key)
		}
	}
}

func TestDispatchDecisionEvent(t *testing.T) {
	bus := New()
	var handledEvent *Event
	bus.Register(&testHandler{
		id:       "decision-handler",
		handles:  []EventType{EventDecisionCreated},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			handledEvent = event
			return nil
		},
	})

	payload := DecisionEventPayload{
		DecisionID: "test-decision",
		Question:   "Which approach?",
		Options:    3,
	}
	payloadJSON, _ := json.Marshal(payload)

	_, err := bus.Dispatch(context.Background(), &Event{
		Type: EventDecisionCreated,
		Raw:  payloadJSON,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handledEvent == nil {
		t.Fatal("decision handler was not called")
	}
	if handledEvent.Type != EventDecisionCreated {
		t.Errorf("expected type %s, got %s", EventDecisionCreated, handledEvent.Type)
	}
}

func TestDecisionEventDoesNotMatchHookHandlers(t *testing.T) {
	bus := New()
	hookCalled := false
	bus.Register(&testHandler{
		id:       "hook-only-handler",
		handles:  []EventType{EventSessionStart, EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			hookCalled = true
			return nil
		},
	})

	_, err := bus.Dispatch(context.Background(), &Event{
		Type: EventDecisionCreated,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hookCalled {
		t.Error("hook handler should not be called for decision events")
	}
}

func TestDecisionEventPublishesToJetStream(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	// Subscribe to specific event type subjects.
	subStart, err := js.SubscribeSync(SubjectForEvent(EventSessionStart), nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe SessionStart: %v", err)
	}
	defer subStart.Unsubscribe()

	subStop, err := js.SubscribeSync(SubjectForEvent(EventStop), nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe Stop: %v", err)
	}
	defer subStop.Unsubscribe()

	subTool, err := js.SubscribeSync(SubjectForEvent(EventPreToolUse), nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe PreToolUse: %v", err)
	}
	defer subTool.Unsubscribe()

	// Dispatch different event types.
	events := []struct {
		eventType EventType
		sessionID string
	}{
		{EventSessionStart, "start-1"},
		{EventStop, "stop-1"},
		{EventPreToolUse, "tool-1"},
		{EventSessionStart, "start-2"},
	}

	for _, e := range events {
		_, err := bus.Dispatch(context.Background(), &Event{
			Type:      e.eventType,
			SessionID: e.sessionID,
		})
		if err != nil {
			t.Fatalf("dispatch %s: %v", e.eventType, err)
		}
	}

	// Verify each subscription got the right messages.
	checkMessages := func(sub *nats.Subscription, expected int, label string) {
		for i := 0; i < expected; i++ {
			_, err := sub.NextMsg(2 * time.Second)
			if err != nil {
				t.Errorf("%s: expected message %d, got error: %v", label, i+1, err)
			}
		}
	}

	checkMessages(subStart, 2, "SessionStart")
	checkMessages(subStop, 1, "Stop")
	checkMessages(subTool, 1, "PreToolUse")

	// Also verify decision events publish to JetStream.
	subDecision, err := js.SubscribeSync(SubjectDecisionPrefix+">", nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe decision: %v", err)
	}
	defer subDecision.Unsubscribe()

	payload := DecisionEventPayload{
		DecisionID: "js-test-decision",
		Question:   "Which DB?",
		Options:    2,
	}
	payloadJSON, _ := json.Marshal(payload)

	_, err = bus.Dispatch(context.Background(), &Event{
		Type: EventDecisionCreated,
		Raw:  payloadJSON,
	})
	if err != nil {
		t.Fatalf("dispatch decision: %v", err)
	}

	msg, err := subDecision.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("expected JetStream decision message: %v", err)
	}

	expectedSubject := SubjectForEvent(EventDecisionCreated)
	if msg.Subject != expectedSubject {
		t.Errorf("expected subject %q, got %q", expectedSubject, msg.Subject)
	}

	var decoded DecisionEventPayload
	if err := json.Unmarshal(msg.Data, &decoded); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}
	if decoded.DecisionID != "js-test-decision" {
		t.Errorf("expected decision_id %q, got %q", "js-test-decision", decoded.DecisionID)
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

func TestJetStreamConcurrentPublish(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	sub, err := js.SubscribeSync(SubjectHookPrefix+">", nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	const numGoroutines = 20
	done := make(chan struct{}, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_, err := bus.Dispatch(context.Background(), &Event{
				Type:      EventSessionStart,
				SessionID: fmt.Sprintf("concurrent-%d", i),
			})
			if err != nil {
				t.Errorf("goroutine %d: dispatch error: %v", i, err)
			}
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Read all messages and verify each has a valid JSON payload with a unique session_id.
	seen := make(map[string]bool)
	for i := 0; i < numGoroutines; i++ {
		msg, err := sub.NextMsg(5 * time.Second)
		if err != nil {
			t.Fatalf("expected message %d, got error: %v", i+1, err)
		}

		var received Event
		if err := json.Unmarshal(msg.Data, &received); err != nil {
			t.Fatalf("message %d: unmarshal error: %v", i+1, err)
		}
		if received.SessionID == "" {
			t.Errorf("message %d: empty session_id", i+1)
		}
		if seen[received.SessionID] {
			t.Errorf("message %d: duplicate session_id %q", i+1, received.SessionID)
		}
		seen[received.SessionID] = true
	}

	if len(seen) != numGoroutines {
		t.Errorf("expected %d unique session IDs, got %d", numGoroutines, len(seen))
	}
}

func TestJetStreamHandlersAndPublishBothWork(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	// Register a handler that injects a message.
	bus.Register(&testHandler{
		id:       "marker",
		handles:  []EventType{EventPreToolUse},
		priority: 1,
		fn: func(ctx context.Context, event *Event, result *Result) error {
			result.Inject = append(result.Inject, "handler was here")
			return nil
		},
	})

	sub, err := js.SubscribeSync(SubjectForEvent(EventPreToolUse), nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	result, err := bus.Dispatch(context.Background(), &Event{
		Type:      EventPreToolUse,
		SessionID: "both-work",
		ToolName:  "Write",
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	// Verify handler result.
	if len(result.Inject) != 1 || result.Inject[0] != "handler was here" {
		t.Errorf("expected inject [\"handler was here\"], got %v", result.Inject)
	}

	// Verify JetStream received the event.
	msg, err := sub.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("expected JetStream message, got error: %v", err)
	}

	var received Event
	if err := json.Unmarshal(msg.Data, &received); err != nil {
		t.Fatalf("unmarshal JetStream message: %v", err)
	}
	if received.SessionID != "both-work" {
		t.Errorf("expected session_id %q, got %q", "both-work", received.SessionID)
	}
	if received.ToolName != "Write" {
		t.Errorf("expected tool_name %q, got %q", "Write", received.ToolName)
	}
}

func TestJetStreamEventPayloadPreservesAllFields(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	sub, err := js.SubscribeSync(SubjectHookPrefix+">", nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	original := &Event{
		Type:           EventPreToolUse,
		SessionID:      "preserve-fields",
		TranscriptPath: "/tmp/transcript.jsonl",
		CWD:            "/home/user/project",
		PermissionMode: "auto-accept",
		ToolName:       "Bash",
		ToolInput:      map[string]interface{}{"command": "ls -la", "timeout": float64(5000)},
		Model:          "claude-opus-4-6",
		AgentID:        "agent-abc-123",
	}

	_, err = bus.Dispatch(context.Background(), original)
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	msg, err := sub.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("expected JetStream message, got error: %v", err)
	}

	var received Event
	if err := json.Unmarshal(msg.Data, &received); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if received.Type != original.Type {
		t.Errorf("Type: expected %q, got %q", original.Type, received.Type)
	}
	if received.SessionID != original.SessionID {
		t.Errorf("SessionID: expected %q, got %q", original.SessionID, received.SessionID)
	}
	if received.TranscriptPath != original.TranscriptPath {
		t.Errorf("TranscriptPath: expected %q, got %q", original.TranscriptPath, received.TranscriptPath)
	}
	if received.CWD != original.CWD {
		t.Errorf("CWD: expected %q, got %q", original.CWD, received.CWD)
	}
	if received.PermissionMode != original.PermissionMode {
		t.Errorf("PermissionMode: expected %q, got %q", original.PermissionMode, received.PermissionMode)
	}
	if received.ToolName != original.ToolName {
		t.Errorf("ToolName: expected %q, got %q", original.ToolName, received.ToolName)
	}
	if received.Model != original.Model {
		t.Errorf("Model: expected %q, got %q", original.Model, received.Model)
	}
	if received.AgentID != original.AgentID {
		t.Errorf("AgentID: expected %q, got %q", original.AgentID, received.AgentID)
	}

	// Verify ToolInput round-trips correctly.
	if received.ToolInput == nil {
		t.Fatal("ToolInput: expected non-nil map")
	}
	if cmd, ok := received.ToolInput["command"].(string); !ok || cmd != "ls -la" {
		t.Errorf("ToolInput[command]: expected %q, got %v", "ls -la", received.ToolInput["command"])
	}
	if timeout, ok := received.ToolInput["timeout"].(float64); !ok || timeout != 5000 {
		t.Errorf("ToolInput[timeout]: expected %v, got %v", 5000, received.ToolInput["timeout"])
	}
}

func TestJetStreamSubjectRouting(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	// Subscribe to 4 specific subjects.
	eventTypes := []EventType{EventSessionStart, EventStop, EventPreToolUse, EventPostToolUse}
	subs := make(map[EventType]*nats.Subscription)

	for _, et := range eventTypes {
		sub, err := js.SubscribeSync(SubjectForEvent(et), nats.DeliverAll())
		if err != nil {
			t.Fatalf("subscribe %s: %v", et, err)
		}
		defer sub.Unsubscribe()
		subs[et] = sub
	}

	// Dispatch events: 2 SessionStart, 1 Stop, 3 PreToolUse, 1 PostToolUse.
	dispatches := []struct {
		eventType EventType
		sessionID string
	}{
		{EventSessionStart, "start-a"},
		{EventSessionStart, "start-b"},
		{EventStop, "stop-a"},
		{EventPreToolUse, "tool-a"},
		{EventPreToolUse, "tool-b"},
		{EventPreToolUse, "tool-c"},
		{EventPostToolUse, "post-a"},
	}

	for _, d := range dispatches {
		_, err := bus.Dispatch(context.Background(), &Event{
			Type:      d.eventType,
			SessionID: d.sessionID,
		})
		if err != nil {
			t.Fatalf("dispatch %s/%s: %v", d.eventType, d.sessionID, err)
		}
	}

	// Verify each subscription received the correct number of messages with correct session IDs.
	expectedCounts := map[EventType]int{
		EventSessionStart: 2,
		EventStop:         1,
		EventPreToolUse:   3,
		EventPostToolUse:  1,
	}

	for et, expectedCount := range expectedCounts {
		sub := subs[et]
		for i := 0; i < expectedCount; i++ {
			msg, err := sub.NextMsg(2 * time.Second)
			if err != nil {
				t.Errorf("%s: expected message %d, got error: %v", et, i+1, err)
				continue
			}
			// Verify the message subject matches.
			if msg.Subject != SubjectForEvent(et) {
				t.Errorf("%s: expected subject %q, got %q", et, SubjectForEvent(et), msg.Subject)
			}
		}

		// Verify no extra messages on this subscription.
		extra, err := sub.NextMsg(200 * time.Millisecond)
		if err == nil {
			t.Errorf("%s: unexpected extra message: %s", et, string(extra.Data))
		}
	}
}

func TestDispatchConcurrentWithJetStream(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	// Register handlers that count calls using atomic-safe channel writes.
	const numHandlers = 3
	handlerCalls := make([]chan struct{}, numHandlers)
	for i := 0; i < numHandlers; i++ {
		handlerCalls[i] = make(chan struct{}, 100) // buffered to avoid blocking
		ch := handlerCalls[i]
		bus.Register(&testHandler{
			id:       fmt.Sprintf("concurrent-handler-%d", i),
			handles:  []EventType{EventSessionStart, EventStop},
			priority: i * 10,
			fn: func(ctx context.Context, event *Event, result *Result) error {
				ch <- struct{}{}
				return nil
			},
		})
	}

	sub, err := js.SubscribeSync(SubjectHookPrefix+">", nats.DeliverAll())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	const numGoroutines = 30
	done := make(chan struct{}, numGoroutines)
	eventTypes := []EventType{EventSessionStart, EventStop}

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_, err := bus.Dispatch(context.Background(), &Event{
				Type:      eventTypes[i%len(eventTypes)],
				SessionID: fmt.Sprintf("concurrent-js-%d", i),
			})
			if err != nil {
				t.Errorf("goroutine %d: dispatch error: %v", i, err)
			}
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify each handler was called exactly numGoroutines times (all handle both event types).
	for i, ch := range handlerCalls {
		count := len(ch)
		if count != numGoroutines {
			t.Errorf("handler-%d: expected %d calls, got %d", i, numGoroutines, count)
		}
	}

	// Verify all JetStream messages arrived.
	seen := make(map[string]bool)
	for i := 0; i < numGoroutines; i++ {
		msg, err := sub.NextMsg(5 * time.Second)
		if err != nil {
			t.Fatalf("expected JetStream message %d, got error: %v", i+1, err)
		}
		var received Event
		if err := json.Unmarshal(msg.Data, &received); err != nil {
			t.Fatalf("message %d: unmarshal error: %v", i+1, err)
		}
		if seen[received.SessionID] {
			t.Errorf("message %d: duplicate session_id %q", i+1, received.SessionID)
		}
		seen[received.SessionID] = true
	}

	if len(seen) != numGoroutines {
		t.Errorf("expected %d unique JetStream messages, got %d", numGoroutines, len(seen))
	}
}

func TestEnsureStreamsIdempotent(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	// EnsureStreams was already called once in startTestNATS.
	// Call it again — should not fail on "stream already exists".
	if err := EnsureStreams(js); err != nil {
		t.Fatalf("second EnsureStreams call failed: %v", err)
	}

	// Call it a third time for good measure.
	if err := EnsureStreams(js); err != nil {
		t.Fatalf("third EnsureStreams call failed: %v", err)
	}

	// Verify the stream still exists and is functional.
	info, err := js.StreamInfo(StreamHookEvents)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	if info.Config.Name != StreamHookEvents {
		t.Errorf("expected stream name %q, got %q", StreamHookEvents, info.Config.Name)
	}
}
