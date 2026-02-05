package gate

import (
	"encoding/json"
	"fmt"
)

// Policy represents the merged gate policy from config beads.
// Structure matches the config:hook-gates metadata format:
//
//	{
//	  "hooks": {
//	    "Stop": {
//	      "gates": {
//	        "decision": {"mode": "strict"},
//	        "commit-push": {"mode": "soft"}
//	      }
//	    }
//	  }
//	}
type Policy struct {
	Hooks map[HookType]HookPolicy `json:"hooks"`
}

// HookPolicy configures gates for a specific hook type.
type HookPolicy struct {
	Gates map[string]GatePolicy `json:"gates"`
}

// GatePolicy configures a single gate's mode.
type GatePolicy struct {
	Mode string `json:"mode"` // "strict" or "soft"
}

// ParsePolicy parses a gate policy from raw JSON (config bead metadata).
func ParsePolicy(data json.RawMessage) (*Policy, error) {
	if len(data) == 0 {
		return &Policy{}, nil
	}

	// Parse into intermediate format to handle the HookType keys
	var raw struct {
		Hooks map[string]HookPolicy `json:"hooks"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing gate policy: %w", err)
	}

	policy := &Policy{
		Hooks: make(map[HookType]HookPolicy),
	}

	for hookName, hookPolicy := range raw.Hooks {
		hookType, err := ParseHookType(hookName)
		if err != nil {
			// Skip unknown hook types for forward compatibility
			continue
		}
		policy.Hooks[hookType] = hookPolicy
	}

	return policy, nil
}

// ApplyPolicy applies a gate policy to a registry, overriding gate modes.
// Gates referenced in the policy but not registered are silently ignored.
// This allows policy to configure gates that might be added later.
func ApplyPolicy(reg *Registry, policy *Policy) {
	if policy == nil {
		return
	}

	for _, hookPolicy := range policy.Hooks {
		for gateID, gatePolicy := range hookPolicy.Gates {
			g := reg.Get(gateID)
			if g == nil {
				continue // gate not registered, skip
			}

			switch gatePolicy.Mode {
			case "strict":
				g.Mode = GateModeStrict
			case "soft":
				g.Mode = GateModeSoft
			}
		}
	}
}

// DefaultPolicy returns the default gate policy (matches built-in defaults).
func DefaultPolicy() *Policy {
	return &Policy{
		Hooks: map[HookType]HookPolicy{
			HookStop: {
				Gates: map[string]GatePolicy{
					"decision":    {Mode: "strict"},
					"commit-push": {Mode: "soft"},
					"bead-update": {Mode: "soft"},
				},
			},
			HookPreToolUse: {
				Gates: map[string]GatePolicy{
					"destructive-op":   {Mode: "strict"},
					"sandbox-boundary": {Mode: "soft"},
				},
			},
			HookPreCompact: {
				Gates: map[string]GatePolicy{
					"state-checkpoint": {Mode: "soft"},
					"dirty-work":       {Mode: "soft"},
				},
			},
			HookUserPromptSubmit: {
				Gates: map[string]GatePolicy{
					"context-injection": {Mode: "soft"},
					"stale-context":     {Mode: "soft"},
				},
			},
		},
	}
}
