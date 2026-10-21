package doctor

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
)

func TestRunServerHealthChecksSQLiteIsNotAMigrationWarning(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := (&configfile.Config{Backend: configfile.BackendSQLite, SQLitePath: "beads.db"}).Save(beadsDir); err != nil {
		t.Fatalf("save SQLite config: %v", err)
	}

	// The sqlite backend is a removed-backend tombstone: server checks report a
	// clearly-attributed non-Dolt warning with no migration fix, and never edit
	// the workspace.
	result := RunServerHealthChecks(tmpDir)
	if result.OverallOK || len(result.Checks) != 1 {
		t.Fatalf("SQLite server-health result = %#v, want one non-Dolt warning", result)
	}
	check := result.Checks[0]
	if check.Status != StatusWarning || !strings.Contains(check.Message, "sqlite") || check.Fix != "" {
		t.Fatalf("SQLite server-health check = %#v, want non-Dolt warning without migration fix", check)
	}
}

// TestRunServerHealthChecksDialsLegacyPortEnv pins `bd doctor --server` to
// the port the store dials when only the legacy BEADS_DOLT_PORT names one:
// it checks that endpoint instead of reporting no configured port and
// advising an auto-start bd never performs for an externally managed server.
func TestRunServerHealthChecksDialsLegacyPortEnv(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	result := RunServerHealthChecks(legacyPortServerWorkspace(t, port))
	want := "Cannot connect to " + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for _, check := range result.Checks {
		if check.Name == "Server Reachable" && check.Message == want {
			return
		}
	}
	t.Fatalf("server-health checks = %#v, want a %q check", result.Checks, want)
}

// TestRunServerHealthChecksVersionCheckDialsLegacyPortEnv is the live half:
// with something listening on the legacy BEADS_DOLT_PORT, the reachability
// check passes, and the version check must dial that same port, not port 0.
// The listener speaks no MySQL, so the version check fails, but only after it
// connected.
func TestRunServerHealthChecksVersionCheckDialsLegacyPortEnv(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			_ = conn.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	result := RunServerHealthChecks(legacyPortServerWorkspace(t, port))

	reached, versionChecked := false, false
	for _, check := range result.Checks {
		switch check.Name {
		case "Server Reachable":
			reached = check.Status == StatusOK
		case "Dolt Version":
			versionChecked = true
		}
	}
	if !reached || !versionChecked {
		t.Fatalf("server-health checks = %#v, want a passing reachability check and a version check", result.Checks)
	}
	// The reachability probe is one connection; the version check makes the
	// second.
	if n := accepted.Load(); n < 2 {
		t.Errorf("the listener on BEADS_DOLT_PORT %d accepted %d connection(s), so the version check dialed another port", port, n)
	}
}

// legacyPortServerWorkspace makes a Dolt server-mode workspace whose only port
// setting is the legacy BEADS_DOLT_PORT, set to port, and returns its root.
func legacyPortServerWorkspace(t *testing.T, port int) string {
	t.Helper()
	for _, name := range []string{"BEADS_DOLT_SHARED_SERVER", "BEADS_DOLT_SERVER_MODE", "BEADS_DOLT_SERVER_PORT",
		"BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_SOCKET"} {
		t.Setenv(name, "")
	}
	t.Setenv("BEADS_DOLT_PORT", strconv.Itoa(port))
	config.ResetForTesting()

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := (&configfile.Config{Backend: configfile.BackendDolt, DoltMode: configfile.DoltModeServer}).Save(beadsDir); err != nil {
		t.Fatalf("save Dolt server config: %v", err)
	}
	return tmpDir
}
