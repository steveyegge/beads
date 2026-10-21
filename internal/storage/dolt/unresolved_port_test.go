package dolt

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/doltserver"
)

// bd-7pkvvd (PR #7729): a server-mode open whose port never resolved (port 0,
// no socket) on an open that will not auto-start must diagnose the unresolved
// port instead of dialing 127.0.0.1:0. These tests call newServerMode directly
// so applyConfigDefaults' BEADS_TEST_MODE port pin cannot turn port 0 into
// port 1 behind them (the port_provenance_test.go pattern).

// unresolvedForbidden holds the substrings the unresolved-port error must
// never contain: "bd dolt start" advice mints a phantom database when bd does
// not manage the server, and the remaining phrases misreport a missing
// address as an outage at an address nobody dials.
var unresolvedForbidden = []string{
	"bd dolt start",
	"unreachable",
	"127.0.0.1:0",
	"connection refused",
	"auto-start is disabled",
}

func assertNotContainsAny(t *testing.T, msg string, forbidden []string) {
	t.Helper()
	for _, bad := range forbidden {
		if strings.Contains(msg, bad) {
			t.Errorf("message contains forbidden %q:\n%s", bad, msg)
		}
	}
}

// TestUnresolvedServerPortError pins the pure helper's shape (AC-1): nil for a
// resolved transport, the diagnosis line first, every port source from
// PortSourceLabels named, and a remedy that never sends an externally-managed
// workspace to `bd dolt start`.
func TestUnresolvedServerPortError(t *testing.T) {
	beadsDir := t.TempDir()
	portFile := filepath.Join(beadsDir, doltserver.PortFileName)

	t.Run("nil for a configured socket", func(t *testing.T) {
		cfg := &Config{BeadsDir: beadsDir, ServerSocket: "/tmp/x.sock", ServerPort: 0, AutoStart: false}
		if err := unresolvedServerPortError(cfg, beadsDir, beadsDir); err != nil {
			t.Fatalf("expected nil for a configured socket, got: %v", err)
		}
	})
	t.Run("nil for a resolved port", func(t *testing.T) {
		cfg := &Config{BeadsDir: beadsDir, ServerPort: 33917, AutoStart: false}
		if err := unresolvedServerPortError(cfg, beadsDir, beadsDir); err != nil {
			t.Fatalf("expected nil for a resolved port, got: %v", err)
		}
	})

	t.Run("auto-start disabled names every port source", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		cfg := &Config{BeadsDir: beadsDir, ServerPort: 0, AutoStart: false}
		err := unresolvedServerPortError(cfg, beadsDir, beadsDir)
		if err == nil {
			t.Fatal("expected an error for an unresolved port")
		}
		msg := err.Error()
		if first := msg[:strings.IndexByte(msg, '\n')]; first != "no Dolt server port resolved for "+beadsDir {
			t.Errorf("first line = %q, want %q", first, "no Dolt server port resolved for "+beadsDir)
		}
		for _, want := range []string{
			doltserver.PortFileName,
			portFile,
			"BEADS_DOLT_SERVER_PORT",
			"listener.port",
			"dolt.port",
			"dolt_server_port",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message missing port-source token %q:\n%s", want, msg)
			}
		}
		assertNotContainsAny(t, msg, unresolvedForbidden)
		if !strings.Contains(msg, "dolt.auto-start is off") {
			t.Errorf("remedy does not say auto-start is off:\n%s", msg)
		}
		if !strings.Contains(msg, portFile) {
			t.Errorf("remedy does not name the port file %s:\n%s", portFile, msg)
		}
	})

	t.Run("externally managed without an explicit disable", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "")
		cfg := &Config{BeadsDir: beadsDir, ServerPort: 0, AutoStart: false}
		err := unresolvedServerPortError(cfg, beadsDir, beadsDir)
		if err == nil {
			t.Fatal("expected an error for an unresolved port")
		}
		msg := err.Error()
		if !strings.Contains(msg, "externally managed") {
			t.Errorf("remedy does not say the server is externally managed:\n%s", msg)
		}
		if strings.Contains(msg, "dolt.auto-start is off") {
			t.Errorf("remedy claims auto-start is off when nothing disabled it:\n%s", msg)
		}
		assertNotContainsAny(t, msg, unresolvedForbidden)
	})

	t.Run("strict readonly may name bd dolt start", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "")
		cfg := &Config{BeadsDir: beadsDir, Path: filepath.Join(beadsDir, "dolt"),
			ServerPort: 0, AutoStart: true, DisableAutoStart: true}
		err := unresolvedServerPortError(cfg, beadsDir, beadsDir)
		if err == nil {
			t.Fatal("expected an error for an unresolved port")
		}
		if !strings.Contains(err.Error(), "bd dolt start") {
			t.Errorf("bd-managed remedy must name bd dolt start:\n%s", err)
		}
		for _, bad := range []string{"unreachable", "127.0.0.1:0", "connection refused", "auto-start is disabled"} {
			if strings.Contains(err.Error(), bad) {
				t.Errorf("message contains forbidden %q:\n%s", bad, err)
			}
		}
	})

	t.Run("library config with no path may name bd dolt start", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "")
		cfg := &Config{BeadsDir: beadsDir, ServerPort: 0, AutoStart: true}
		err := unresolvedServerPortError(cfg, beadsDir, beadsDir)
		if err == nil {
			t.Fatal("expected an error for an unresolved port")
		}
		if !strings.Contains(err.Error(), "bd dolt start") {
			t.Errorf("bd-managed remedy must name bd dolt start:\n%s", err)
		}
	})
}

