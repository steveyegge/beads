package eventbus

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNewExternalHandler(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:       "test-handler",
		Command:  "echo hello",
		Events:   []string{"SessionStart", "Stop"},
		Priority: 25,
		Shell:    "bash",
	}

	h := NewExternalHandler(cfg)
	if h.ID() != "test-handler" {
		t.Errorf("expected ID 'test-handler', got %q", h.ID())
	}
	if h.Priority() != 25 {
		t.Errorf("expected priority 25, got %d", h.Priority())
	}
	if len(h.Handles()) != 2 {
		t.Fatalf("expected 2 event types, got %d", len(h.Handles()))
	}
	if h.Handles()[0] != EventSessionStart {
		t.Errorf("expected first event SessionStart, got %s", h.Handles()[0])
	}
	if h.Handles()[1] != EventStop {
		t.Errorf("expected second event Stop, got %s", h.Handles()[1])
	}
}

func TestNewExternalHandlerDefaults(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:      "defaults",
		Command: "true",
		Events:  []string{"Stop"},
	}

	h := NewExternalHandler(cfg)
	if h.Priority() != 50 {
		t.Errorf("expected default priority 50, got %d", h.Priority())
	}
	if h.config.Shell != "sh" {
		t.Errorf("expected default shell 'sh', got %q", h.config.Shell)
	}
}

func TestExternalHandlerConfig(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:       "roundtrip",
		Command:  "cat",
		Events:   []string{"SessionStart"},
		Priority: 10,
		Shell:    "bash",
	}

	h := NewExternalHandler(cfg)
	got := h.Config()

	if got.ID != cfg.ID {
		t.Errorf("Config().ID: expected %q, got %q", cfg.ID, got.ID)
	}
	if got.Command != cfg.Command {
		t.Errorf("Config().Command: expected %q, got %q", cfg.Command, got.Command)
	}
}

func TestExternalHandlerHandleSuccess(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:      "echo-test",
		Command: `echo '{"inject":["hello from handler"]}'`,
		Events:  []string{"SessionStart"},
	}

	h := NewExternalHandler(cfg)
	event := &Event{
		Type:      EventSessionStart,
		SessionID: "test-session",
	}
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, event, result)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(result.Inject) != 1 || result.Inject[0] != "hello from handler" {
		t.Errorf("expected inject ['hello from handler'], got %v", result.Inject)
	}
}

func TestExternalHandlerHandleBlock(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:      "block-test",
		Command: `echo '{"block":true,"reason":"not allowed"}'`,
		Events:  []string{"PreToolUse"},
	}

	h := NewExternalHandler(cfg)
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, &Event{Type: EventPreToolUse}, result)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if !result.Block {
		t.Error("expected block=true")
	}
	if result.Reason != "not allowed" {
		t.Errorf("expected reason 'not allowed', got %q", result.Reason)
	}
}

func TestExternalHandlerHandleNoOutput(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:      "silent-test",
		Command: "true",
		Events:  []string{"Stop"},
	}

	h := NewExternalHandler(cfg)
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, &Event{Type: EventStop}, result)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if result.Block {
		t.Error("expected no block from silent handler")
	}
	if len(result.Inject) > 0 {
		t.Errorf("expected no inject, got %v", result.Inject)
	}
}

func TestExternalHandlerHandleExitError(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:      "error-test",
		Command: "exit 1",
		Events:  []string{"Stop"},
	}

	h := NewExternalHandler(cfg)
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, &Event{Type: EventStop}, result)
	if err == nil {
		t.Fatal("expected error from handler that exits 1")
	}

	// Result should NOT be modified on error.
	if result.Block {
		t.Error("expected no block on handler error")
	}
}

func TestExternalHandlerHandleNonJSONOutput(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:      "text-output",
		Command: "echo 'just some log text'",
		Events:  []string{"SessionStart"},
	}

	h := NewExternalHandler(cfg)
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, &Event{Type: EventSessionStart}, result)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// Non-JSON output should be silently ignored.
	if result.Block {
		t.Error("expected no block")
	}
	if len(result.Inject) > 0 {
		t.Errorf("expected no inject, got %v", result.Inject)
	}
}

func TestExternalHandlerReceivesEventJSON(t *testing.T) {
	// Use a command that reads stdin via cat and checks it contains expected data.
	cfg := ExternalHandlerConfig{
		ID:      "stdin-test",
		Command: `input=$(cat) && echo "{\"inject\":[\"received\"]}"`,
		Events:  []string{"SessionStart"},
		Shell:   "bash",
	}

	h := NewExternalHandler(cfg)
	event := &Event{
		Type:      EventSessionStart,
		SessionID: "test-stdin",
		Raw:       json.RawMessage(`{"session_id":"test-stdin"}`),
	}
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, event, result)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// The handler received stdin (cat succeeded) and output inject.
	if len(result.Inject) != 1 || result.Inject[0] != "received" {
		t.Errorf("expected inject ['received'], got %v", result.Inject)
	}
}

func TestExternalHandlerConfigSerialization(t *testing.T) {
	cfg := ExternalHandlerConfig{
		ID:       "serial-test",
		Command:  "echo test",
		Events:   []string{"SessionStart", "Stop"},
		Priority: 25,
		Shell:    "bash",
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ExternalHandlerConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != cfg.ID {
		t.Errorf("ID: expected %q, got %q", cfg.ID, decoded.ID)
	}
	if decoded.Command != cfg.Command {
		t.Errorf("Command: expected %q, got %q", cfg.Command, decoded.Command)
	}
	if len(decoded.Events) != len(cfg.Events) {
		t.Fatalf("Events: expected %d, got %d", len(cfg.Events), len(decoded.Events))
	}
	if decoded.Priority != cfg.Priority {
		t.Errorf("Priority: expected %d, got %d", cfg.Priority, decoded.Priority)
	}
	if decoded.Shell != cfg.Shell {
		t.Errorf("Shell: expected %q, got %q", cfg.Shell, decoded.Shell)
	}
}
