package slackbot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateManager_PersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, "settings")

	// Create StateManager with a fake beads dir so it writes to our temp dir
	// The StateManager builds: beadsDir/../settings/slack_state.json
	// so we set beadsDir to dir/fake/.beads
	beadsDir := filepath.Join(dir, "fake", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sm := NewStateManager(beadsDir)

	// Verify it initialized empty
	cards := sm.AllAgentCards()
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards, got %d", len(cards))
	}

	// Set two agent cards
	if err := sm.SetAgentCard("gastown/polecats/furiosa", "C_RIG1", "1234567890.000001"); err != nil {
		t.Fatalf("SetAgentCard: %v", err)
	}
	if err := sm.SetAgentCard("gastown/crew/max", "C_RIG1", "1234567890.000002"); err != nil {
		t.Fatalf("SetAgentCard: %v", err)
	}

	// Verify file was written
	stateFile := filepath.Join(settingsDir, "slack_state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		// The path is beadsDir/../settings so: dir/fake/settings/slack_state.json
		stateFile = filepath.Join(dir, "fake", "settings", "slack_state.json")
	}
	// Verify the state manager's file path
	actualPath := sm.GetFilePath()
	if _, err := os.Stat(actualPath); os.IsNotExist(err) {
		t.Fatalf("state file not written at %s", actualPath)
	}

	// Create a NEW StateManager pointing at the same path — simulates restart
	sm2 := NewStateManager(beadsDir)

	// Verify it loaded the persisted cards
	ch, ts, ok := sm2.GetAgentCard("gastown/polecats/furiosa")
	if !ok {
		t.Fatal("expected furiosa card to be loaded")
	}
	if ch != "C_RIG1" || ts != "1234567890.000001" {
		t.Fatalf("unexpected furiosa card: ch=%s ts=%s", ch, ts)
	}

	ch, ts, ok = sm2.GetAgentCard("gastown/crew/max")
	if !ok {
		t.Fatal("expected max card to be loaded")
	}
	if ch != "C_RIG1" || ts != "1234567890.000002" {
		t.Fatalf("unexpected max card: ch=%s ts=%s", ch, ts)
	}

	// Verify unknown agent returns not-ok
	_, _, ok = sm2.GetAgentCard("citadel/warboys/nux")
	if ok {
		t.Fatal("expected unknown agent to return ok=false")
	}
}

func TestStateManager_RemoveAgentCard(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), "fake", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sm := NewStateManager(beadsDir)
	if err := sm.SetAgentCard("gastown/polecats/furiosa", "C1", "ts1"); err != nil {
		t.Fatal(err)
	}
	if err := sm.SetAgentCard("gastown/crew/max", "C1", "ts2"); err != nil {
		t.Fatal(err)
	}

	// Remove one
	if err := sm.RemoveAgentCard("gastown/polecats/furiosa"); err != nil {
		t.Fatal(err)
	}

	// Reload and verify
	sm2 := NewStateManager(beadsDir)
	_, _, ok := sm2.GetAgentCard("gastown/polecats/furiosa")
	if ok {
		t.Fatal("expected furiosa to be removed")
	}
	_, _, ok = sm2.GetAgentCard("gastown/crew/max")
	if !ok {
		t.Fatal("expected max to still exist")
	}
}

func TestStateManager_EmptyFileHandled(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), "fake", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// No state file exists — should load cleanly
	sm := NewStateManager(beadsDir)
	cards := sm.AllAgentCards()
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards from fresh state, got %d", len(cards))
	}
}

func TestStateManager_CorruptFileHandled(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), "fake", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sm := NewStateManager(beadsDir)
	settingsDir := filepath.Dir(sm.GetFilePath())
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write garbage to the state file
	if err := os.WriteFile(sm.GetFilePath(), []byte("not json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	// Loading should fail but not panic; NewStateManager ignores load errors
	sm2 := NewStateManager(beadsDir)
	cards := sm2.AllAgentCards()
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards after corrupt load, got %d", len(cards))
	}
}
