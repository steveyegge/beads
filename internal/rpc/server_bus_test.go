package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestBusEmitMultiHandlerPriorityChain(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Shared slice to track call order across handlers.
	var mu sync.Mutex
	var callOrder []string

	bus := eventbus.New()

	// Register 3 handlers with different priorities for EventStop.
	// Priority order: 5 < 10 < 20 (lowest number = called first).
	bus.Register(&testBusHandler{
		id:       "medium-priority",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 10,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			mu.Lock()
			callOrder = append(callOrder, "medium")
			mu.Unlock()
			result.Inject = append(result.Inject, "medium-inject")
			result.Warnings = append(result.Warnings, "medium-warning")
			return nil
		},
	})
	bus.Register(&testBusHandler{
		id:       "high-priority",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 5,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			mu.Lock()
			callOrder = append(callOrder, "high")
			mu.Unlock()
			result.Inject = append(result.Inject, "high-inject")
			return nil
		},
	})
	bus.Register(&testBusHandler{
		id:       "low-priority",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 20,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			mu.Lock()
			callOrder = append(callOrder, "low")
			mu.Unlock()
			result.Warnings = append(result.Warnings, "low-warning")
			return nil
		},
	})
	server.SetBus(bus)

	args := BusEmitArgs{
		HookType:  "Stop",
		EventJSON: json.RawMessage(`{"session_id":"priority-test"}`),
		SessionID: "priority-test",
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

	// All 3 handlers should have been called.
	mu.Lock()
	order := make([]string, len(callOrder))
	copy(order, callOrder)
	mu.Unlock()

	if len(order) != 3 {
		t.Fatalf("expected 3 handler calls, got %d: %v", len(order), order)
	}

	// Verify priority ordering: high (5) -> medium (10) -> low (20).
	expectedOrder := []string{"high", "medium", "low"}
	for i, expected := range expectedOrder {
		if order[i] != expected {
			t.Errorf("call order[%d]: expected %q, got %q (full order: %v)", i, expected, order[i], order)
		}
	}

	// Verify aggregated inject from all handlers.
	if len(result.Inject) != 2 {
		t.Errorf("expected 2 injects, got %d: %v", len(result.Inject), result.Inject)
	}

	// Verify aggregated warnings from all handlers.
	if len(result.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestBusEmitHandlerErrorContinuesChain(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()

	// First handler (higher priority) returns an error.
	bus.Register(&testBusHandler{
		id:       "error-handler",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			return fmt.Errorf("simulated handler failure")
		},
	})

	// Second handler (lower priority) injects content.
	bus.Register(&testBusHandler{
		id:       "inject-handler",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 10,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			result.Inject = append(result.Inject, "survived-error")
			return nil
		},
	})
	server.SetBus(bus)

	args := BusEmitArgs{
		HookType:  "Stop",
		EventJSON: json.RawMessage(`{"session_id":"error-test"}`),
		SessionID: "error-test",
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// The RPC call should succeed — handler errors are logged, not returned.
	if !resp.Success {
		t.Fatalf("expected success (handler errors are logged, not returned), got error: %s", resp.Error)
	}

	var result BusEmitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Second handler's inject should be present.
	if len(result.Inject) != 1 || result.Inject[0] != "survived-error" {
		t.Errorf("expected inject ['survived-error'], got %v", result.Inject)
	}
}

func TestBusEmitContextTimeoutPropagation(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Set a very short request timeout on the server.
	server.requestTimeout = 50 * time.Millisecond

	var handlerCtxDone bool
	var handlerMu sync.Mutex

	bus := eventbus.New()
	bus.Register(&testBusHandler{
		id:       "slow-handler",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			// Simulate a slow handler that respects context cancellation.
			select {
			case <-ctx.Done():
				handlerMu.Lock()
				handlerCtxDone = true
				handlerMu.Unlock()
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil
			}
		},
	})
	server.SetBus(bus)

	args := BusEmitArgs{
		HookType:  "Stop",
		EventJSON: json.RawMessage(`{"session_id":"timeout-test"}`),
		SessionID: "timeout-test",
	}

	start := time.Now()
	resp, err := client.Execute(OpBusEmit, args)
	elapsed := time.Since(start)

	// The call should complete quickly, not hang for 10 seconds.
	if elapsed > 5*time.Second {
		t.Fatalf("call took %v, expected fast return due to timeout", elapsed)
	}

	// The response may be an error (context deadline exceeded) or success with
	// partial results depending on timing. Either way, it should not hang.
	if err != nil {
		// Transport error is acceptable for timeout.
		t.Logf("got transport error (acceptable): %v", err)
		return
	}

	// If we got a response, check that the context was cancelled or an error was returned.
	if !resp.Success {
		// dispatch error with context cancellation is expected.
		t.Logf("got dispatch error (expected): %s", resp.Error)
	}

	// Verify the handler saw context cancellation.
	// Give a small grace period for the goroutine to complete.
	time.Sleep(100 * time.Millisecond)
	handlerMu.Lock()
	ctxDone := handlerCtxDone
	handlerMu.Unlock()

	if !ctxDone {
		t.Error("expected handler to observe context cancellation")
	}
}

