//go:build cgo

package doctor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/testutil"
)

func TestRunDoltPerformanceDiagnostics_RequiresServer(t *testing.T) {
	// Server-only mode: diagnostics require a running dolt sql-server.
	// Without a server, RunDoltPerformanceDiagnostics should return an error.
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write config pointing to a nonexistent database
	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.DoltMode = configfile.DoltModeServer
	cfg.DoltServerHost = "127.0.0.1"
	cfg.DoltServerPort = doctorTestServerPort()
	cfg.DoltDatabase = "doctest_perf_nonexistent"
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	_, err := RunDoltPerformanceDiagnostics(tmpDir, false)
	if err == nil {
		t.Fatal("expected error when no dolt server is running")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "not running") && !strings.Contains(errStr, "not reachable") && !strings.Contains(errStr, "database not found") {
		t.Errorf("expected server/database error, got: %v", err)
	}
}

func TestRunDoltPerformanceDiagnosticsDeclineNonDoltBackend(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfg := &configfile.Config{Backend: configfile.BackendSQLite, SQLitePath: "beads.db"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("save SQLite config: %v", err)
	}

	_, err := RunDoltPerformanceDiagnostics(tmpDir, false)
	if err == nil {
		t.Fatal("expected Dolt-only diagnostics to decline a non-Dolt workspace")
	}
	message := err.Error()
	if !strings.Contains(message, "Dolt") || !strings.Contains(message, `"sqlite"`) {
		t.Fatalf("diagnostic error should explain the backend mismatch: %v", err)
	}
}

// TestRunDoltPerformanceDiagnosticsRemoteHost pins the endpoint bd doctor
// --perf dials for a server on another machine: the configured host, which the
// store dials, on DialConfig's port, never 127.0.0.1. When nothing answers
// there it gives the remote-host advice, not `bd dolt start`, which refuses a
// remote host.
func TestRunDoltPerformanceDiagnosticsRemoteHost(t *testing.T) {
	tmpDir, port := perfServerWorkspace(t, "db.invalid")

	_, err := RunDoltPerformanceDiagnostics(tmpDir, false)
	if err == nil {
		t.Fatal("performance diagnostics succeeded with no server at db.invalid")
	}
	msg := err.Error()
	for _, want := range []string{net.JoinHostPort("db.invalid", strconv.Itoa(port)), fmt.Sprintf("nc -zv db.invalid %d", port)} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not contain %q:\n%s", want, msg)
		}
	}
	for _, banned := range []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "bd dolt start", "externally managed"} {
		if strings.Contains(msg, banned) {
			t.Errorf("error contains %q:\n%s", banned, msg)
		}
	}
}

// TestRunDoltPerformanceDiagnosticsCentralConfigHost pins that bd doctor
// --perf does not dial a host set only in the central server config
// (~/.config/beads/server.json). The commands it times (bd ready, bd list,
// bd show) open the store with metadata.json alone, so they dial 127.0.0.1
// here, and --perf measures that server and gives their advice for it,
// although bd doctor's store checks (dolt.NewFromConfig*) apply the file.
func TestRunDoltPerformanceDiagnosticsCentralConfigHost(t *testing.T) {
	tmpDir, port := perfServerWorkspace(t, "")
	centralPath := filepath.Join(tmpDir, "server.json")
	if err := os.WriteFile(centralPath, []byte(`{"dolt_server_host":"db.invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_CENTRAL_CONFIG", centralPath)

	_, err := RunDoltPerformanceDiagnostics(tmpDir, false)
	if err == nil {
		t.Fatal("performance diagnostics succeeded with no server at 127.0.0.1")
	}
	msg := err.Error()
	for _, want := range []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "externally managed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not contain %q:\n%s", want, msg)
		}
	}
	for _, banned := range []string{"db.invalid", "bd dolt start"} {
		if strings.Contains(msg, banned) {
			t.Errorf("error contains %q:\n%s", banned, msg)
		}
	}
}

// perfServerWorkspace writes a server-mode metadata.json naming host, when it
// is not empty, and a free port nothing listens on, with no Dolt server
// variable set, and returns the project directory and the port.
func perfServerWorkspace(t *testing.T, host string) (string, int) {
	t.Helper()
	for _, name := range []string{"BEADS_DOLT_SHARED_SERVER", "BEADS_DOLT_SERVER_MODE", "BEADS_DOLT_SERVER_PORT",
		"BEADS_DOLT_PORT", "BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_SOCKET"} {
		t.Setenv(name, "")
	}
	config.ResetForTesting()
	port, err := testutil.FindFreePort()
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.DoltMode = configfile.DoltModeServer
	cfg.DoltServerHost = host
	cfg.DoltServerPort = port
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return tmpDir, port
}
