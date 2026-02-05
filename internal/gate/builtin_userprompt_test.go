package gate

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestContextInjectionGate(t *testing.T) {
	g := ContextInjectionGate()
	if g.ID != "context-injection" {
		t.Errorf("expected ID 'context-injection', got %q", g.ID)
	}
	if g.Hook != HookUserPromptSubmit {
		t.Errorf("expected HookUserPromptSubmit, got %q", g.Hook)
	}
	if g.Mode != GateModeSoft {
		t.Errorf("expected soft, got %q", g.Mode)
	}
}

func TestStaleContextGate(t *testing.T) {
	g := StaleContextGate()
	if g.ID != "stale-context" {
		t.Errorf("expected ID 'stale-context', got %q", g.ID)
	}
	if g.Hook != HookUserPromptSubmit {
		t.Errorf("expected HookUserPromptSubmit, got %q", g.Hook)
	}
}

func TestRegisterUserPromptSubmitGates(t *testing.T) {
	reg := NewRegistry()
	RegisterUserPromptSubmitGates(reg)

	if reg.Count() != 2 {
		t.Errorf("expected 2 gates, got %d", reg.Count())
	}

	gates := reg.GatesForHook(HookUserPromptSubmit)
	if len(gates) != 2 {
		t.Errorf("expected 2 UserPromptSubmit gates, got %d", len(gates))
	}
}

func TestCheckContextFresh_NoFile(t *testing.T) {
	dir := t.TempDir()
	ctx := GateContext{WorkDir: dir, SessionID: "test"}
	if !checkContextFresh(ctx) {
		t.Error("missing activity file should fail open (fresh)")
	}
}

func TestCheckContextFresh_RecentActivity(t *testing.T) {
	dir := t.TempDir()
	sessionID := "test-recent"
	t.Setenv("BD_STALE_THRESHOLD_MINUTES", "")

	// Write a recent timestamp
	if err := TouchActivity(dir, sessionID); err != nil {
		t.Fatal(err)
	}

	ctx := GateContext{WorkDir: dir, SessionID: sessionID}
	if !checkContextFresh(ctx) {
		t.Error("recent activity should be fresh")
	}
}

func TestCheckContextFresh_StaleActivity(t *testing.T) {
	dir := t.TempDir()
	sessionID := "test-stale"
	t.Setenv("BD_STALE_THRESHOLD_MINUTES", "")

	// Write an old timestamp (2 hours ago)
	actDir := filepath.Join(dir, ".runtime", "activity")
	if err := os.MkdirAll(actDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTs := strconv.FormatInt(time.Now().Add(-2*time.Hour).Unix(), 10)
	if err := os.WriteFile(filepath.Join(actDir, sessionID), []byte(oldTs), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := GateContext{WorkDir: dir, SessionID: sessionID}
	if checkContextFresh(ctx) {
		t.Error("2-hour-old activity should be stale")
	}
}

func TestCheckContextFresh_CustomThreshold(t *testing.T) {
	dir := t.TempDir()
	sessionID := "test-custom"
	t.Setenv("BD_STALE_THRESHOLD_MINUTES", "120") // 2 hours

	// Write a 1-hour-old timestamp (within custom threshold)
	actDir := filepath.Join(dir, ".runtime", "activity")
	if err := os.MkdirAll(actDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := strconv.FormatInt(time.Now().Add(-1*time.Hour).Unix(), 10)
	if err := os.WriteFile(filepath.Join(actDir, sessionID), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := GateContext{WorkDir: dir, SessionID: sessionID}
	if !checkContextFresh(ctx) {
		t.Error("1-hour-old activity with 2-hour threshold should be fresh")
	}
}

func TestTouchActivity(t *testing.T) {
	dir := t.TempDir()
	sessionID := "test-touch"

	if err := TouchActivity(dir, sessionID); err != nil {
		t.Fatalf("TouchActivity failed: %v", err)
	}

	// Verify file exists and contains a valid timestamp
	data, err := os.ReadFile(filepath.Join(dir, ".runtime", "activity", sessionID))
	if err != nil {
		t.Fatalf("activity file not found: %v", err)
	}

	ts, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		t.Fatalf("invalid timestamp: %v", err)
	}

	// Should be within the last second
	if time.Since(time.Unix(ts, 0)) > 2*time.Second {
		t.Error("activity timestamp is too old")
	}
}
