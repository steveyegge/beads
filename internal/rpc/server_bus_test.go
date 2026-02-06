package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/eventbus"
)

func TestHandleBusEmitNoBus(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()
	_ = server // bus is nil by default

	args := BusEmitArgs{
		HookType:  "SessionStart",
		EventJSON: []byte(`{"session_id":"test-123"}`),
		SessionID: "test-123",
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result BusEmitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// No bus = passthrough (no block, no inject).
	if result.Block {
		t.Error("expected no block with nil bus")
	}
	if len(result.Inject) > 0 {
		t.Errorf("expected no inject, got %v", result.Inject)
	}
}

func TestHandleBusEmitWithHandlers(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create bus with a test handler that injects content.
	bus := eventbus.New()
	bus.Register(&testBusHandler{
		id:       "test-injector",
		handles:  []eventbus.EventType{eventbus.EventSessionStart},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			result.Inject = append(result.Inject, "test injection")
			result.Warnings = append(result.Warnings, "test warning")
			return nil
		},
	})
	server.SetBus(bus)

	args := BusEmitArgs{
		HookType:  "SessionStart",
		EventJSON: []byte(`{"session_id":"test-456"}`),
		SessionID: "test-456",
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result BusEmitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.Block {
		t.Error("expected no block")
	}
	if len(result.Inject) != 1 || result.Inject[0] != "test injection" {
		t.Errorf("unexpected inject: %v", result.Inject)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != "test warning" {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}
}

func TestHandleBusEmitBlock(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()
	bus.Register(&testBusHandler{
		id:       "test-blocker",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			result.Block = true
			result.Reason = "session not ready"
			return nil
		},
	})
	server.SetBus(bus)

	args := BusEmitArgs{
		HookType:  "Stop",
		EventJSON: []byte(`{"session_id":"test-789"}`),
		SessionID: "test-789",
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result BusEmitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !result.Block {
		t.Error("expected block=true")
	}
	if result.Reason != "session not ready" {
		t.Errorf("expected reason 'session not ready', got %q", result.Reason)
	}
}

func TestHandleBusEmitMissingHookType(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()
	server.SetBus(bus)

	args := BusEmitArgs{
		HookType: "", // Missing
	}

	_, err := client.Execute(OpBusEmit, args)
	if err == nil {
		t.Error("expected error for missing hook_type")
	}
}

func TestHandleBusStatusNoBus(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := client.Execute(OpBusStatus, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result BusStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.HandlerCount != 0 {
		t.Errorf("expected 0 handlers, got %d", result.HandlerCount)
	}
}

func TestHandleBusStatusWithHandlers(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()
	bus.Register(&testBusHandler{id: "h1", handles: []eventbus.EventType{eventbus.EventStop}, priority: 1})
	bus.Register(&testBusHandler{id: "h2", handles: []eventbus.EventType{eventbus.EventSessionStart}, priority: 2})
	server.SetBus(bus)

	resp, err := client.Execute(OpBusStatus, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result BusStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.HandlerCount != 2 {
		t.Errorf("expected 2 handlers, got %d", result.HandlerCount)
	}
}

func TestHandleBusHandlersEmpty(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()
	server.SetBus(bus)

	resp, err := client.Execute(OpBusHandlers, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result BusHandlersResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Handlers) != 0 {
		t.Errorf("expected 0 handlers, got %d", len(result.Handlers))
	}
}

func TestHandleBusHandlersWithRegistered(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()
	bus.Register(&testBusHandler{
		id:       "test-gate",
		handles:  []eventbus.EventType{eventbus.EventStop, eventbus.EventPreToolUse},
		priority: 20,
	})
	server.SetBus(bus)

	resp, err := client.Execute(OpBusHandlers, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result BusHandlersResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(result.Handlers))
	}

	h := result.Handlers[0]
	if h.ID != "test-gate" {
		t.Errorf("expected ID 'test-gate', got %q", h.ID)
	}
	if h.Priority != 20 {
		t.Errorf("expected priority 20, got %d", h.Priority)
	}
	if len(h.Handles) != 2 {
		t.Errorf("expected 2 event types, got %d", len(h.Handles))
	}
}

// testBusHandler implements eventbus.Handler for RPC tests.
type testBusHandler struct {
	id       string
	handles  []eventbus.EventType
	priority int
	fn       func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error
}

func (h *testBusHandler) ID() string                { return h.id }
func (h *testBusHandler) Handles() []eventbus.EventType { return h.handles }
func (h *testBusHandler) Priority() int              { return h.priority }
func (h *testBusHandler) Handle(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
	if h.fn != nil {
		return h.fn(ctx, event, result)
	}
	return nil
}
