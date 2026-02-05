package gate

import (
	"testing"
)

func TestMolGatePendingGate(t *testing.T) {
	g := MolGatePendingGate()
	if g.ID != "mol-gate-pending" {
		t.Errorf("expected ID 'mol-gate-pending', got %q", g.ID)
	}
	if g.Hook != HookStop {
		t.Errorf("expected HookStop, got %q", g.Hook)
	}
	if g.Mode != GateModeSoft {
		t.Errorf("expected soft, got %q", g.Mode)
	}
	if g.AutoCheck == nil {
		t.Error("should have auto-check function")
	}
}

func TestRegisterBridgeGates(t *testing.T) {
	reg := NewRegistry()
	RegisterBridgeGates(reg)

	if reg.Count() != 1 {
		t.Errorf("expected 1 gate, got %d", reg.Count())
	}
	if reg.Get("mol-gate-pending") == nil {
		t.Error("mol-gate-pending should be registered")
	}
}

func TestCheckMolGatesPending_NoHookBead(t *testing.T) {
	t.Setenv("GT_HOOK_BEAD", "")

	ctx := GateContext{}
	if !checkMolGatesPending(ctx) {
		t.Error("no hooked bead should auto-satisfy (return true)")
	}
}

func TestCheckMolGatesPending_HookBeadFromContext(t *testing.T) {
	t.Setenv("GT_HOOK_BEAD", "")

	// With HookBead set in context but bd not available in PATH,
	// the check should fail open (return true)
	ctx := GateContext{HookBead: "gt-test-123"}
	result := checkMolGatesPending(ctx)
	// When bd command fails (not in PATH or error), should fail open
	if !result {
		t.Error("should fail open when bd command unavailable")
	}
}

func TestCheckMolGatesPending_EnvVarOverride(t *testing.T) {
	// When GT_HOOK_BEAD is set but bd CLI is not available,
	// should fail open
	t.Setenv("GT_HOOK_BEAD", "gt-some-bead")

	ctx := GateContext{}
	result := checkMolGatesPending(ctx)
	if !result {
		t.Error("should fail open when bd command unavailable")
	}
}
