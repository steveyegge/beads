package gate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateCheckpointGate(t *testing.T) {
	g := StateCheckpointGate()
	if g.ID != "state-checkpoint" {
		t.Errorf("expected ID 'state-checkpoint', got %q", g.ID)
	}
	if g.Hook != HookPreCompact {
		t.Errorf("expected HookPreCompact, got %q", g.Hook)
	}
	if g.Mode != GateModeSoft {
		t.Errorf("expected soft, got %q", g.Mode)
	}
}

func TestDirtyWorkGate(t *testing.T) {
	g := DirtyWorkGate()
	if g.ID != "dirty-work" {
		t.Errorf("expected ID 'dirty-work', got %q", g.ID)
	}
	if g.Hook != HookPreCompact {
		t.Errorf("expected HookPreCompact, got %q", g.Hook)
	}
}

func TestRegisterPreCompactGates(t *testing.T) {
	reg := NewRegistry()
	RegisterPreCompactGates(reg)

	if reg.Count() != 2 {
		t.Errorf("expected 2 gates, got %d", reg.Count())
	}

	gates := reg.GatesForHook(HookPreCompact)
	if len(gates) != 2 {
		t.Errorf("expected 2 PreCompact gates, got %d", len(gates))
	}
}

func TestCheckInjectQueueEmpty_NoFile(t *testing.T) {
	dir := t.TempDir()
	ctx := GateContext{WorkDir: dir, SessionID: "test-session"}
	if !checkInjectQueueEmpty(ctx) {
		t.Error("missing queue file should be treated as empty")
	}
}

func TestCheckInjectQueueEmpty_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	queueDir := filepath.Join(dir, ".runtime", "inject-queue")
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queueDir, "test-session.jsonl"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := GateContext{WorkDir: dir, SessionID: "test-session"}
	if !checkInjectQueueEmpty(ctx) {
		t.Error("empty queue file should be treated as empty")
	}
}

func TestCheckInjectQueueEmpty_NonEmptyFile(t *testing.T) {
	dir := t.TempDir()
	queueDir := filepath.Join(dir, ".runtime", "inject-queue")
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"type":"inject","data":"test"}` + "\n")
	if err := os.WriteFile(filepath.Join(queueDir, "test-session.jsonl"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := GateContext{WorkDir: dir, SessionID: "test-session"}
	if checkInjectQueueEmpty(ctx) {
		t.Error("non-empty queue file should NOT be treated as empty")
	}
}

func TestCheckInjectQueueEmpty_NoWorkDir(t *testing.T) {
	ctx := GateContext{SessionID: "test"}
	if !checkInjectQueueEmpty(ctx) {
		t.Error("no workdir should fail open")
	}
}

func TestCheckInjectQueueEmpty_NoSession(t *testing.T) {
	ctx := GateContext{WorkDir: "/tmp"}
	if !checkInjectQueueEmpty(ctx) {
		t.Error("no session should fail open")
	}
}
