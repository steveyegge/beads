package gate

import (
	"encoding/json"
	"testing"
)

func TestParsePolicy_Empty(t *testing.T) {
	policy, err := ParsePolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	if policy == nil {
		t.Fatal("expected non-nil policy")
	}
}

func TestParsePolicy_ValidJSON(t *testing.T) {
	data := json.RawMessage(`{
		"hooks": {
			"Stop": {
				"gates": {
					"decision": {"mode": "soft"},
					"commit-push": {"mode": "strict"}
				}
			},
			"PreToolUse": {
				"gates": {
					"destructive-op": {"mode": "soft"}
				}
			}
		}
	}`)

	policy, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}

	// Check Stop hook
	stopPolicy, ok := policy.Hooks[HookStop]
	if !ok {
		t.Fatal("expected Stop hook policy")
	}

	decisionPolicy, ok := stopPolicy.Gates["decision"]
	if !ok {
		t.Fatal("expected decision gate policy")
	}
	if decisionPolicy.Mode != "soft" {
		t.Errorf("expected decision mode 'soft', got %q", decisionPolicy.Mode)
	}

	commitPolicy, ok := stopPolicy.Gates["commit-push"]
	if !ok {
		t.Fatal("expected commit-push gate policy")
	}
	if commitPolicy.Mode != "strict" {
		t.Errorf("expected commit-push mode 'strict', got %q", commitPolicy.Mode)
	}

	// Check PreToolUse hook
	toolPolicy, ok := policy.Hooks[HookPreToolUse]
	if !ok {
		t.Fatal("expected PreToolUse hook policy")
	}
	if len(toolPolicy.Gates) != 1 {
		t.Errorf("expected 1 PreToolUse gate, got %d", len(toolPolicy.Gates))
	}
}

func TestParsePolicy_UnknownHookType(t *testing.T) {
	data := json.RawMessage(`{
		"hooks": {
			"UnknownHook": {
				"gates": {"foo": {"mode": "strict"}}
			},
			"Stop": {
				"gates": {"decision": {"mode": "soft"}}
			}
		}
	}`)

	policy, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}

	// Unknown hook should be skipped
	if len(policy.Hooks) != 1 {
		t.Errorf("expected 1 hook (Stop), got %d", len(policy.Hooks))
	}
}

func TestParsePolicy_InvalidJSON(t *testing.T) {
	data := json.RawMessage(`invalid json`)
	_, err := ParsePolicy(data)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestApplyPolicy(t *testing.T) {
	reg := NewRegistry()
	RegisterBuiltinGates(reg)

	// Verify initial modes
	if reg.Get("decision").Mode != GateModeStrict {
		t.Error("decision should start as strict")
	}
	if reg.Get("commit-push").Mode != GateModeSoft {
		t.Error("commit-push should start as soft")
	}

	// Apply a policy that flips modes
	policy := &Policy{
		Hooks: map[HookType]HookPolicy{
			HookStop: {
				Gates: map[string]GatePolicy{
					"decision":    {Mode: "soft"},
					"commit-push": {Mode: "strict"},
				},
			},
		},
	}

	ApplyPolicy(reg, policy)

	if reg.Get("decision").Mode != GateModeSoft {
		t.Error("decision should be soft after policy")
	}
	if reg.Get("commit-push").Mode != GateModeStrict {
		t.Error("commit-push should be strict after policy")
	}
}

func TestApplyPolicy_UnregisteredGate(t *testing.T) {
	reg := NewRegistry()
	RegisterBuiltinGates(reg)

	// Policy references a gate that doesn't exist
	policy := &Policy{
		Hooks: map[HookType]HookPolicy{
			HookStop: {
				Gates: map[string]GatePolicy{
					"nonexistent-gate": {Mode: "strict"},
				},
			},
		},
	}

	// Should not panic
	ApplyPolicy(reg, policy)
}

func TestApplyPolicy_Nil(t *testing.T) {
	reg := NewRegistry()
	RegisterBuiltinGates(reg)

	// Should not panic
	ApplyPolicy(reg, nil)
}

func TestDefaultPolicy(t *testing.T) {
	policy := DefaultPolicy()

	// Should have all 4 hook types
	if len(policy.Hooks) != 4 {
		t.Errorf("expected 4 hook types, got %d", len(policy.Hooks))
	}

	// Spot-check decision gate
	stopPolicy, ok := policy.Hooks[HookStop]
	if !ok {
		t.Fatal("missing Stop hook")
	}
	if stopPolicy.Gates["decision"].Mode != "strict" {
		t.Error("decision default should be strict")
	}
	if stopPolicy.Gates["commit-push"].Mode != "soft" {
		t.Error("commit-push default should be soft")
	}
}

func TestParsePolicyRoundTrip(t *testing.T) {
	policy := DefaultPolicy()

	// Serialize
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}

	// Parse back
	parsed, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}

	// Verify same number of hooks
	if len(parsed.Hooks) != len(policy.Hooks) {
		t.Errorf("expected %d hooks, got %d", len(policy.Hooks), len(parsed.Hooks))
	}
}
