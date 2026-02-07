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

func TestHandleBusStatusWithNATSHealthFn(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()
	bus.Register(&testBusHandler{id: "h1", handles: []eventbus.EventType{eventbus.EventStop}, priority: 1})
	server.SetBus(bus)

	// Set a NATS health callback that returns synthetic health data.
	server.SetNATSHealthFn(func() NATSHealthInfo {
		return NATSHealthInfo{
			Enabled:     true,
			Status:      "running",
			Port:        4222,
			Connections: 3,
			JetStream:   true,
			Streams:     2,
		}
	})

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

	// Handler count comes from the bus.
	if result.HandlerCount != 1 {
		t.Errorf("expected 1 handler, got %d", result.HandlerCount)
	}

	// NATS fields come from the health callback.
	if !result.NATSEnabled {
		t.Error("expected NATSEnabled=true")
	}
	if result.NATSStatus != "running" {
		t.Errorf("expected NATSStatus 'running', got %q", result.NATSStatus)
	}
	if result.NATSPort != 4222 {
		t.Errorf("expected NATSPort 4222, got %d", result.NATSPort)
	}
	if result.Connections != 3 {
		t.Errorf("expected Connections 3, got %d", result.Connections)
	}
	if !result.JetStream {
		t.Error("expected JetStream=true")
	}
	if result.Streams != 2 {
		t.Errorf("expected Streams 2, got %d", result.Streams)
	}
}

func TestHandleBusEmitEventJSONParsing(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Register a handler that captures the event to verify field parsing.
	var capturedEvent *eventbus.Event
	bus := eventbus.New()
	bus.Register(&testBusHandler{
		id:       "field-checker",
		handles:  []eventbus.EventType{eventbus.EventPreToolUse},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			capturedEvent = event
			return nil
		},
	})
	server.SetBus(bus)

	// EventJSON includes fields that should be parsed into Event struct.
	eventJSON := `{
		"hook_event_name": "PreToolUse",
		"session_id": "sess-abc",
		"cwd": "/home/user/project",
		"tool_name": "Bash",
		"permission_mode": "auto",
		"model": "claude-opus-4-6"
	}`

	args := BusEmitArgs{
		HookType:  "PreToolUse",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-abc",
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	if capturedEvent == nil {
		t.Fatal("handler was not called")
	}

	// Type should come from args.HookType, not from the JSON hook_event_name.
	if string(capturedEvent.Type) != "PreToolUse" {
		t.Errorf("expected Type 'PreToolUse', got %q", capturedEvent.Type)
	}
	if capturedEvent.CWD != "/home/user/project" {
		t.Errorf("expected CWD '/home/user/project', got %q", capturedEvent.CWD)
	}
	if capturedEvent.ToolName != "Bash" {
		t.Errorf("expected ToolName 'Bash', got %q", capturedEvent.ToolName)
	}
	if capturedEvent.PermissionMode != "auto" {
		t.Errorf("expected PermissionMode 'auto', got %q", capturedEvent.PermissionMode)
	}
	if capturedEvent.Model != "claude-opus-4-6" {
		t.Errorf("expected Model 'claude-opus-4-6', got %q", capturedEvent.Model)
	}
	// Raw should be preserved.
	if len(capturedEvent.Raw) == 0 {
		t.Error("expected Raw to be preserved")
	}
}

func TestHandleBusEmitEventJSONTypeOverride(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Verify that args.HookType takes precedence even when EventJSON has a different hook_event_name.
	var capturedType eventbus.EventType
	bus := eventbus.New()
	bus.Register(&testBusHandler{
		id:       "type-checker",
		handles:  []eventbus.EventType{eventbus.EventSessionStart},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			capturedType = event.Type
			return nil
		},
	})
	server.SetBus(bus)

	// EventJSON has a DIFFERENT hook_event_name than args.HookType.
	args := BusEmitArgs{
		HookType:  "SessionStart",
		EventJSON: json.RawMessage(`{"hook_event_name":"Stop","session_id":"test"}`),
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	if string(capturedType) != "SessionStart" {
		t.Errorf("expected Type 'SessionStart' (from args), got %q", capturedType)
	}
}

func TestExportMutexSingleFlight(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Manually set exportInProgress to simulate a concurrent export.
	if !server.exportInProgress.CompareAndSwap(false, true) {
		t.Fatal("expected exportInProgress to be false initially")
	}

	// Now call handleSyncExport directly — it should see the guard and return skipped.
	req := &Request{Operation: OpSyncExport}
	resp := server.handleSyncExport(req)

	if !resp.Success {
		t.Fatalf("expected success (skipped), got error: %s", resp.Error)
	}

	var result SyncExportResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !result.Skipped {
		t.Error("expected Skipped=true when export is already in progress")
	}
	if result.Message != "export already in progress" {
		t.Errorf("expected 'export already in progress', got %q", result.Message)
	}

	// Clean up: release the guard.
	server.exportInProgress.Store(false)
}

func TestExportMutexHandleExportSingleFlight(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Set the guard.
	server.exportInProgress.Store(true)

	// Call handleExport — it should return the skipped JSON.
	req := &Request{
		Operation: OpExport,
		Args:      json.RawMessage(`{"output_path":"/tmp/test.jsonl"}`),
	}
	resp := server.handleExport(req)

	if !resp.Success {
		t.Fatalf("expected success (skipped), got error: %s", resp.Error)
	}

	// The handleExport returns raw JSON for the skip case.
	var raw map[string]interface{}
	if err := json.Unmarshal(resp.Data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["skipped"] != true {
		t.Errorf("expected skipped=true, got %v", raw["skipped"])
	}

	server.exportInProgress.Store(false)
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
