package eventbus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultHandlers(t *testing.T) {
	handlers := DefaultHandlers()
	if len(handlers) != 4 {
		t.Fatalf("expected 4 default handlers, got %d", len(handlers))
	}

	// Verify IDs
	ids := map[string]bool{}
	for _, h := range handlers {
		if h.ID() == "" {
			t.Error("handler has empty ID")
		}
		if ids[h.ID()] {
			t.Errorf("duplicate handler ID: %s", h.ID())
		}
		ids[h.ID()] = true
	}

	if !ids["prime"] {
		t.Error("missing prime handler")
	}
	if !ids["gate"] {
		t.Error("missing gate handler")
	}
	if !ids["decision"] {
		t.Error("missing decision handler")
	}
}

func TestPrimeHandlerMetadata(t *testing.T) {
	h := &PrimeHandler{}
	if h.ID() != "prime" {
		t.Errorf("expected ID 'prime', got %q", h.ID())
	}
	if h.Priority() != 10 {
		t.Errorf("expected priority 10, got %d", h.Priority())
	}
	handles := h.Handles()
	if len(handles) != 2 {
		t.Fatalf("expected 2 event types, got %d", len(handles))
	}
	expected := map[EventType]bool{EventSessionStart: true, EventPreCompact: true}
	for _, et := range handles {
		if !expected[et] {
			t.Errorf("unexpected event type: %s", et)
		}
	}
}

func TestGateHandlerMetadata(t *testing.T) {
	h := &GateHandler{}
	if h.ID() != "gate" {
		t.Errorf("expected ID 'gate', got %q", h.ID())
	}
	if h.Priority() != 20 {
		t.Errorf("expected priority 20, got %d", h.Priority())
	}
	handles := h.Handles()
	if len(handles) != 2 {
		t.Fatalf("expected 2 event types, got %d", len(handles))
	}
	expected := map[EventType]bool{EventStop: true, EventPreToolUse: true}
	for _, et := range handles {
		if !expected[et] {
			t.Errorf("unexpected event type: %s", et)
		}
	}
}

func TestDecisionHandlerMetadata(t *testing.T) {
	h := &DecisionHandler{}
	if h.ID() != "decision" {
		t.Errorf("expected ID 'decision', got %q", h.ID())
	}
	if h.Priority() != 30 {
		t.Errorf("expected priority 30, got %d", h.Priority())
	}
	handles := h.Handles()
	if len(handles) != 2 {
		t.Fatalf("expected 2 event types, got %d", len(handles))
	}
	expected := map[EventType]bool{EventSessionStart: true, EventPreCompact: true}
	for _, et := range handles {
		if !expected[et] {
			t.Errorf("unexpected event type: %s", et)
		}
	}
}

func TestStopDecisionHandlerMetadata(t *testing.T) {
	h := &StopDecisionHandler{}
	if h.ID() != "stop-decision" {
		t.Errorf("expected ID 'stop-decision', got %q", h.ID())
	}
	if h.Priority() != 15 {
		t.Errorf("expected priority 15, got %d", h.Priority())
	}
	handles := h.Handles()
	if len(handles) != 1 {
		t.Fatalf("expected 1 event type, got %d", len(handles))
	}
	if handles[0] != EventStop {
		t.Errorf("expected EventStop, got %s", handles[0])
	}
}

func TestHandlerPriorityOrdering(t *testing.T) {
	handlers := DefaultHandlers()
	// Verify priority ordering: prime(10) < stop-decision(15) < gate(20) < decision(30)
	for i := 0; i < len(handlers)-1; i++ {
		if handlers[i].Priority() >= handlers[i+1].Priority() {
			t.Errorf("handler %q (priority %d) should have lower priority than %q (priority %d)",
				handlers[i].ID(), handlers[i].Priority(),
				handlers[i+1].ID(), handlers[i+1].Priority())
		}
	}
}

func TestBusWithDefaultHandlers(t *testing.T) {
	bus := New()
	for _, h := range DefaultHandlers() {
		bus.Register(h)
	}

	if len(bus.Handlers()) != 4 {
		t.Errorf("expected 4 handlers, got %d", len(bus.Handlers()))
	}
}

func TestFindBDBinary(t *testing.T) {
	// This test verifies the bd binary lookup works in CI.
	path, err := findBDBinary()
	if err != nil {
		t.Skipf("bd binary not found (expected in dev/CI only): %v", err)
	}
	if path == "" {
		t.Error("findBDBinary returned empty path")
	}
}

