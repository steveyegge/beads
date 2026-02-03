package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCascadeCommand(t *testing.T) {
	origJsonOutput := jsonOutput
	defer func() { jsonOutput = origJsonOutput }()
	jsonOutput = false

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Setenv("BEADS_DIR", beadsDir); err != nil {
		t.Fatalf("set BEADS_DIR: %v", err)
	}
	defer os.Unsetenv("BEADS_DIR")

	skillsPath, historyPath, err := wobbleStorePaths()
	if err != nil {
		t.Fatalf("wobble store paths: %v", err)
	}

	snapshot := wobbleStore{
		Version:     1,
		GeneratedAt: time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC),
		Skills: []wobbleSkill{{
			ID:          "beads",
			Verdict:     "stable",
			ChangeState: "stable",
			Dependents:  []string{"spec-tracker", "pacman"},
		}},
	}
	entry := buildWobbleHistoryEntry("tester", snapshot.GeneratedAt, snapshot.Skills)
	if err := writeWobbleStore(skillsPath, historyPath, snapshot, entry); err != nil {
		t.Fatalf("write wobble store: %v", err)
	}

	output := captureCascadeOutput(t, "beads")
	if !strings.Contains(output, "spec-tracker") {
		t.Fatalf("expected dependent spec-tracker, got: %s", output)
	}
	if !strings.Contains(output, "pacman") {
		t.Fatalf("expected dependent pacman, got: %s", output)
	}
}

func captureCascadeOutput(t *testing.T, skill string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	wobbleCascadeCmd.Run(wobbleCascadeCmd, []string{skill})

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	return buf.String()
}
