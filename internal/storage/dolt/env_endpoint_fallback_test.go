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
	"sync"
	"testing"

	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/doltserver"
)

// fakeServers answers an open by the endpoint it targets: a store, the
// identity refusal, or another error. Endpoints not listed refuse to connect.
type fakeServers struct {
	stores    map[string]*DoltStore
	foreign   map[string]bool // the server holds another project: its id is "other-" + the endpoint
	opened    []string
	openedCfg []Config // a copy of cfg as open saw it, same index as opened
	otherErrs map[string]error
}

func (f *fakeServers) open(_ context.Context, cfg *Config) (*DoltStore, error) {
	addr := net.JoinHostPort(cfg.ServerHost, strconv.Itoa(cfg.ServerPort))
	f.opened = append(f.opened, addr)
	f.openedCfg = append(f.openedCfg, *cfg)
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
	prevOnce := envEndpointFallbackNoteOnce
	envEndpointFallbackNote = &buf
	envEndpointFallbackNoteOnce = &sync.Once{}
	t.Cleanup(func() {
		envEndpointFallbackNote = prev
		envEndpointFallbackNoteOnce = prevOnce
	})
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
		// 3308, not Dolt's historical default 3307: this suite's TestMain sets
		// BEADS_TEST_MODE=1 for the whole package, and the production-port
		// guard (S1) now applies to the retry too — 3307 is flagged
		// unconditionally (DefaultSQLPort) and would be rewritten to 1 before
		// the retry ever dials it, which is a different test than this one.
		{"the declared endpoint is the project's", "dolt:\n  host: 127.0.0.1\n  port: 3308\n", []string{"127.0.0.1:45865"}, []string{"127.0.0.1:45865", "127.0.0.1:3308"}, own},
		{"the declared port, the loopback by default", "dolt.port: 3308\n", []string{"127.0.0.1:45865"}, []string{"127.0.0.1:45865", "127.0.0.1:3308"}, own},
		{"the declared endpoint serves another project too", "dolt.port: 3308\n", []string{"127.0.0.1:45865", "127.0.0.1:3308"}, []string{"127.0.0.1:45865", "127.0.0.1:3308"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			note := captureFallbackNote(t)
			f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": own}, foreign: map[string]bool{}}
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
			if err != nil || got != c.want || cfg.ServerPort != 3308 || cfg.ServerPortSource == doltserver.PortSourceEnv || !strings.Contains(note.String(), "127.0.0.1:3308") {
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

// TestProjectDeclaredEndpointHostFollowsNormalPrecedence (S4). config.yaml's
// dolt.port names only the port; the host comes from metadata.json's
// dolt_server_host (bd's normal host precedence — configfile.Config.
// GetDoltServerHost puts metadata.json ahead of config.yaml), not the
// loopback default, and never from BEADS_DOLT_SERVER_HOST: that env var is
// exactly the kind of value inherited from another workspace this fallback
// exists to escape.
func TestProjectDeclaredEndpointHostFollowsNormalPrecedence(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "10.9.9.9") // must be ignored
	dir := beadsDirWith(t, "dolt.port: 3308\n")
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"dolt_server_host":"10.0.0.5"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := projectDeclaredEndpoint(dir)
	if !ok || got.host != "10.0.0.5" || got.port != 3308 || got.source != doltserver.PortSourceConfigYaml {
		t.Fatalf("declared endpoint %+v, %v; want metadata.json's host 10.0.0.5 with config.yaml's port 3308", got, ok)
	}
}

// TestEnvEndpointFallbackExcludesCreateIfMissing (S2). A CreateIfMissing open
// (bd init, bd bootstrap) must never fall back onto a database that may not
// exist yet on the project's declared endpoint — init and bootstrap
// deliberately ignore config ports (GH#2336), and the fallback replaying that
// ignored port would reintroduce exactly what GH#2336 fixed.
func TestEnvEndpointFallbackExcludesCreateIfMissing(t *testing.T) {
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv, CreateIfMissing: true,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err == nil || len(f.opened) != 1 || note.Len() != 0 {
		t.Fatalf("err %v, opened %v, note %q; want the first open's error, one open, no note", err, f.opened, note.String())
	}
}

// TestEnvEndpointFallbackExcludesConfiguredSocket (L1). A project configured
// for a unix socket must stay excluded from the fallback even though open's
// own socket-first/TCP-fallback resolution (ResolveSocketTransport) clears
// cfg.ServerSocket to "" as a side effect of the first open when the socket
// is dead but TCP is live — the exclusion must read the socket as it stood
// BEFORE that mutation, not after.
func TestEnvEndpointFallbackExcludesConfiguredSocket(t *testing.T) {
	note := captureFallbackNote(t)
	var opens int
	open := func(_ context.Context, cfg *Config) (*DoltStore, error) {
		opens++
		// Simulate newServerMode's own socket-first/TCP-fallback: the socket
		// is dead, TCP is live, so cfg.ServerSocket is cleared in place before
		// the identity check runs and refuses.
		cfg.ServerSocket = ""
		return nil, projectIdentityMismatchError("local-project", "other-env", "beads", false)
	}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv, ServerSocket: "/tmp/does-not-matter.sock",
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, open); err == nil || opens != 1 || note.Len() != 0 {
		t.Fatalf("err %v, opens %d, note %q; want the first open's error, one open, no note (socket-configured project excluded)", err, opens, note.String())
	}
}

// TestEnvEndpointFallbackRestrictsToLoopbackHosts (S4). A project whose
// declared endpoint is not on this machine is never dialed by the fallback:
// an identity refusal is already a sign something is not as expected, and the
// fallback keeps to the narrowest blast radius available.
func TestEnvEndpointFallbackRestrictsToLoopbackHosts(t *testing.T) {
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"10.0.0.9:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt:\n  host: 10.0.0.9\n  port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err == nil || len(f.opened) != 1 || note.Len() != 0 {
		t.Fatalf("err %v, opened %v, note %q; want the first open's error, one open, no note (non-loopback declared host excluded)", err, f.opened, note.String())
	}
}

// TestEnvEndpointFallbackExcludesGateway (S5). A gateway config (a configured
// credential command mints a short-lived token keyed to the ORIGINAL
// endpoint's identity) must stay excluded: retrying onto a different endpoint
// with that same token is not a correction, it is a misuse of someone else's
// credential.
func TestEnvEndpointFallbackExcludesGateway(t *testing.T) {
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv, Gateway: true,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err == nil || len(f.opened) != 1 || note.Len() != 0 {
		t.Fatalf("err %v, opened %v, note %q; want the first open's error, one open, no note (gateway excluded)", err, f.opened, note.String())
	}
}

// TestEnvEndpointFallbackExcludesGlobalDatabase (S5). The shared global
// database is not a per-project one, so there is no project-specific
// "declared endpoint" to retry onto in the first place.
func TestEnvEndpointFallbackExcludesGlobalDatabase(t *testing.T) {
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv, Database: doltserver.GlobalDatabaseName,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err == nil || len(f.opened) != 1 || note.Len() != 0 {
		t.Fatalf("err %v, opened %v, note %q; want the first open's error, one open, no note (global database excluded)", err, f.opened, note.String())
	}
}

// TestEnvEndpointFallbackExcludesSharedServerMode (S5). Shared-server mode is
// one server fronting every project by design, not a cross-project leak; the
// fallback must not second-guess that deployment's own one declared endpoint.
func TestEnvEndpointFallbackExcludesSharedServerMode(t *testing.T) {
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err == nil || len(f.opened) != 1 || note.Len() != 0 {
		t.Fatalf("err %v, opened %v, note %q; want the first open's error, one open, no note (shared-server mode excluded)", err, f.opened, note.String())
	}
}

// TestEnvEndpointFallbackExcludesEmptyBeadsDir (S5). Nothing is "declared"
// without a workspace to declare it; cfg.BeadsDir == "" must not panic or
// fall through to some other project's config.yaml/metadata.json by accident.
// config.yaml/metadata.json are read via filepath.Join(beadsDir, ...), so an
// empty beadsDir resolves to the process's current directory: this test
// chdirs into a directory that genuinely declares an endpoint (port 3308) to
// prove the exclusion is a real guard and not just an absence of files to
// stumble on.
func TestEnvEndpointFallbackExcludesEmptyBeadsDir(t *testing.T) {
	note := captureFallbackNote(t)
	dir := beadsDirWith(t, "dolt.port: 3308\n")
	t.Chdir(dir)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: "", ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err == nil || len(f.opened) != 1 || note.Len() != 0 {
		t.Fatalf("err %v, opened %v, note %q; want the first open's error, one open, no note (empty BeadsDir excluded even though cwd declares port 3308)", err, f.opened, note.String())
	}
}

// TestProjectDeclaredEndpointPrefersConfigYamlOverMetadataJSON (S5). When
// both config.yaml's dolt.port and metadata.json's dolt_server_port are
// present and disagree, config.yaml wins — matching envEndpointFallback's own
// doc comment ("config.yaml dolt.port, else metadata.json dolt_server_port").
func TestProjectDeclaredEndpointPrefersConfigYamlOverMetadataJSON(t *testing.T) {
	dir := beadsDirWith(t, "dolt.port: 3308\n")
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"dolt_server_host":"10.0.0.5","dolt_server_port":9999}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := projectDeclaredEndpoint(dir)
	if !ok || got.port != 3308 || got.source != doltserver.PortSourceConfigYaml {
		t.Fatalf("declared endpoint %+v, %v; want config.yaml's port 3308 to win over metadata.json's 9999", got, ok)
	}
}

