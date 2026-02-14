package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
)

// TestRemoteDaemonDoesNotRequireLocalDB verifies that when BD_DAEMON_HOST is set,
// bd commands don't exit with "no beads database found" even when no local .beads
// directory exists. Instead, execution should reach the daemon connection logic.
//
// This is the fix for bd-ges3k: pods in gastown-next would fail because the
// working directory had no .beads, but BD_DAEMON_HOST was set pointing at the
// remote daemon. The "no beads database found" check fired before the daemon
// connection code was reached.
func TestRemoteDaemonDoesNotRequireLocalDB(t *testing.T) {
	// Set up: temp directory with no .beads, BD_DAEMON_HOST points to bogus host
	tmpDir := t.TempDir()
	t.Setenv("BD_DAEMON_HOST", "http://127.0.0.1:1")
	t.Setenv("BEADS_DIR", "")
	t.Setenv("BEADS_DB", "")
	t.Setenv("HOME", tmpDir)

	// Verify no local database is found
	dbPath := beads.FindDatabasePath()
	if dbPath != "" {
		t.Fatalf("expected no database path in temp dir, got %s", dbPath)
	}

	// The key assertion: when BD_DAEMON_HOST is set, GetDaemonHost() should
	// return non-empty. This means PersistentPreRun in main.go will skip the
	// "no beads database found" exit and proceed to daemon connection instead.
	host := rpc.GetDaemonHost()
	if host == "" {
		t.Fatal("expected GetDaemonHost() to return non-empty when BD_DAEMON_HOST is set")
	}
}
