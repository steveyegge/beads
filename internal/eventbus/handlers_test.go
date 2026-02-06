package eventbus

import (
	"testing"
)

func TestDefaultHandlers(t *testing.T) {
	handlers := DefaultHandlers()
	if len(handlers) != 3 {
		t.Fatalf("expected 3 default handlers, got %d", len(handlers))
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

func TestHandlerPriorityOrdering(t *testing.T) {
	handlers := DefaultHandlers()
	// Verify priority ordering: prime (10) < gate (20) < decision (30)
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

	// Verify all handlers registered
	if len(bus.Handlers()) != 3 {
		t.Errorf("expected 3 handlers, got %d", len(bus.Handlers()))
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
