package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteWobbleStore(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	skillsPath := filepath.Join(tmpDir, "skills.json")

	generatedAt := time.Date(2026, 2, 3, 8, 15, 0, 0, time.UTC)
	store := wobbleStore{
		Version:     1,
		GeneratedAt: generatedAt,
		Skills: []wobbleSkill{{
			ID:          "beads",
			Verdict:     "stable",
			ChangeState: "stable",
			Signals:     []string{"ok"},
			Dependents:  []string{"spec-tracker"},
		}},
	}
	entry := wobbleHistoryEntry{
		Actor:     "claude",
		CreatedAt: generatedAt,
		Stable:    1,
		Wobbly:    0,
		Unstable:  0,
		Skills:    []string{"beads"},
	}

	if err := writeWobbleStore(skillsPath, historyPath, store, entry); err != nil {
		t.Fatalf("write wobble store: %v", err)
	}

	loaded, history, err := loadWobbleStore(skillsPath, historyPath)
	if err != nil {
		t.Fatalf("load wobble store: %v", err)
	}
	if loaded.Version != 1 {
		t.Fatalf("expected version 1, got %d", loaded.Version)
	}
	if len(loaded.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(loaded.Skills))
	}
	if loaded.Skills[0].ID != "beads" {
		t.Fatalf("expected beads skill entry")
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Actor != "claude" {
		t.Fatalf("expected actor claude, got %s", history[0].Actor)
	}

	// Ensure files exist
	if _, err := os.Stat(skillsPath); err != nil {
		t.Fatalf("missing store file: %v", err)
	}
	if _, err := os.Stat(historyPath); err != nil {
		t.Fatalf("missing history file: %v", err)
	}
}
