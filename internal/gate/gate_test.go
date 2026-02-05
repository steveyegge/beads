package gate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHookType(t *testing.T) {
	tests := []struct {
		input   string
		want    HookType
		wantErr bool
	}{
		{"Stop", HookStop, false},
		{"stop", HookStop, false},
		{"STOP", HookStop, false},
		{"PreToolUse", HookPreToolUse, false},
		{"pretooluse", HookPreToolUse, false},
		{"UserPromptSubmit", HookUserPromptSubmit, false},
		{"PreCompact", HookPreCompact, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseHookType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHookType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseHookType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkAndCheckGate(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-session-1"

	// Gate should not be satisfied initially
	if IsGateSatisfied(workDir, sessionID, "decision") {
		t.Error("gate should not be satisfied before marking")
	}

	// Mark it
	if err := MarkGate(workDir, sessionID, "decision"); err != nil {
		t.Fatalf("MarkGate failed: %v", err)
	}

	// Now it should be satisfied
	if !IsGateSatisfied(workDir, sessionID, "decision") {
		t.Error("gate should be satisfied after marking")
	}

	// Marker file should exist
	path := filepath.Join(workDir, ".runtime", "gates", sessionID, "decision")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("marker file should exist at %s: %v", path, err)
	}
}

func TestClearGate(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-session-2"

	if err := MarkGate(workDir, sessionID, "commit-push"); err != nil {
		t.Fatalf("MarkGate failed: %v", err)
	}

	if !IsGateSatisfied(workDir, sessionID, "commit-push") {
		t.Error("gate should be satisfied after marking")
	}

	ClearGate(workDir, sessionID, "commit-push")

	if IsGateSatisfied(workDir, sessionID, "commit-push") {
		t.Error("gate should not be satisfied after clearing")
	}
}

func TestClearAllGates(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-session-3"

	// Mark multiple gates
	for _, id := range []string{"decision", "commit-push", "bead-update"} {
		if err := MarkGate(workDir, sessionID, id); err != nil {
			t.Fatalf("MarkGate(%s) failed: %v", id, err)
		}
	}

	// All should be satisfied
	for _, id := range []string{"decision", "commit-push", "bead-update"} {
		if !IsGateSatisfied(workDir, sessionID, id) {
			t.Errorf("gate %s should be satisfied", id)
		}
	}

	ClearAllGates(workDir, sessionID)

	// None should be satisfied
	for _, id := range []string{"decision", "commit-push", "bead-update"} {
		if IsGateSatisfied(workDir, sessionID, id) {
			t.Errorf("gate %s should not be satisfied after ClearAllGates", id)
		}
	}
}

func TestClearGatesForHook(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-session-4"

	reg := NewRegistry()

	// Register gates for different hooks
	stopGate := &Gate{ID: "decision", Hook: HookStop, Mode: GateModeStrict}
	preToolGate := &Gate{ID: "destructive-op", Hook: HookPreToolUse, Mode: GateModeStrict}
	if err := reg.Register(stopGate); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(preToolGate); err != nil {
		t.Fatal(err)
	}

	// Mark both
	if err := MarkGate(workDir, sessionID, "decision"); err != nil {
		t.Fatal(err)
	}
	if err := MarkGate(workDir, sessionID, "destructive-op"); err != nil {
		t.Fatal(err)
	}

	// Clear only Stop gates
	ClearGatesForHook(workDir, sessionID, HookStop, reg)

	// Stop gate should be cleared
	if IsGateSatisfied(workDir, sessionID, "decision") {
		t.Error("Stop gate should be cleared")
	}

	// PreToolUse gate should still be satisfied
	if !IsGateSatisfied(workDir, sessionID, "destructive-op") {
		t.Error("PreToolUse gate should still be satisfied")
	}
}

func TestSessionIsolation(t *testing.T) {
	workDir := t.TempDir()

	// Mark gate in session A
	if err := MarkGate(workDir, "session-a", "decision"); err != nil {
		t.Fatal(err)
	}

	// Should be satisfied in session A
	if !IsGateSatisfied(workDir, "session-a", "decision") {
		t.Error("gate should be satisfied in session-a")
	}

	// Should NOT be satisfied in session B
	if IsGateSatisfied(workDir, "session-b", "decision") {
		t.Error("gate should not be satisfied in session-b")
	}
}

func TestCheckGatesForHook(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-check"

	reg := NewRegistry()

	// Register two Stop gates
	if err := reg.Register(&Gate{
		ID:          "decision",
		Hook:        HookStop,
		Description: "decision point offered",
		Mode:        GateModeStrict,
		Hint:        "offer a decision point before stopping",
	}); err != nil {
		t.Fatal(err)
	}

	if err := reg.Register(&Gate{
		ID:          "commit-push",
		Hook:        HookStop,
		Description: "changes committed and pushed",
		Mode:        GateModeSoft,
		Hint:        "commit and push your changes",
	}); err != nil {
		t.Fatal(err)
	}

	// Check with nothing marked — both unsatisfied
	results, err := CheckGatesForHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Satisfied {
			t.Errorf("gate %s should not be satisfied", r.GateID)
		}
	}

	// Mark the decision gate
	if err := MarkGate(workDir, sessionID, "decision"); err != nil {
		t.Fatal(err)
	}

	// Check again — decision satisfied, commit-push not
	results, err = CheckGatesForHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		switch r.GateID {
		case "decision":
			if !r.Satisfied {
				t.Error("decision gate should be satisfied")
			}
		case "commit-push":
			if r.Satisfied {
				t.Error("commit-push gate should not be satisfied")
			}
		}
	}
}

