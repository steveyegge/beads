//go:build cgo && integration

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDoltDoctor_NoSQLiteWarningsAfterInitAndCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("dolt doctor integration test not supported on windows")
	}

	tmpDir := newCLIIntegrationRepo(t)

	socketPath := filepath.Join(tmpDir, ".beads", "bd.sock")
	env := cliIntegrationEnvWithNoDaemon("0",
		"BEADS_AUTO_START_DAEMON=true",
		"BD_SOCKET="+socketPath,
	)

	// Init dolt backend.
	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		skipIfDoltBackendUnavailable(t, initOut)
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	// Ensure daemon cleanup so temp dir removal doesn't flake.
	t.Cleanup(func() {
		_, _ = runBDExecAllowErrorWithEnv(t, tmpDir, env, "daemon", "stop")
		sockPath := filepath.Join(tmpDir, ".beads", "bd.sock")
		waitFor(t, 2*time.Second, 50*time.Millisecond, func() bool {
			_, err := os.Stat(sockPath)
			return os.IsNotExist(err)
		})
	})

	// Create one issue so the store is definitely initialized.
	createOut, createErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "create", "doctor dolt smoke", "--json")
	if createErr != nil {
		t.Fatalf("bd create failed: %v\n%s", createErr, createOut)
	}

	// Run doctor; it may return non-zero for unrelated warnings (upstream, claude, etc),
	// but it should NOT include SQLite-only failures on dolt.
	doctorOut, _ := runBDExecAllowErrorWithEnv(t, tmpDir, env, "doctor")

	// Also include stderr-like output if doctor wrote it to stdout in some modes.
	// (CombinedOutput already captures both.)
	for _, forbidden := range []string{
		"No beads.db found",
		"Unable to read database version",
		"Legacy database",
	} {
		if strings.Contains(doctorOut, forbidden) {
			t.Fatalf("bd doctor printed sqlite-specific warning %q in dolt mode; output:\n%s", forbidden, doctorOut)
		}
	}

	// Regression check: dolt init must NOT create a SQLite database file.
	if _, err := os.Stat(filepath.Join(tmpDir, ".beads", "beads.db")); err == nil {
		t.Fatalf("unexpected sqlite database created in dolt mode: %s", filepath.Join(tmpDir, ".beads", "beads.db"))
	}
}
