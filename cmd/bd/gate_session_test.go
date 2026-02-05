package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/gate"
)

func TestGetSessionID(t *testing.T) {
	t.Setenv("CLAUDE_SESSION_ID", "test-session-xyz")
	if got := getSessionID(); got != "test-session-xyz" {
		t.Errorf("getSessionID() = %q, want %q", got, "test-session-xyz")
	}

	t.Setenv("CLAUDE_SESSION_ID", "")
	if got := getSessionID(); got != "" {
		t.Errorf("getSessionID() with empty env = %q, want empty", got)
	}
}

func TestSoftCopyRegistry(t *testing.T) {
	reg := gate.NewRegistry()
	_ = reg.Register(&gate.Gate{ID: "strict-stop", Hook: gate.HookStop, Mode: gate.GateModeStrict})
	_ = reg.Register(&gate.Gate{ID: "strict-tool", Hook: gate.HookPreToolUse, Mode: gate.GateModeStrict})

	softReg := softCopyRegistry(reg, gate.HookStop)

	// Stop gate should be soft in the copy
	stopGates := softReg.GatesForHook(gate.HookStop)
	if len(stopGates) != 1 {
		t.Fatalf("expected 1 stop gate, got %d", len(stopGates))
	}
	if stopGates[0].Mode != gate.GateModeSoft {
		t.Errorf("expected soft mode for Stop gate, got %s", stopGates[0].Mode)
	}

	// PreToolUse gate should still be strict
	toolGates := softReg.GatesForHook(gate.HookPreToolUse)
	if len(toolGates) != 1 {
		t.Fatalf("expected 1 tool gate, got %d", len(toolGates))
	}
	if toolGates[0].Mode != gate.GateModeStrict {
		t.Errorf("expected strict mode for PreToolUse gate, got %s", toolGates[0].Mode)
	}

	// Original should be unchanged
	origStopGates := reg.GatesForHook(gate.HookStop)
	if origStopGates[0].Mode != gate.GateModeStrict {
		t.Error("original registry should not be modified")
	}
}

func TestFormatGateResults(t *testing.T) {
	results := []gate.GateResult{
		{GateID: "decision", Satisfied: true},
		{GateID: "commit-push", Satisfied: false},
	}

	got := formatGateResults(results)
	if got != "● decision, ○ commit-push" {
		t.Errorf("formatGateResults = %q, want %q", got, "● decision, ○ commit-push")
	}
}

func TestGateMarkAndStatusIntegration(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "integration-test-1"

	// Mark a gate
	if err := gate.MarkGate(workDir, sessionID, "test-gate"); err != nil {
		t.Fatalf("MarkGate failed: %v", err)
	}

	// Verify marker file exists
	markerFile := filepath.Join(workDir, ".runtime", "gates", sessionID, "test-gate")
	if _, err := os.Stat(markerFile); err != nil {
		t.Errorf("marker file should exist: %v", err)
	}

	// Verify satisfaction
	if !gate.IsGateSatisfied(workDir, sessionID, "test-gate") {
		t.Error("gate should be satisfied after marking")
	}

	// Clear it
	gate.ClearGate(workDir, sessionID, "test-gate")

	if gate.IsGateSatisfied(workDir, sessionID, "test-gate") {
		t.Error("gate should not be satisfied after clearing")
	}
}

func TestSessionGateCheckIntegration(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "integration-test-2"

	reg := gate.NewRegistry()
	_ = reg.Register(&gate.Gate{
		ID:          "test-strict",
		Hook:        gate.HookStop,
		Description: "strict gate",
		Mode:        gate.GateModeStrict,
		Hint:        "satisfy this gate",
	})
	_ = reg.Register(&gate.Gate{
		ID:          "test-soft",
		Hook:        gate.HookStop,
		Description: "soft gate",
		Mode:        gate.GateModeSoft,
	})

	// Check before any marking — should block
	resp, err := gate.EvaluateHook(workDir, sessionID, gate.HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "block" {
		t.Errorf("expected block with unsatisfied strict gate, got %q", resp.Decision)
	}

	// Mark the strict gate
	if err := gate.MarkGate(workDir, sessionID, "test-strict"); err != nil {
		t.Fatal(err)
	}

	// Check again — should allow (soft gate just warns)
	resp, err = gate.EvaluateHook(workDir, sessionID, gate.HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "allow" {
		t.Errorf("expected allow with strict gate satisfied, got %q", resp.Decision)
	}
	if len(resp.Warnings) != 1 {
		t.Errorf("expected 1 warning for unsatisfied soft gate, got %d", len(resp.Warnings))
	}
}