// TestProjectDeclaredEndpointRejectsNonPositiveYamlPort (S5). dolt.port: 0 (or
// a negative value) in config.yaml is not a declaration — it must fall
// through to metadata.json rather than being read as "port 0, declared".
func TestProjectDeclaredEndpointRejectsNonPositiveYamlPort(t *testing.T) {
	dir := beadsDirWith(t, "dolt.port: 0\n")
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"dolt_server_host":"10.0.0.5","dolt_server_port":3307}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := projectDeclaredEndpoint(dir)
	if !ok || got.port != 3307 || got.source != doltserver.PortSourceMetadataJSON {
		t.Fatalf("declared endpoint %+v, %v; want dolt.port: 0 ignored, falling through to metadata.json's 3307", got, ok)
	}
}

// TestEnvEndpointFallbackRetriesTheDeclaredHostNotTheEnvHost (S5/S4). The
// retry's host must come from the declared endpoint, not be left over from
// the env-resolved cfg: a declared host that is a different loopback spelling
// ("localhost" vs the env's "127.0.0.1") proves the retry actually dials the
// declared host, not merely the declared port against the inherited host.
func TestEnvEndpointFallbackRetriesTheDeclaredHostNotTheEnvHost(t *testing.T) {
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"localhost:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt:\n  host: localhost\n  port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	got, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open)
	wantOpened := []string{"127.0.0.1:45865", "localhost:3308"}
	if strings.Join(f.opened, " ") != strings.Join(wantOpened, " ") {
		t.Fatalf("opened %v, want %v (the retry must dial the declared host \"localhost\", not the inherited \"127.0.0.1\")", f.opened, wantOpened)
	}
	if err != nil || got == nil || cfg.ServerHost != "localhost" {
		t.Fatalf("got %v, err %v, cfg host %q; want the retry to succeed on the declared host", got, err, cfg.ServerHost)
	}
	if note.Len() == 0 {
		t.Fatal("want a fallback note")
	}
}