func TestBusEmitNoMatchingHandlers(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()

	// Register a handler for EventStop only.
	bus.Register(&testBusHandler{
		id:       "stop-only",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			result.Block = true
			result.Reason = "should not be called"
			result.Inject = append(result.Inject, "should not appear")
			return nil
		},
	})
	server.SetBus(bus)

	// Emit a SessionStart event — the Stop handler should NOT match.
	args := BusEmitArgs{
		HookType:  "SessionStart",
		EventJSON: json.RawMessage(`{"session_id":"nomatch-test"}`),
		SessionID: "nomatch-test",
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
		t.Error("expected no block when no handlers match")
	}
	if len(result.Inject) > 0 {
		t.Errorf("expected no inject, got %v", result.Inject)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings, got %v", result.Warnings)
	}
}

func TestBusEmitMultipleBlocksLastWins(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()

	// First handler (priority 1) sets block with reason A.
	bus.Register(&testBusHandler{
		id:       "blocker-a",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 1,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			result.Block = true
			result.Reason = "reason-A"
			return nil
		},
	})

	// Second handler (priority 10) overwrites block reason with B.
	bus.Register(&testBusHandler{
		id:       "blocker-b",
		handles:  []eventbus.EventType{eventbus.EventStop},
		priority: 10,
		fn: func(ctx context.Context, event *eventbus.Event, result *eventbus.Result) error {
			result.Block = true
			result.Reason = "reason-B"
			return nil
		},
	})
	server.SetBus(bus)

	args := BusEmitArgs{
		HookType:  "Stop",
		EventJSON: json.RawMessage(`{"session_id":"block-test"}`),
		SessionID: "block-test",
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
	// Since handlers mutate the same Result and run in priority order,
	// the last handler (priority 10, "blocker-b") overwrites the reason.
	if result.Reason != "reason-B" {
		t.Errorf("expected reason 'reason-B' (last handler wins), got %q", result.Reason)
	}
}

func TestBusStatusAfterSetBusThenNilBus(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Phase 1: Set a bus with handlers.
	bus := eventbus.New()
	bus.Register(&testBusHandler{id: "h1", handles: []eventbus.EventType{eventbus.EventStop}, priority: 1})
	bus.Register(&testBusHandler{id: "h2", handles: []eventbus.EventType{eventbus.EventSessionStart}, priority: 2})
	server.SetBus(bus)

	resp, err := client.Execute(OpBusStatus, nil)
	if err != nil {
		t.Fatalf("execute (phase 1): %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success (phase 1), got error: %s", resp.Error)
	}

	var result1 BusStatusResult
	if err := json.Unmarshal(resp.Data, &result1); err != nil {
		t.Fatalf("unmarshal (phase 1): %v", err)
	}
	if result1.HandlerCount != 2 {
		t.Errorf("phase 1: expected 2 handlers, got %d", result1.HandlerCount)
	}

	// Phase 2: Set bus to nil.
	server.SetBus(nil)

	resp, err = client.Execute(OpBusStatus, nil)
	if err != nil {
		t.Fatalf("execute (phase 2): %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success (phase 2), got error: %s", resp.Error)
	}

	var result2 BusStatusResult
	if err := json.Unmarshal(resp.Data, &result2); err != nil {
		t.Fatalf("unmarshal (phase 2): %v", err)
	}
	if result2.HandlerCount != 0 {
		t.Errorf("phase 2: expected 0 handlers after setting bus to nil, got %d", result2.HandlerCount)
	}
}