// setupMockBD creates a temporary directory with a mock bd shell script,
// prepends it to PATH so handlers find it via exec.LookPath, and returns
// a cleanup function that restores the original PATH.
func setupMockBD(t *testing.T, script string) func() {
	t.Helper()
	dir := t.TempDir()
	bdPath := filepath.Join(dir, "bd")
	if err := os.WriteFile(bdPath, []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatalf("failed to write mock bd script: %v", err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+oldPath)
	return func() { os.Setenv("PATH", oldPath) }
}

// ---------------------------------------------------------------------------
// PrimeHandler.Handle integration tests
// ---------------------------------------------------------------------------

func TestPrimeHandlerHandle(t *testing.T) {
	cleanup := setupMockBD(t, `
case "$1" in
  prime) printf "# Beads Workflow Context\n\nSome context here"; exit 0;;
esac
exit 1
`)
	defer cleanup()

	h := &PrimeHandler{}
	event := &Event{
		Type: EventSessionStart,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result.Inject) != 1 {
		t.Fatalf("expected 1 inject entry, got %d", len(result.Inject))
	}
	if !strings.Contains(result.Inject[0], "Beads Workflow Context") {
		t.Errorf("expected inject to contain workflow context, got: %q", result.Inject[0])
	}
	if !strings.Contains(result.Inject[0], "Some context here") {
		t.Errorf("expected inject to contain 'Some context here', got: %q", result.Inject[0])
	}
}

func TestPrimeHandlerHandleBDNotFound(t *testing.T) {
	// Point PATH to an empty directory so bd is not found.
	emptyDir := t.TempDir()
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", emptyDir)
	defer os.Setenv("PATH", oldPath)

	h := &PrimeHandler{}
	event := &Event{
		Type: EventSessionStart,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err == nil {
		t.Fatal("expected error when bd is not in PATH, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error message, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GateHandler.Handle integration tests
// ---------------------------------------------------------------------------

func TestGateHandlerHandle_Allow(t *testing.T) {
	cleanup := setupMockBD(t, `
case "$1" in
  gate) printf '{"decision":"allow","warnings":["test warning"]}'; exit 0;;
esac
exit 1
`)
	defer cleanup()

	h := &GateHandler{}
	event := &Event{
		Type: EventPreToolUse,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Block {
		t.Error("expected Block=false for allow decision")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	if result.Warnings[0] != "test warning" {
		t.Errorf("expected warning 'test warning', got %q", result.Warnings[0])
	}
}

func TestGateHandlerHandle_Block(t *testing.T) {
	cleanup := setupMockBD(t, `
case "$1" in
  gate) printf '{"decision":"block","reason":"gate failed","warnings":["blocked warning"]}'; exit 1;;
esac
exit 1
`)
	defer cleanup()

	h := &GateHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error (block is not an error), got: %v", err)
	}
	if !result.Block {
		t.Error("expected Block=true for block decision")
	}
	if result.Reason != "gate failed" {
		t.Errorf("expected reason 'gate failed', got %q", result.Reason)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	if result.Warnings[0] != "blocked warning" {
		t.Errorf("expected warning 'blocked warning', got %q", result.Warnings[0])
	}
}

func TestGateHandlerHandle_BlockRawOutput(t *testing.T) {
	// When bd exits 1 with non-JSON output, the handler should treat
	// the raw stdout as the block reason.
	cleanup := setupMockBD(t, `
case "$1" in
  gate) printf "raw gate failure message"; exit 1;;
esac
exit 1
`)
	defer cleanup()

	h := &GateHandler{}
	event := &Event{
		Type: EventPreToolUse,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error (block is not an error), got: %v", err)
	}
	if !result.Block {
		t.Error("expected Block=true for non-JSON exit-1 output")
	}
	if result.Reason != "raw gate failure message" {
		t.Errorf("expected reason 'raw gate failure message', got %q", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// DecisionHandler.Handle integration tests
// ---------------------------------------------------------------------------

func TestDecisionHandlerHandle(t *testing.T) {
	cleanup := setupMockBD(t, `
case "$1" in
  decision) printf "Decision response injected"; exit 0;;
esac
exit 1
`)
	defer cleanup()

	h := &DecisionHandler{}
	event := &Event{
		Type: EventSessionStart,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result.Inject) != 1 {
		t.Fatalf("expected 1 inject entry, got %d", len(result.Inject))
	}
	if result.Inject[0] != "Decision response injected" {
		t.Errorf("expected inject 'Decision response injected', got %q", result.Inject[0])
	}
}

func TestDecisionHandlerHandle_Empty(t *testing.T) {
	// When bd outputs nothing, result.Inject should remain empty.
	cleanup := setupMockBD(t, `
case "$1" in
  decision) exit 0;;
esac
exit 1
`)
	defer cleanup()

	h := &DecisionHandler{}
	event := &Event{
		Type: EventPreCompact,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result.Inject) != 0 {
		t.Errorf("expected 0 inject entries for empty output, got %d: %v", len(result.Inject), result.Inject)
	}
}

// ---------------------------------------------------------------------------
// StopDecisionHandler.Handle integration tests
// ---------------------------------------------------------------------------

func TestStopDecisionHandler_Allow(t *testing.T) {
	// bd decision stop-check exits 0 → allow stop.
	cleanup := setupMockBD(t, `
case "$1" in
  decision)
    case "$2" in
      stop-check) printf '{"decision":"allow","reason":"human selected stop"}'; exit 0;;
    esac
    ;;
esac
exit 1
`)
	defer cleanup()

	h := &StopDecisionHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Block {
		t.Error("expected Block=false when stop-check exits 0")
	}
}

func TestStopDecisionHandler_Block(t *testing.T) {
	// bd decision stop-check exits 1 with JSON → block stop.
	cleanup := setupMockBD(t, `
case "$1" in
  decision)
    case "$2" in
      stop-check) printf '{"decision":"block","reason":"Keep going with the tests"}'; exit 1;;
    esac
    ;;
esac
exit 1
`)
	defer cleanup()

	h := &StopDecisionHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error (block is not an error), got: %v", err)
	}
	if !result.Block {
		t.Error("expected Block=true when stop-check exits 1")
	}
	if result.Reason != "Keep going with the tests" {
		t.Errorf("expected reason 'Keep going with the tests', got %q", result.Reason)
	}
}

func TestStopDecisionHandler_StopHookActive(t *testing.T) {
	// When stop_hook_active=true, the handler should still call stop-check but
	// pass --reentry flag. The stop-check subprocess decides how to handle re-entry.
	// Here we simulate stop-check allowing the stop on re-entry (exit 0).
	cleanup := setupMockBD(t, `
case "$1" in
  decision)
    case "$2" in
      stop-check) printf '{"decision":"allow","reason":"re-entry allowed"}'; exit 0;;
    esac
    ;;
esac
exit 1
`)
	defer cleanup()

	h := &StopDecisionHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
		Raw:  []byte(`{"stop_hook_active":true}`),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Block {
		t.Error("expected Block=false when stop-check allows on re-entry")
	}
}

func TestStopDecisionHandler_StopHookActiveBlocks(t *testing.T) {
	// When stop_hook_active=true and agent created a decision but human hasn't
	// responded yet, the stop-check re-entry should still be able to block.
	cleanup := setupMockBD(t, `
case "$1" in
  decision)
    case "$2" in
      stop-check) printf '{"decision":"block","reason":"awaiting human response"}'; exit 1;;
    esac
    ;;
esac
exit 1
`)
	defer cleanup()

	h := &StopDecisionHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
		Raw:  []byte(`{"stop_hook_active":true}`),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result.Block {
		t.Error("expected Block=true when stop-check blocks on re-entry")
	}
	if result.Reason != "awaiting human response" {
		t.Errorf("expected reason 'awaiting human response', got %q", result.Reason)
	}
}

func TestStopDecisionHandler_StopHookActiveFalse(t *testing.T) {
	// When stop_hook_active=false, handler should proceed normally.
	cleanup := setupMockBD(t, `
case "$1" in
  decision)
    case "$2" in
      stop-check) printf '{"decision":"allow","reason":"ok"}'; exit 0;;
    esac
    ;;
esac
exit 1
`)
	defer cleanup()

	h := &StopDecisionHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
		Raw:  []byte(`{"stop_hook_active":false}`),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Block {
		t.Error("expected Block=false when stop-check exits 0")
	}
}

func TestStopDecisionHandler_Error(t *testing.T) {
	// bd exits with unexpected error (exit code 2) → handler returns error, no block.
	cleanup := setupMockBD(t, `
case "$1" in
  decision)
    case "$2" in
      stop-check) printf "unexpected failure"; exit 2;;
    esac
    ;;
esac
exit 1
`)
	defer cleanup()

	h := &StopDecisionHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err == nil {
		t.Fatal("expected error for unexpected exit code, got nil")
	}
	if !strings.Contains(err.Error(), "stop-decision") {
		t.Errorf("expected error to mention 'stop-decision', got: %v", err)
	}
	if result.Block {
		t.Error("expected Block=false on unexpected error (fail-open)")
	}
}

func TestStopDecisionHandler_BlockRawOutput(t *testing.T) {
	// bd exits 1 with non-JSON output → treat as block with raw reason.
	cleanup := setupMockBD(t, `
case "$1" in
  decision)
    case "$2" in
      stop-check) printf "raw block reason"; exit 1;;
    esac
    ;;
esac
exit 1
`)
	defer cleanup()

	h := &StopDecisionHandler{}
	event := &Event{
		Type: EventStop,
		CWD:  t.TempDir(),
	}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Fatalf("expected no error (block is not an error), got: %v", err)
	}
	if !result.Block {
		t.Error("expected Block=true for non-JSON exit-1 output")
	}
	if result.Reason != "raw block reason" {
		t.Errorf("expected reason 'raw block reason', got %q", result.Reason)
	}
}