// TestNewRoutesThroughEnvEndpointFallback (S5, B1 "the wiring in New"). New
// must always open through openWithEnvEndpointFallback — never call the
// server-mode opener directly — since that wrapper is the only thing that
// performs this retry at all. newServerModeOpener is stubbed to a fake so
// nothing here dials a real dolt sql-server.
func TestNewRoutesThroughEnvEndpointFallback(t *testing.T) {
	captureFallbackNote(t)
	prevOpener := newServerModeOpener
	t.Cleanup(func() { newServerModeOpener = prevOpener })
	own := &DoltStore{}
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": own}, foreign: map[string]bool{"127.0.0.1:45923": true}}
	newServerModeOpener = f.open

	cfg := &Config{
		Path:             filepath.Join(t.TempDir(), "dolt-data"),
		BeadsDir:         beadsDirWith(t, "dolt.port: 3308\n"),
		ServerHost:       "127.0.0.1",
		ServerPort:       45923,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	got, err := New(context.Background(), cfg)
	wantOpened := []string{"127.0.0.1:45923", "127.0.0.1:3308"}
	if strings.Join(f.opened, " ") != strings.Join(wantOpened, " ") {
		t.Fatalf("opened %v, want %v; New must retry through openWithEnvEndpointFallback, not call its opener directly", f.opened, wantOpened)
	}
	if err != nil || got != own {
		t.Fatalf("got %v, err %v; want New to return the project's own declared-endpoint store", got, err)
	}
}

// TestEnvEndpointFallbackJoinsRetryError (S3). When the retry also refuses on
// identity, the env endpoint's refusal stays the primary, actionable error,
// but the retry's own refusal is attached rather than discarded, and both
// stay reachable with errors.As.
func TestEnvEndpointFallbackJoinsRetryError(t *testing.T) {
	note := captureFallbackNote(t)
	f := &fakeServers{foreign: map[string]bool{"127.0.0.1:45865": true, "127.0.0.1:3308": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	_, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open)
	if !isProjectIdentityMismatch(err) {
		t.Fatalf("err %v; want errors.As to still reach a *projectIdentityMismatch", err)
	}
	if !strings.Contains(err.Error(), "other-127.0.0.1:45865") {
		t.Fatalf("err %v; want the env endpoint's refusal to stay primary", err)
	}
	if !strings.Contains(err.Error(), "other-127.0.0.1:3308") {
		t.Fatalf("err %v; want the retry's own refusal attached, not discarded", err)
	}
	if note.Len() != 0 {
		t.Fatalf("note %q; want no note when the retry itself is refused", note.String())
	}
}

// TestEnvEndpointFallbackProductionPortGuardAppliesToRetry (S1). The
// BEADS_TEST_MODE=1 production-port guard applies to the retry too, exactly
// as New()'s own applyConfigDefaults-then-panic sequence applies to the first
// open: a production port on the retry is rewritten to 1, not dialed, and if
// it is somehow still production after the rewrite, the guard panics. This
// test's own TestMain sets BEADS_TEST_MODE=1 for the whole package.
func TestEnvEndpointFallbackProductionPortGuardAppliesToRetry(t *testing.T) {
	if os.Getenv("BEADS_TEST_MODE") != "1" {
		t.Fatal("expected BEADS_TEST_MODE=1 from this package's TestMain")
	}
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:1": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		// DefaultSQLPort (3307): production, unconditionally, even under
		// BEADS_TEST_SERVER=1 (set by this package's TestMain).
		BeadsDir: beadsDirWith(t, "dolt.port: 3307\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	got, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open)
	wantOpened := []string{"127.0.0.1:45865", "127.0.0.1:1"}
	if strings.Join(f.opened, " ") != strings.Join(wantOpened, " ") {
		t.Fatalf("opened %v, want %v (the retry's production port rewritten to 1, never dialed at 3307)", f.opened, wantOpened)
	}
	if err != nil || got == nil || cfg.ServerPort != 1 || !strings.Contains(note.String(), "127.0.0.1:1") {
		t.Fatalf("got %v, err %v, cfg port %d, note %q; want the rewritten retry to succeed", got, err, cfg.ServerPort, note.String())
	}
}

// TestEnvEndpointFallbackReResolvesRetryCredentials (S4). The retry's
// password and TLS setting come from the declared endpoint's own
// configuration, not copied forward from the env endpoint: replaying another
// server's password onto this one is the same shape of cross-project leak
// this fallback exists to fix.
func TestEnvEndpointFallbackReResolvesRetryCredentials(t *testing.T) {
	t.Setenv("BEADS_DOLT_PASSWORD", "") // no explicit env override in play
	note := captureFallbackNote(t)
	credFile := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(credFile, []byte("[127.0.0.1:3308]\npassword=project-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_CREDENTIALS_FILE", credFile)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv, ServerUser: "city-robot", ServerPassword: "city-secret", ServerTLS: true,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err != nil {
		t.Fatalf("err %v; want the retry to succeed", err)
	}
	if note.Len() == 0 {
		t.Fatal("want a fallback note")
	}
	retryCfg := f.openedCfg[1]
	if retryCfg.ServerPassword != "project-secret" {
		t.Fatalf("retry password %q; want the declared endpoint's own project-secret, not the env endpoint's city-secret", retryCfg.ServerPassword)
	}
	if retryCfg.ServerUser != "root" {
		t.Fatalf("retry user %q; want the declared endpoint's own default root, not the env endpoint's city-robot", retryCfg.ServerUser)
	}
	if retryCfg.ServerTLS {
		t.Fatal("retry TLS true; want the declared endpoint's own (unset -> false), not the env endpoint's true")
	}
}

// TestEnvEndpointFallbackNoteRespectsQuietAndJSON (nit). The advisory note is
// output a normal user or script did not ask for; it must not appear under
// --quiet or --json.
func TestEnvEndpointFallbackNoteRespectsQuietAndJSON(t *testing.T) {
	for _, mode := range []struct {
		name  string
		setup func()
	}{
		{"quiet", func() { debug.SetQuiet(true) }},
		{"json", func() { debug.SetJSON(true) }},
	} {
		t.Run(mode.name, func(t *testing.T) {
			note := captureFallbackNote(t)
			mode.setup()
			t.Cleanup(func() { debug.SetQuiet(false); debug.SetJSON(false) })
			f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
			cfg := &Config{
				BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
				ServerPortSource: doltserver.PortSourceEnv,
			}
			if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err != nil {
				t.Fatalf("err %v; want the retry to succeed", err)
			}
			if note.Len() != 0 {
				t.Fatalf("note %q; want no note under --%s", note.String(), mode.name)
			}
		})
	}
}

// TestEnvEndpointFallbackNotePrintsOncePerProcess (nit). A process that
// repeats the same fallback (e.g. a global database open alongside a project
// one, both pointed at the same inherited env endpoint) gets the advisory
// exactly once, not once per open.
func TestEnvEndpointFallbackNotePrintsOncePerProcess(t *testing.T) {
	note := captureFallbackNote(t)
	open := func(dir string) (*DoltStore, error) {
		f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
		cfg := &Config{BeadsDir: dir, ServerHost: "127.0.0.1", ServerPort: 45865, ServerPortSource: doltserver.PortSourceEnv}
		return openWithEnvEndpointFallback(context.Background(), cfg, f.open)
	}
	dir := beadsDirWith(t, "dolt.port: 3308\n")
	if _, err := open(dir); err != nil {
		t.Fatalf("first open: %v", err)
	}
	firstLen := note.Len()
	if firstLen == 0 {
		t.Fatal("want a note on the first fallback")
	}
	if _, err := open(dir); err != nil {
		t.Fatalf("second open: %v", err)
	}
	if note.Len() != firstLen {
		t.Fatalf("note grew from %d to %d bytes on a second fallback in the same process; want it unchanged", firstLen, note.Len())
	}
}

// TestEnvEndpointFallbackNoteNamesActualEnvVar (nit). The note names the
// environment variable that actually supplied the port — the legacy
// BEADS_DOLT_PORT, when that is the one set, not a hardcoded
// BEADS_DOLT_SERVER_PORT the process never had.
func TestEnvEndpointFallbackNoteNamesActualEnvVar(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_PORT", "")
	t.Setenv("BEADS_DOLT_PORT", "45865")
	note := captureFallbackNote(t)
	f := &fakeServers{stores: map[string]*DoltStore{"127.0.0.1:3308": {}}, foreign: map[string]bool{"127.0.0.1:45865": true}}
	cfg := &Config{
		BeadsDir: beadsDirWith(t, "dolt.port: 3308\n"), ServerHost: "127.0.0.1", ServerPort: 45865,
		ServerPortSource: doltserver.PortSourceEnv,
	}
	if _, err := openWithEnvEndpointFallback(context.Background(), cfg, f.open); err != nil {
		t.Fatalf("err %v; want the retry to succeed", err)
	}
	if !strings.Contains(note.String(), "BEADS_DOLT_PORT") {
		t.Fatalf("note %q; want it to name BEADS_DOLT_PORT, the env var actually set", note.String())
	}
	if strings.Contains(note.String(), "BEADS_DOLT_SERVER_PORT") {
		t.Fatalf("note %q; want it not to claim BEADS_DOLT_SERVER_PORT, which this process never set", note.String())
	}
}
