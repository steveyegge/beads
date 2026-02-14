package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRemoteDaemonDoesNotRequireLocalDB verifies that when BD_DAEMON_HOST is set,
// bd commands don't exit with "no beads database found" even when no local .beads
// directory exists. Instead, execution should reach the daemon connection logic
// and fail with a connection error (or succeed if a daemon is actually running).
//
// This is the fix for bd-ges3k: pods in gastown-next would fail because the
// working directory had no .beads, but BD_DAEMON_HOST was set pointing at the
// remote daemon. The "no beads database found" check fired before the daemon
// connection code was reached.
func TestRemoteDaemonDoesNotRequireLocalDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	exe, err := exec.LookPath("bd")
	if err != nil {
		t.Skip("bd binary not in PATH, skipping")
	}

	// Run bd in a temp directory with no .beads, but with BD_DAEMON_HOST set.
	// Use a bogus host so the daemon connection will fail — but the error should
	// be about the connection, NOT about "no beads database found".
	tmpDir := t.TempDir()

	// Initialize a git repo so bd doesn't complain about missing git
	gitInit := exec.Command("git", "init", "--initial-branch=main")
	gitInit.Dir = tmpDir
	if err := gitInit.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	cmd := exec.Command(exe, "list")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"BD_DAEMON_HOST=http://127.0.0.1:1", // bogus port, will fail to connect
		"BEADS_DIR=",                          // clear any inherited BEADS_DIR
		"BEADS_DB=",                           // clear any inherited BEADS_DB
	)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// The command should fail (bogus daemon host), but NOT with "no beads database found"
	if err == nil {
		t.Fatal("expected error (bogus daemon host), but command succeeded")
	}

	if strings.Contains(outputStr, "no beads database found") {
		t.Fatalf("got 'no beads database found' error — the fix for bd-ges3k is not working.\n"+
			"When BD_DAEMON_HOST is set, bd should attempt daemon connection instead of "+
			"requiring a local .beads directory.\nOutput: %s", outputStr)
	}

	// Should instead fail with a daemon connection error
	if !strings.Contains(outputStr, "failed to connect") &&
		!strings.Contains(outputStr, "connection refused") &&
		!strings.Contains(outputStr, "daemon") {
		t.Logf("unexpected error output (expected daemon connection error): %s", outputStr)
	}
}
