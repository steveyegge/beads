// Package gate implements session-level gates for Claude Code hook events.
//
// This is Layer 2 in the three-layer gate architecture:
//   - Layer 1: Formula gates (internal/formula/) — per-step in a molecule
//   - Layer 2: Session gates (this package) — per-hook-event in a session
//   - Layer 3: Config bead policies — per-scope policy
//
// Session gates use a file-marker system in .runtime/gates/<session-id>/
// to track which gates have been satisfied during a Claude Code session.
package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HookType represents a Claude Code hook event type.
type HookType string

const (
	HookStop             HookType = "Stop"
	HookPreToolUse       HookType = "PreToolUse"
	HookUserPromptSubmit HookType = "UserPromptSubmit"
	HookPreCompact       HookType = "PreCompact"
)

// ValidHookTypes returns all valid hook types.
func ValidHookTypes() []HookType {
	return []HookType{HookStop, HookPreToolUse, HookUserPromptSubmit, HookPreCompact}
}

// ParseHookType parses a string into a HookType, case-insensitive.
func ParseHookType(s string) (HookType, error) {
	lower := strings.ToLower(s)
	for _, h := range ValidHookTypes() {
		if strings.ToLower(string(h)) == lower {
			return h, nil
		}
	}
	return "", fmt.Errorf("unknown hook type %q (valid: Stop, PreToolUse, UserPromptSubmit, PreCompact)", s)
}

// GateMode determines whether a gate blocks or warns.
type GateMode string

const (
	GateModeStrict GateMode = "strict" // Block the hook event
	GateModeSoft   GateMode = "soft"   // Warn but allow
)

// GateContext provides runtime context for gate auto-check functions.
type GateContext struct {
	SessionID string
	HookType  HookType
	WorkDir   string
	HookBead  string // current hooked bead if any
	Role      string // GT_ROLE
	ToolInput string // for PreToolUse: the command being executed
}

// Gate defines a session-level gate that can block or warn on a hook event.
type Gate struct {
	ID          string                    // e.g., "decision", "commit-push", "destructive-op"
	Hook        HookType                  // which hook this gate applies to
	Description string                    // human-readable purpose
	Mode        GateMode                  // strict (block) or soft (warn)
	AutoCheck   func(ctx GateContext) bool // optional: returns true if gate is auto-satisfied
	Hint        string                    // how to satisfy this gate
}

// GateResult holds the outcome of checking a single gate.
type GateResult struct {
	GateID    string   `json:"gate_id"`
	Hook      HookType `json:"hook"`
	Satisfied bool     `json:"satisfied"`
	Mode      GateMode `json:"mode"`
	Message   string   `json:"message,omitempty"`
	Hint      string   `json:"hint,omitempty"`
}

// CheckResponse is the JSON response for a hook gate check.
type CheckResponse struct {
	Decision string       `json:"decision"`          // "allow" or "block"
	Reason   string       `json:"reason,omitempty"`   // why blocked
	Results  []GateResult `json:"results,omitempty"`  // per-gate details
	Warnings []string     `json:"warnings,omitempty"` // soft gate warnings
}

// runtimeGatesDir returns the path to the gates runtime directory.
// Layout: <workdir>/.runtime/gates/<session-id>/
func runtimeGatesDir(workDir, sessionID string) string {
	return filepath.Join(workDir, ".runtime", "gates", sessionID)
}

// markerPath returns the path to a gate marker file.
func markerPath(workDir, sessionID, gateID string) string {
	return filepath.Join(runtimeGatesDir(workDir, sessionID), gateID)
}

// MarkGate marks a gate as satisfied for the given session.
func MarkGate(workDir, sessionID, gateID string) error {
	dir := runtimeGatesDir(workDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating gates dir: %w", err)
	}
	f, err := os.Create(markerPath(workDir, sessionID, gateID))
	if err != nil {
		return fmt.Errorf("marking gate %s: %w", gateID, err)
	}
	return f.Close()
}

// ClearGate removes a gate marker for the given session.
func ClearGate(workDir, sessionID, gateID string) {
	_ = os.Remove(markerPath(workDir, sessionID, gateID))
}

// ClearGatesForHook clears all gate markers for a specific hook type.
// It only removes markers for gates that are registered for that hook.
func ClearGatesForHook(workDir, sessionID string, hookType HookType, reg *Registry) {
	gates := reg.GatesForHook(hookType)
	for _, g := range gates {
		ClearGate(workDir, sessionID, g.ID)
	}
}

// ClearAllGates removes all gate markers for the given session.
func ClearAllGates(workDir, sessionID string) {
	dir := runtimeGatesDir(workDir, sessionID)
	_ = os.RemoveAll(dir)
}

// IsGateSatisfied checks whether a gate marker exists for the given session.
func IsGateSatisfied(workDir, sessionID, gateID string) bool {
	_, err := os.Stat(markerPath(workDir, sessionID, gateID))
	return err == nil
}

// CheckGatesForHook evaluates all registered gates for the specified hook type.
// A gate is satisfied if:
//  1. Its marker file exists, OR
//  2. Its AutoCheck function returns true (and the marker is set automatically)
func CheckGatesForHook(workDir, sessionID string, hookType HookType, reg *Registry) ([]GateResult, error) {
	gates := reg.GatesForHook(hookType)
	results := make([]GateResult, 0, len(gates))

	ctx := GateContext{
		SessionID: sessionID,
		HookType:  hookType,
		WorkDir:   workDir,
	}

	for _, g := range gates {
		result := GateResult{
			GateID: g.ID,
			Hook:   g.Hook,
			Mode:   g.Mode,
			Hint:   g.Hint,
		}

		// Check marker first
		if IsGateSatisfied(workDir, sessionID, g.ID) {
			result.Satisfied = true
			result.Message = "marked satisfied"
			results = append(results, result)
			continue
		}

		// Try auto-check
		if g.AutoCheck != nil && g.AutoCheck(ctx) {
			// Auto-satisfied — set the marker for future checks
			if err := MarkGate(workDir, sessionID, g.ID); err != nil {
				return nil, fmt.Errorf("auto-marking gate %s: %w", g.ID, err)
			}
			result.Satisfied = true
			result.Message = "auto-satisfied"
			results = append(results, result)
			continue
		}

		// Not satisfied
		result.Satisfied = false
		result.Message = g.Description
		results = append(results, result)
	}

	return results, nil
}

// EvaluateHook checks all gates for a hook type and returns a CheckResponse.
// If any strict gate is unsatisfied, the decision is "block".
// Soft unsatisfied gates produce warnings but allow the hook.
func EvaluateHook(workDir, sessionID string, hookType HookType, reg *Registry) (*CheckResponse, error) {
	results, err := CheckGatesForHook(workDir, sessionID, hookType, reg)
	if err != nil {
		return nil, err
	}

	resp := &CheckResponse{
		Decision: "allow",
		Results:  results,
	}

	var blockReasons []string
	for _, r := range results {
		if r.Satisfied {
			continue
		}
		switch r.Mode {
		case GateModeStrict:
			blockReasons = append(blockReasons, fmt.Sprintf("%s: %s", r.GateID, r.Message))
		case GateModeSoft:
			warning := fmt.Sprintf("%s: %s", r.GateID, r.Message)
			if r.Hint != "" {
				warning += fmt.Sprintf(" (hint: %s)", r.Hint)
			}
			resp.Warnings = append(resp.Warnings, warning)
		}
	}

	if len(blockReasons) > 0 {
		resp.Decision = "block"
		resp.Reason = strings.Join(blockReasons, "; ")
	}

	return resp, nil
}
