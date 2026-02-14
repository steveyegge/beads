package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoltWorkspace(t *testing.T, workspaceDir string) (beadsDir string, doltDir string) {
	t.Helper()
	beadsDir = filepath.Join(workspaceDir, ".beads")
	doltDir = filepath.Join(beadsDir, "dolt")
	if err := os.MkdirAll(doltDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	metadata := `{
  "database": "dolt",
  "backend": "dolt"
}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0o600); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	return beadsDir, doltDir
}

func TestDoltSingleProcess_ShouldAutoStartDaemonFalse(t *testing.T) {
	oldDBPath := dbPath
	t.Cleanup(func() { dbPath = oldDBPath })
	dbPath = ""

	ws := t.TempDir()
	beadsDir, _ := writeDoltWorkspace(t, ws)

	t.Setenv("BEADS_DIR", beadsDir)
	// Ensure the finder sees a workspace root (and not the repo running tests).
	oldWD, _ := os.Getwd()
	_ = os.Chdir(ws)
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	if shouldAutoStartDaemon() {
		t.Fatalf("expected shouldAutoStartDaemon() to be false for dolt backend")
	}
}

func TestDoltSingleProcess_TryAutoStartDoesNotCreateStartlock(t *testing.T) {
	oldDBPath := dbPath
	t.Cleanup(func() { dbPath = oldDBPath })
	dbPath = ""

	ws := t.TempDir()
	beadsDir, _ := writeDoltWorkspace(t, ws)
	t.Setenv("BEADS_DIR", beadsDir)

	socketPath := filepath.Join(ws, "bd.sock")
	lockPath := socketPath + ".startlock"

	ok := tryAutoStartDaemon(socketPath)
	if ok {
		t.Fatalf("expected tryAutoStartDaemon() to return false for dolt backend")
	}
	if _, err := os.Stat(lockPath); err == nil {
		t.Fatalf("expected startlock not to be created for dolt backend: %s", lockPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat startlock: %v", err)
	}
}

func TestDoltSingleProcess_DaemonGuardBlocksStartCommand(t *testing.T) {
	oldDBPath := dbPath
	t.Cleanup(func() { dbPath = oldDBPath })
	dbPath = ""

	ws := t.TempDir()
	beadsDir, _ := writeDoltWorkspace(t, ws)
	t.Setenv("BEADS_DIR", beadsDir)

	// Use daemonStartCmd which has the guard attached.
	// Ensure help and federation flags exist (cobra adds them during execution).
	cmd := daemonStartCmd
	cmd.Flags().Bool("help", false, "help")
	// Note: federation flag is already registered in init()
	err := guardDaemonStartForDolt(cmd, nil)
	if err == nil {
		t.Fatalf("expected daemon guard error for dolt backend without --federation")
	}
	// Guardrail wording may evolve; assert a stable intent.
	if !strings.Contains(err.Error(), "daemon mode is not supported") {
		t.Fatalf("expected error to mention daemon mode unsupported, got: %v", err)
	}
}
