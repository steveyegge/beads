package dolt

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/doltserver"
)

// fakeServers answers an open by the endpoint it targets: a store, the
// identity refusal, or another error. Endpoints not listed refuse to connect.
type fakeServers struct {
	stores    map[string]*DoltStore
	foreign   map[string]bool // the server holds another project: its id is "other-" + the endpoint
	opened    []string
	otherErrs map[string]error
}

func (f *fakeServers) open(_ context.Context, cfg *Config) (*DoltStore, error) {
	addr := net.JoinHostPort(cfg.ServerHost, strconv.Itoa(cfg.ServerPort))
	f.opened = append(f.opened, addr)
	switch {
	case f.foreign[addr]:
		return nil, projectIdentityMismatchError("local-project", "other-"+addr, "beads", false)
	case f.otherErrs[addr] != nil:
		return nil, f.otherErrs[addr]
	case f.stores[addr] != nil:
		return f.stores[addr], nil
	}
	return nil, errors.New("connection refused")
}

// beadsDirWith is a .beads dir whose config.yaml holds yaml (none when empty).
func beadsDirWith(t *testing.T, yaml string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func captureFallbackNote(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := envEndpointFallbackNote
	envEndpointFallbackNote = &buf
	t.Cleanup(func() { envEndpointFallbackNote = prev })
	return &buf
}

// TestEnvEndpointYieldsToTheProjectsDeclaredEndpointOnIdentityMismatch
// (gascity mc-3gbed). Kills the inherited BEADS_DOLT_SERVER_PORT of another
// workspace winning over the project's own explicit endpoint: on the identity
// refusal from the env's server, the project's declared endpoint is opened
// and verified, and used, with a note; a declared endpoint that serves
// another project too is refused with the original error.
func TestEnvEndpointYieldsToTheProjectsDeclaredEndpointOnIdentityMismatch(t *testing.T) {
	own := &DoltStore{}
	for _, c := range []struct {
		name     string
		yaml     string
		foreign  []string
		wantOpen []string
		want     *DoltStore
	}{
		{"the declared endpoint is the project's", "dolt:\n  host: 127.0.0.1\n  port: 3307\n", []string{"127.0.0.1:45865"}, []string{"127.0.0.1:45865", "127.0.0.1:3307"}, own},
		{"the declared port, the loopback by default", "dolt.port: 3307\n", []string{"127.0.0.1:45865"}, []string{"127.0.0.1:45865", "127.0.0.1:3307"}, own},
		{"the declared endpoint serves another project too", "dolt.port: 3307\n", []string{"127.0.0.1:45865", "127.0.0.1:3307"}, []string{"127.0.0.1:45865", "127.0.0.1:3307"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			note := captureFallbackNote(t)
			f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3307": own}, foreign: map[string]bool{}}
			for _, a := range c.foreign {
				f.foreign[a] = true
			}
			cfg := &Config{BeadsDir: beadsDirWith(t, c.yaml), ServerHost: "127.0.0.1", ServerPort: 45865, ServerPortSource: doltserver.PortSourceEnv}
			got, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open)
			if strings.Join(f.opened, " ") != strings.Join(c.wantOpen, " ") {
				t.Fatalf("opened %v, want %v", f.opened, c.wantOpen)
			}
			if c.want == nil {
				if got != nil || !isProjectIdentityMismatch(err) || !strings.Contains(err.Error(), "other-127.0.0.1:45865") || cfg.ServerPort != 45865 || note.Len() != 0 {
					t.Fatalf("got %v, %v, cfg port %d, note %q; want the original refusal (the env server's), cfg untouched, no note", got, err, cfg.ServerPort, note.String())
				}
				return
			}
			if err != nil || got != c.want || cfg.ServerPort != 3307 || cfg.ServerPortSource == doltserver.PortSourceEnv || !strings.Contains(note.String(), "127.0.0.1:3307") {
				t.Fatalf("got %v, %v, cfg %s:%d (%s), note %q; want the project's store, cfg on its endpoint, a note", got, err, cfg.ServerHost, cfg.ServerPort, cfg.ServerPortSource, note.String())
			}
		})
	}
}

// TestEnvEndpointFallbackStaysNarrow (gascity mc-3gbed). Kills a fallback
// that second-guesses anything but the case it is for: a port that did not
// come from the environment, a project that declares nothing, or declares
// the env's own endpoint, and any error but the identity refusal, all open
// once and return as before.
func TestEnvEndpointFallbackStaysNarrow(t *testing.T) {
	for _, c := range []struct {
		name   string
		yaml   string
		source doltserver.PortSource
		other  bool // the env's server fails with another error
	}{
		{"a port from the project's config", "dolt.port: 3307\n", doltserver.PortSourceConfigYaml, false},
		{"a project that declares nothing", "", doltserver.PortSourceEnv, false},
		{"a project that declares the env's endpoint", "dolt.port: 45865\n", doltserver.PortSourceEnv, false},
		{"another error", "dolt.port: 3307\n", doltserver.PortSourceEnv, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			note := captureFallbackNote(t)
			f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3307": {}}, foreign: map[string]bool{}, otherErrs: map[string]error{}}
			if c.other {
				f.otherErrs["127.0.0.1:45865"] = errors.New("server down")
			} else {
				f.foreign["127.0.0.1:45865"] = true
			}
			cfg := &Config{BeadsDir: beadsDirWith(t, c.yaml), ServerHost: "127.0.0.1", ServerPort: 45865, ServerPortSource: c.source}
			if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err == nil || len(f.opened) != 1 || note.Len() != 0 {
				t.Fatalf("err %v, opened %v, note %q; want the first open's error, one open, no note", err, f.opened, note.String())
			}
		})
	}
}

// TestProjectDeclaredEndpointReadsMetadataJSON: a project that declares its
// server in metadata.json (dolt_server_host/port) is honored when its
// config.yaml declares none.
func TestProjectDeclaredEndpointReadsMetadataJSON(t *testing.T) {
	dir := beadsDirWith(t, "")
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"dolt_server_host":"10.0.0.5","dolt_server_port":3307}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := projectDeclaredEndpoint(dir)
	if !ok || got.host != "10.0.0.5" || got.port != 3307 || got.source != doltserver.PortSourceMetadataJSON {
		t.Fatalf("declared endpoint %+v, %v; want metadata.json's 10.0.0.5:3307", got, ok)
	}
}
