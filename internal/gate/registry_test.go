package gate

import (
	"testing"
)

func TestRegistryRegister(t *testing.T) {
	reg := NewRegistry()

	g := &Gate{
		ID:          "decision",
		Hook:        HookStop,
		Description: "decision point offered",
		Mode:        GateModeStrict,
	}

	if err := reg.Register(g); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if reg.Count() != 1 {
		t.Errorf("expected 1 gate, got %d", reg.Count())
	}
}

func TestRegistryDuplicateReject(t *testing.T) {
	reg := NewRegistry()

	g := &Gate{ID: "dup", Hook: HookStop, Mode: GateModeStrict}
	if err := reg.Register(g); err != nil {
		t.Fatal(err)
	}

	if err := reg.Register(g); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistryGet(t *testing.T) {
	reg := NewRegistry()

	g := &Gate{ID: "test-gate", Hook: HookStop, Mode: GateModeSoft}
	if err := reg.Register(g); err != nil {
		t.Fatal(err)
	}

	got := reg.Get("test-gate")
	if got == nil {
		t.Fatal("Get returned nil for registered gate")
	}
	if got.ID != "test-gate" {
		t.Errorf("expected ID %q, got %q", "test-gate", got.ID)
	}

	if reg.Get("nonexistent") != nil {
		t.Error("Get should return nil for unregistered gate")
	}
}

func TestRegistryGatesForHook(t *testing.T) {
	reg := NewRegistry()

	// Register gates for different hooks
	gates := []*Gate{
		{ID: "stop-1", Hook: HookStop, Mode: GateModeStrict},
		{ID: "stop-2", Hook: HookStop, Mode: GateModeSoft},
		{ID: "tool-1", Hook: HookPreToolUse, Mode: GateModeStrict},
		{ID: "compact-1", Hook: HookPreCompact, Mode: GateModeSoft},
	}
	for _, g := range gates {
		if err := reg.Register(g); err != nil {
			t.Fatalf("Register(%s) failed: %v", g.ID, err)
		}
	}

	// Check Stop gates
	stopGates := reg.GatesForHook(HookStop)
	if len(stopGates) != 2 {
		t.Errorf("expected 2 Stop gates, got %d", len(stopGates))
	}

	// Check PreToolUse gates
	toolGates := reg.GatesForHook(HookPreToolUse)
	if len(toolGates) != 1 {
		t.Errorf("expected 1 PreToolUse gate, got %d", len(toolGates))
	}

	// Check UserPromptSubmit gates (none registered)
	submitGates := reg.GatesForHook(HookUserPromptSubmit)
	if len(submitGates) != 0 {
		t.Errorf("expected 0 UserPromptSubmit gates, got %d", len(submitGates))
	}
}

func TestRegistryUnregister(t *testing.T) {
	reg := NewRegistry()

	g := &Gate{ID: "removable", Hook: HookStop, Mode: GateModeStrict}
	if err := reg.Register(g); err != nil {
		t.Fatal(err)
	}

	if reg.Count() != 1 {
		t.Fatalf("expected 1 gate, got %d", reg.Count())
	}

	reg.Unregister("removable")

	if reg.Count() != 0 {
		t.Errorf("expected 0 gates after unregister, got %d", reg.Count())
	}

	if reg.Get("removable") != nil {
		t.Error("Get should return nil after unregister")
	}

	stopGates := reg.GatesForHook(HookStop)
	if len(stopGates) != 0 {
		t.Errorf("expected 0 Stop gates after unregister, got %d", len(stopGates))
	}
}

func TestRegistryUnregisterNonexistent(t *testing.T) {
	reg := NewRegistry()

	// Should not panic
	reg.Unregister("does-not-exist")
}

func TestRegistryAllGates(t *testing.T) {
	reg := NewRegistry()

	gates := []*Gate{
		{ID: "a", Hook: HookStop, Mode: GateModeStrict},
		{ID: "b", Hook: HookPreToolUse, Mode: GateModeSoft},
		{ID: "c", Hook: HookPreCompact, Mode: GateModeStrict},
	}
	for _, g := range gates {
		if err := reg.Register(g); err != nil {
			t.Fatal(err)
		}
	}

	all := reg.AllGates()
	if len(all) != 3 {
		t.Errorf("expected 3 gates, got %d", len(all))
	}

	// Verify returned slice is a copy (modifying it shouldn't affect registry)
	all[0] = nil
	if reg.Get("a") == nil {
		t.Error("modifying AllGates result should not affect registry")
	}
}

func TestRegistryGatesForHookReturnsCopy(t *testing.T) {
	reg := NewRegistry()

	if err := reg.Register(&Gate{ID: "g1", Hook: HookStop, Mode: GateModeStrict}); err != nil {
		t.Fatal(err)
	}

	gates := reg.GatesForHook(HookStop)
	gates[0] = nil

	// Original should be unaffected
	gates2 := reg.GatesForHook(HookStop)
	if gates2[0] == nil {
		t.Error("modifying GatesForHook result should not affect registry")
	}
}
