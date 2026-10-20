package dolt

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/testutil"
)

// externalPortAdviceEnv clears every setting that classifies a workspace's
// server, so each case below names exactly the one it is about.
func externalPortAdviceEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BEADS_SHARED_SERVER_DIR", filepath.Join(home, "shared-server"))
	for _, name := range []string{
		"BEADS_DOLT_SERVER_MODE", "BEADS_DOLT_SHARED_SERVER", "BEADS_DOLT_SERVER_HOST",
		"BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_PORT", "BEADS_DOLT_AUTO_START", "BEADS_DOLT_SERVER_SOCKET",
	} {
		t.Setenv(name, "")
	}
	config.ResetForTesting()
	t.Cleanup(config.ResetForTesting)
}

// externalPortAdviceWorkspace writes a server-mode metadata.json, naming
// metadataPort as dolt_server_port when it is nonzero.
func externalPortAdviceWorkspace(t *testing.T, metadataPort int) string {
	t.Helper()
	beadsDir := t.TempDir()
	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.DoltMode = configfile.DoltModeServer
	cfg.DoltDatabase = "advice"
	cfg.DoltServerPort = metadataPort
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}
	return beadsDir
}

func deadLoopbackPort(t *testing.T) int {
	t.Helper()
	port, err := testutil.FindFreePort()
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	return port
}

// TestNewServerModeExternallyManagedPortAdvice pins the advice for a dead
// endpoint whose port a setting names, which also declares the server
// externally managed: BEADS_DOLT_SERVER_PORT or BEADS_DOLT_PORT
// (ResolveServerMode check 2c), or metadata.json's dolt_server_port (check 4).
// bd starts no server for such a workspace, and `bd dolt start` would start
// its own over .beads/dolt on that port, so the advice names the endpoint that
// was dialed and the setting, says bd did not start a server, and never
// suggests `bd dolt start`. It must hold on both opens: the one that may not
// auto-start (every ordinary command, since ApplyCLIAutoStart turns
// auto-start off for an external server) and the one that may (bd init builds
// its config by hand), where EnsureRunning refuses instead of starting.
func TestNewServerModeExternallyManagedPortAdvice(t *testing.T) {
	cases := []struct {
		name         string
		envName      string // env var naming the port, or ""
		metadataPort bool
		source       doltserver.PortSource
		openedByInit bool
		setting      string
	}{
		{"BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_SERVER_PORT", false, doltserver.PortSourceEnv, false, "BEADS_DOLT_SERVER_PORT"},
		{"legacy BEADS_DOLT_PORT", "BEADS_DOLT_PORT", false, doltserver.PortSourceEnv, false, "BEADS_DOLT_PORT"},
		{"metadata.json dolt_server_port", "", true, doltserver.PortSourceMetadataJSON, false, "metadata.json"},
		// bd init promotes --server-port into BEADS_DOLT_SERVER_PORT and
		// passes the port itself as caller-explicit.
		{"bd init --server-port", "BEADS_DOLT_SERVER_PORT", false, doltserver.PortSourceCallerExplicit, true, "--server-port"},
	}
	for _, autoStart := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(tc.name+"/auto-start="+strconv.FormatBool(autoStart), func(t *testing.T) {
				externalPortAdviceEnv(t)
				port := deadLoopbackPort(t)
				addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
				if tc.envName != "" {
					t.Setenv(tc.envName, strconv.Itoa(port))
				}
				metadataPort := 0
				if tc.metadataPort {
					metadataPort = port
				}
				beadsDir := externalPortAdviceWorkspace(t, metadataPort)
				cfg := &Config{
					Path:             filepath.Join(beadsDir, "dolt"),
					BeadsDir:         beadsDir,
					Database:         "advice",
					ServerHost:       "127.0.0.1",
					ServerPort:       port,
					ServerPortSource: tc.source,
					AutoStart:        autoStart,
					OpenedByInit:     tc.openedByInit,
				}

				_, err := newServerMode(context.Background(), cfg)
				if err == nil {
					t.Fatalf("open of the dead endpoint %s succeeded", addr)
				}
				msg := err.Error()
				for _, want := range []string{
					tc.setting + " declares the Dolt server at " + addr + " externally managed",
					"did not start",
				} {
					if !strings.Contains(msg, want) {
						t.Errorf("error does not contain %q:\n%s", want, msg)
					}
				}
				for _, banned := range []string{"bd dolt start", "auto-start failed"} {
					if strings.Contains(msg, banned) {
						t.Errorf("error contains %q:\n%s", banned, msg)
					}
				}
				var opErr *net.OpError
				if !errors.As(err, &opErr) {
					t.Errorf("error does not wrap the failed dial, so callers cannot tell it is one:\n%s", msg)
				}
				if _, statErr := os.Stat(filepath.Join(beadsDir, doltserver.PortFileName)); !os.IsNotExist(statErr) {
					t.Errorf("open wrote %s for a server it did not start (stat: %v)", doltserver.PortFileName, statErr)
				}
			})
		}
	}
}

// TestNewServerModeUnreachableAdviceOutsideNamedPorts pins the controls: an
// endpoint whose port no such setting names keeps its own advice, `bd dolt
// start` included, because bd may own that server. A port from bd's own port
// file (beside a deprecated metadata port that outranks nothing), the
// shared-server port, and a bd-owned workspace with auto-start turned off.
func TestNewServerModeUnreachableAdviceOutsideNamedPorts(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(t *testing.T, port int)
		metadataPort bool
		source       doltserver.PortSource
		want         string
	}{
		{"port file beside a metadata port", nil, true, doltserver.PortSourcePortFile, "The Dolt server may not be running"},
		{"shared server", func(t *testing.T, port int) {
			t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")
			t.Setenv("BEADS_DOLT_SERVER_PORT", strconv.Itoa(port))
		}, false, doltserver.PortSourceEnv, "The Dolt server may not be running"},
		{"owned, auto-start off", func(t *testing.T, _ int) {
			t.Setenv("BEADS_DOLT_AUTO_START", "0")
		}, false, doltserver.PortSourcePortFile, "auto-start is disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			externalPortAdviceEnv(t)
			port := deadLoopbackPort(t)
			if tc.setup != nil {
				tc.setup(t, port)
			}
			metadataPort := 0
			if tc.metadataPort {
				metadataPort = deadLoopbackPort(t)
			}
			beadsDir := externalPortAdviceWorkspace(t, metadataPort)
			cfg := &Config{
				Path:             filepath.Join(beadsDir, "dolt"),
				BeadsDir:         beadsDir,
				Database:         "advice",
				ServerHost:       "127.0.0.1",
				ServerPort:       port,
				ServerPortSource: tc.source,
			}

			_, err := newServerMode(context.Background(), cfg)
			if err == nil {
				t.Fatal("open of a dead endpoint succeeded")
			}
			msg := err.Error()
			for _, want := range []string{tc.want, "bd dolt start"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error does not contain %q:\n%s", want, msg)
				}
			}
			if strings.Contains(msg, "externally managed") {
				t.Errorf("error calls a server bd may own externally managed:\n%s", msg)
			}
		})
	}
}