func TestAutoCheckGate(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-autocheck"

	reg := NewRegistry()

	// Register a gate with an auto-check that always passes
	if err := reg.Register(&Gate{
		ID:          "auto-gate",
		Hook:        HookStop,
		Description: "auto-satisfying gate",
		Mode:        GateModeStrict,
		AutoCheck:   func(_ GateContext) bool { return true },
	}); err != nil {
		t.Fatal(err)
	}

	// Check — should be auto-satisfied
	results, err := CheckGatesForHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Satisfied {
		t.Error("auto-check gate should be satisfied")
	}
	if results[0].Message != "auto-satisfied" {
		t.Errorf("expected message 'auto-satisfied', got %q", results[0].Message)
	}

	// Marker should now exist for future checks
	if !IsGateSatisfied(workDir, sessionID, "auto-gate") {
		t.Error("auto-satisfied gate should have marker set")
	}
}

func TestAutoCheckGateFailing(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-autocheck-fail"

	reg := NewRegistry()

	if err := reg.Register(&Gate{
		ID:          "failing-auto",
		Hook:        HookStop,
		Description: "failing auto-check",
		Mode:        GateModeStrict,
		AutoCheck:   func(_ GateContext) bool { return false },
	}); err != nil {
		t.Fatal(err)
	}

	results, err := CheckGatesForHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Satisfied {
		t.Error("failing auto-check gate should not be satisfied")
	}
}

func TestEvaluateHook_AllSatisfied(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-eval-ok"

	reg := NewRegistry()
	if err := reg.Register(&Gate{
		ID:   "g1",
		Hook: HookStop,
		Mode: GateModeStrict,
	}); err != nil {
		t.Fatal(err)
	}

	// Mark the gate
	if err := MarkGate(workDir, sessionID, "g1"); err != nil {
		t.Fatal(err)
	}

	resp, err := EvaluateHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "allow" {
		t.Errorf("expected allow, got %q", resp.Decision)
	}
}

func TestEvaluateHook_StrictBlocks(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-eval-block"

	reg := NewRegistry()
	if err := reg.Register(&Gate{
		ID:          "blocker",
		Hook:        HookStop,
		Description: "must be satisfied",
		Mode:        GateModeStrict,
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := EvaluateHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "block" {
		t.Errorf("expected block, got %q", resp.Decision)
	}
	if resp.Reason == "" {
		t.Error("expected a block reason")
	}
}

func TestEvaluateHook_SoftWarns(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-eval-soft"

	reg := NewRegistry()
	if err := reg.Register(&Gate{
		ID:          "soft-gate",
		Hook:        HookStop,
		Description: "nice to have",
		Mode:        GateModeSoft,
		Hint:        "do this if you can",
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := EvaluateHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "allow" {
		t.Errorf("soft gate should not block, got %q", resp.Decision)
	}
	if len(resp.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(resp.Warnings))
	}
}

func TestEvaluateHook_MixedModes(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-eval-mixed"

	reg := NewRegistry()
	if err := reg.Register(&Gate{
		ID:          "strict-unsatisfied",
		Hook:        HookStop,
		Description: "required",
		Mode:        GateModeStrict,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&Gate{
		ID:          "soft-unsatisfied",
		Hook:        HookStop,
		Description: "optional",
		Mode:        GateModeSoft,
		Hint:        "try this",
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := EvaluateHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}

	// Should block because of the strict gate
	if resp.Decision != "allow" {
		// Only block if strict is unsatisfied — let's verify
	}
	if resp.Decision != "block" {
		t.Errorf("should block due to strict unsatisfied gate, got %q", resp.Decision)
	}

	// Should also have a warning for the soft gate
	if len(resp.Warnings) != 1 {
		t.Errorf("expected 1 warning for soft gate, got %d", len(resp.Warnings))
	}
}

func TestEvaluateHook_NoGates(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-eval-none"

	reg := NewRegistry()

	resp, err := EvaluateHook(workDir, sessionID, HookStop, reg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "allow" {
		t.Errorf("no gates should allow, got %q", resp.Decision)
	}
}

func TestEvaluateHook_OnlyChecksCorrectHook(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "test-eval-hookfilter"

	reg := NewRegistry()
	if err := reg.Register(&Gate{
		ID:   "stop-gate",
		Hook: HookStop,
		Mode: GateModeStrict,
	}); err != nil {
		t.Fatal(err)
	}

	// Check PreToolUse — should allow since the gate is for Stop
	resp, err := EvaluateHook(workDir, sessionID, HookPreToolUse, reg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "allow" {
		t.Errorf("PreToolUse should allow when only Stop gates registered, got %q", resp.Decision)
	}
}