// TestNewServerModeUnresolvedPort pins the newServerMode call site (AC-1,
// AC-2, AC-3): port-0 opens that will not start a server stop dialing :0, and
// every resolved transport, auto-start-capable open, and externally-managed
// outage keeps today's behavior.
func TestNewServerModeUnresolvedPort(t *testing.T) {
	stubNever := func(t *testing.T) {
		t.Helper()
		orig := ensureRunningDetailed
		t.Cleanup(func() { ensureRunningDetailed = orig })
		ensureRunningDetailed = func(beadsDir string) (int, bool, error) {
			t.Fatalf("ensureRunningDetailed must not be called for this open (beadsDir %s)", beadsDir)
			return 0, false, nil
		}
	}

	t.Run("auto-start off with no port reports the unresolved port", func(t *testing.T) {
		t.Setenv("BEADS_TEST_MODE", "1")
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		stubNever(t)
		beadsDir := t.TempDir()
		cfg := &Config{Database: "test_unresolved_callsite", BeadsDir: beadsDir,
			Path: filepath.Join(beadsDir, "dolt"), ServerHost: "127.0.0.1",
			ServerPort: 0, AutoStart: false}
		_, err := newServerMode(context.Background(), cfg)
		if err == nil || !strings.Contains(err.Error(), "no Dolt server port resolved") {
			t.Fatalf("expected the unresolved-port error, got: %v", err)
		}
		assertNotContainsAny(t, err.Error(), unresolvedForbidden)
	})

	t.Run("BEADS_DOLT_AUTO_START=false spelling reports the unresolved port", func(t *testing.T) {
		// resolveAutoStart honours only the literal "0"; IsAutoStartDisabled
		// also accepts "false" (be-0eyj's isFalsyBool). Today this spelling
		// slips past the disable check into the auto-start path.
		t.Setenv("BEADS_TEST_MODE", "1")
		t.Setenv("BEADS_DOLT_AUTO_START", "false")
		stubNever(t)
		beadsDir := t.TempDir()
		cfg := &Config{Database: "test_unresolved_spelling", BeadsDir: beadsDir,
			Path: filepath.Join(beadsDir, "dolt"), ServerHost: "127.0.0.1",
			ServerPort: 0, AutoStart: true}
		_, err := newServerMode(context.Background(), cfg)
		if err == nil || !strings.Contains(err.Error(), "no Dolt server port resolved") {
			t.Fatalf("expected the unresolved-port error, got: %v", err)
		}
		assertNotContainsAny(t, err.Error(), unresolvedForbidden)
	})

	t.Run("auto-start on still reaches ensure-running", func(t *testing.T) {
		t.Setenv("BEADS_TEST_MODE", "1")
		t.Setenv("BEADS_DOLT_AUTO_START", "")
		beadsDir := t.TempDir()
		calls := new(int)
		orig := ensureRunningDetailed
		t.Cleanup(func() { ensureRunningDetailed = orig })
		ensureRunningDetailed = func(string) (int, bool, error) {
			*calls++
			return 0, false, fmt.Errorf("stub: auto-start refused")
		}
		cfg := &Config{Database: "test_unresolved_autostart", BeadsDir: beadsDir,
			Path: filepath.Join(beadsDir, "dolt"), ServerHost: "127.0.0.1",
			ServerPort: 0, AutoStart: true}
		_, err := newServerMode(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected an error from the stubbed auto-start")
		}
		if *calls != 1 {
			t.Fatalf("ensureRunningDetailed called %d times, want 1", *calls)
		}
		if !strings.Contains(err.Error(), "auto-start failed") {
			t.Fatalf("expected the auto-start-failed error, got: %v", err)
		}
		if strings.Contains(err.Error(), "no Dolt server port resolved") {
			t.Fatalf("an auto-start-capable open must not report the unresolved-port error: %v", err)
		}
	})

	t.Run("closed local port with auto-start off keeps today's error", func(t *testing.T) {
		t.Setenv("BEADS_TEST_MODE", "1")
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		ln, dialErr := net.Listen("tcp", "127.0.0.1:0")
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		cfg := &Config{Database: "test_unresolved_closed_port", BeadsDir: t.TempDir(),
			ServerHost: "127.0.0.1", ServerPort: port, AutoStart: false}
		_, err := newServerMode(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected a connection error for a closed port")
		}
		msg := err.Error()
		if !strings.Contains(msg, fmt.Sprintf("Dolt server unreachable at 127.0.0.1:%d", port)) {
			t.Errorf("expected today's unreachable error, got: %s", msg)
		}
		if !strings.Contains(msg, "Dolt server auto-start is disabled (dolt.auto-start: false).") {
			t.Errorf("expected today's auto-start-disabled hint, got: %s", msg)
		}
		if strings.Contains(msg, "no Dolt server port resolved") {
			t.Errorf("a resolved port must not report the unresolved-port error: %s", msg)
		}
	})

	t.Run("missing socket with auto-start off keeps today's hint", func(t *testing.T) {
		t.Setenv("BEADS_TEST_MODE", "1")
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		cfg := &Config{Database: "test_unresolved_socket", BeadsDir: t.TempDir(),
			ServerSocket: filepath.Join(t.TempDir(), "missing.sock"),
			ServerHost:   "127.0.0.1", ServerPort: 0, AutoStart: false}
		_, err := newServerMode(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected a socket connection error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "The Dolt server is not listening on socket") {
			t.Errorf("expected today's socket hint, got: %s", msg)
		}
		if strings.Contains(msg, "no Dolt server port resolved") {
			t.Errorf("a configured socket must not report the unresolved-port error: %s", msg)
		}
	})

	t.Run("remote host with a resolved port keeps today's hint", func(t *testing.T) {
		t.Setenv("BEADS_TEST_MODE", "1")
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		cfg := &Config{Database: "test_unresolved_remote", BeadsDir: t.TempDir(),
			ServerHost: "192.0.2.1", ServerPort: 1, AutoStart: false}
		_, err := newServerMode(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected a connection error for the remote host")
		}
		msg := err.Error()
		if !strings.Contains(msg, "Verify the external server is running") {
			t.Errorf("expected today's external-server hint (GH#3518), got: %s", msg)
		}
		if strings.Contains(msg, "no Dolt server port resolved") {
			t.Errorf("a resolved remote port must not report the unresolved-port error: %s", msg)
		}
	})
}
