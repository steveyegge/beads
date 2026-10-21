package doctor

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

	result := RunServerHealthChecks(tmpDir)
	want := "Cannot connect to " + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for _, check := range result.Checks {
		if check.Name == "Server Reachable" && check.Message == want {
			return
		}
	}
	t.Fatalf("server-health checks = %#v, want a %q check", result.Checks, want)
}