func TestBusHandlersPriorityOrdering(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	bus := eventbus.New()

	// Register handlers in non-priority order: 30, 10, 20.
	bus.Register(&testBusHandler{id: "p30", handles: []eventbus.EventType{eventbus.EventStop}, priority: 30})
	bus.Register(&testBusHandler{id: "p10", handles: []eventbus.EventType{eventbus.EventStop}, priority: 10})
	bus.Register(&testBusHandler{id: "p20", handles: []eventbus.EventType{eventbus.EventStop}, priority: 20})
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

	if len(result.Handlers) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(result.Handlers))
	}

	// BusHandlers returns handlers from bus.Handlers() which returns them in
	// registration order, NOT priority order. Verify this behavior.
	expectedIDs := []string{"p30", "p10", "p20"}
	expectedPriorities := []int{30, 10, 20}

	for i, h := range result.Handlers {
		if h.ID != expectedIDs[i] {
			t.Errorf("handler[%d]: expected ID %q, got %q", i, expectedIDs[i], h.ID)
		}
		if h.Priority != expectedPriorities[i] {
			t.Errorf("handler[%d]: expected priority %d, got %d", i, expectedPriorities[i], h.Priority)
		}
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

// ---------------------------------------------------------------------------
// Helper: setupMockBDForRPC creates a mock bd shell script for RPC tests.
// ---------------------------------------------------------------------------

func setupMockBDForRPC(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	bdPath := filepath.Join(dir, "bd")
	if err := os.WriteFile(bdPath, []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatalf("failed to write mock bd script: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// ---------------------------------------------------------------------------
// Integration tests: default handler chain (prime/gate/decision) via RPC
// ---------------------------------------------------------------------------

func TestDefaultHandlerChainSessionStart(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	setupMockBDForRPC(t, `
case "$1" in
  prime)
    printf "# Workflow Context\nReady to work"
    exit 0
    ;;
  decision)
    printf "Decision: deploy approved"
    exit 0
    ;;
  gate)
    # gate should NOT be called for SessionStart
    printf "UNEXPECTED GATE CALL"
    exit 1
    ;;
esac
exit 1
`)

	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}
	server.SetBus(bus)

	cwd := t.TempDir()
	eventJSON := fmt.Sprintf(`{"session_id":"sess-start-1","cwd":%q}`, cwd)
	args := BusEmitArgs{
		HookType:  "SessionStart",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-start-1",
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

	// Prime (priority 10) and Decision (priority 30) both handle SessionStart.
	if len(result.Inject) != 2 {
		t.Fatalf("expected 2 inject entries, got %d: %v", len(result.Inject), result.Inject)
	}

	// Prime output should come before decision output (priority 10 < 30).
	if !strings.Contains(result.Inject[0], "Workflow Context") {
		t.Errorf("expected first inject to contain prime output, got: %q", result.Inject[0])
	}
	if !strings.Contains(result.Inject[1], "Decision: deploy approved") {
		t.Errorf("expected second inject to contain decision output, got: %q", result.Inject[1])
	}

	if result.Block {
		t.Error("expected Block=false for SessionStart")
	}
}

func TestDefaultHandlerChainPreToolUseAllow(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	setupMockBDForRPC(t, `
case "$1" in
  gate)
    printf '{"decision":"allow","warnings":["review pending"]}'
    exit 0
    ;;
  prime)
    printf "UNEXPECTED PRIME CALL"
    exit 0
    ;;
  decision)
    printf "UNEXPECTED DECISION CALL"
    exit 0
    ;;
esac
exit 1
`)

	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}
	server.SetBus(bus)

	cwd := t.TempDir()
	eventJSON := fmt.Sprintf(`{"session_id":"sess-ptu-1","cwd":%q,"tool_name":"Bash"}`, cwd)
	args := BusEmitArgs{
		HookType:  "PreToolUse",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-ptu-1",
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
		t.Error("expected Block=false for allow decision")
	}

	if len(result.Warnings) != 1 || result.Warnings[0] != "review pending" {
		t.Errorf("expected warnings [\"review pending\"], got %v", result.Warnings)
	}

	// Prime and decision do NOT handle PreToolUse, so no inject.
	if len(result.Inject) != 0 {
		t.Errorf("expected no inject entries for PreToolUse, got %d: %v", len(result.Inject), result.Inject)
	}
}

func TestDefaultHandlerChainPreToolUseBlock(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	setupMockBDForRPC(t, `
case "$1" in
  gate)
    printf '{"decision":"block","reason":"gate XYZ not satisfied"}'
    exit 1
    ;;
esac
exit 1
`)

	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}
	server.SetBus(bus)

	cwd := t.TempDir()
	eventJSON := fmt.Sprintf(`{"session_id":"sess-ptu-block","cwd":%q,"tool_name":"Write"}`, cwd)
	args := BusEmitArgs{
		HookType:  "PreToolUse",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-ptu-block",
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
		t.Error("expected Block=true for block decision")
	}
	if !strings.Contains(result.Reason, "gate XYZ not satisfied") {
		t.Errorf("expected reason to contain 'gate XYZ not satisfied', got %q", result.Reason)
	}
}

func TestDefaultHandlerChainStopBlock(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	setupMockBDForRPC(t, `
case "$1" in
  gate)
    printf '{"decision":"block","reason":"session gate not met"}'
    exit 1
    ;;
esac
exit 1
`)

	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}
	server.SetBus(bus)

	cwd := t.TempDir()
	eventJSON := fmt.Sprintf(`{"session_id":"sess-stop-block","cwd":%q}`, cwd)
	args := BusEmitArgs{
		HookType:  "Stop",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-stop-block",
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
		t.Error("expected Block=true for Stop block decision")
	}
	if !strings.Contains(result.Reason, "session gate not met") {
		t.Errorf("expected reason to contain 'session gate not met', got %q", result.Reason)
	}
}

func TestDefaultHandlerChainPreCompact(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	setupMockBDForRPC(t, `
case "$1" in
  prime)
    printf "Compact workflow context refreshed"
    exit 0
    ;;
  decision)
    # decision outputs nothing — no pending decisions
    exit 0
    ;;
  gate)
    printf "UNEXPECTED GATE CALL"
    exit 1
    ;;
esac
exit 1
`)

	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}
	server.SetBus(bus)

	cwd := t.TempDir()
	eventJSON := fmt.Sprintf(`{"session_id":"sess-compact","cwd":%q}`, cwd)
	args := BusEmitArgs{
		HookType:  "PreCompact",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-compact",
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

	// Only prime should inject (decision had no output).
	if len(result.Inject) != 1 {
		t.Fatalf("expected 1 inject entry, got %d: %v", len(result.Inject), result.Inject)
	}
	if !strings.Contains(result.Inject[0], "Compact workflow context refreshed") {
		t.Errorf("expected inject to contain prime output, got: %q", result.Inject[0])
	}
	if result.Block {
		t.Error("expected Block=false for PreCompact")
	}
}

func TestDefaultHandlerChainBDNotInPath(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Set PATH to an empty directory so bd binary is not found.
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}
	server.SetBus(bus)

	cwd := t.TempDir()
	eventJSON := fmt.Sprintf(`{"session_id":"sess-nobd","cwd":%q}`, cwd)
	args := BusEmitArgs{
		HookType:  "SessionStart",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-nobd",
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Handler errors are logged, not fatal — dispatch still succeeds.
	if !resp.Success {
		t.Fatalf("expected success (handler errors are logged, not fatal), got error: %s", resp.Error)
	}

	var result BusEmitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// No inject because all handlers failed to find bd.
	if len(result.Inject) != 0 {
		t.Errorf("expected no inject entries when bd is missing, got %d: %v", len(result.Inject), result.Inject)
	}
	if result.Block {
		t.Error("expected Block=false even when handlers error")
	}
}

func TestDefaultHandlerChainEventFieldPassthrough(t *testing.T) {
	server, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Mock bd that writes its arguments to a file for inspection.
	argsFile := filepath.Join(t.TempDir(), "bd-args.txt")
	setupMockBDForRPC(t, fmt.Sprintf(`
# Write all arguments to file for inspection.
printf "%%s\n" "$@" >> %s
case "$1" in
  gate)
    printf '{"decision":"allow"}'
    exit 0
    ;;
esac
exit 0
`, argsFile))

	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}
	server.SetBus(bus)

	cwd := t.TempDir()
	eventJSON := fmt.Sprintf(`{"session_id":"sess-fields","cwd":%q,"tool_name":"Bash"}`, cwd)
	args := BusEmitArgs{
		HookType:  "PreToolUse",
		EventJSON: json.RawMessage(eventJSON),
		SessionID: "sess-fields",
	}

	resp, err := client.Execute(OpBusEmit, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	// Read the captured arguments file to verify gate handler was called
	// with the correct subcommand and --hook flag.
	argsContent, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read args file: %v", err)
	}

	argsStr := string(argsContent)

	// Gate handler calls: bd gate session-check --hook PreToolUse --json
	if !strings.Contains(argsStr, "gate") {
		t.Errorf("expected 'gate' in captured args, got:\n%s", argsStr)
	}
	if !strings.Contains(argsStr, "session-check") {
		t.Errorf("expected 'session-check' in captured args, got:\n%s", argsStr)
	}
	if !strings.Contains(argsStr, "--hook") {
		t.Errorf("expected '--hook' in captured args, got:\n%s", argsStr)
	}
	if !strings.Contains(argsStr, "PreToolUse") {
		t.Errorf("expected 'PreToolUse' in captured args, got:\n%s", argsStr)
	}
	if !strings.Contains(argsStr, "--json") {
		t.Errorf("expected '--json' in captured args, got:\n%s", argsStr)
	}
}
