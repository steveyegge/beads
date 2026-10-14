package dolt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/doltserver"
)

// projectIdentityMismatch is verifyProjectIdentity's refusal: the database the
// resolved endpoint serves belongs to another project.
type projectIdentityMismatch struct{ err error }

func (e *projectIdentityMismatch) Error() string { return e.err.Error() }

// isProjectIdentityMismatch reports whether err is verifyProjectIdentity's
// refusal.
func isProjectIdentityMismatch(err error) bool {
	var m *projectIdentityMismatch
	return errors.As(err, &m)
}

// declaredEndpoint is the server endpoint a project's own .beads declares.
type declaredEndpoint struct {
	host   string
	port   int
	source doltserver.PortSource
}

// envEndpointFallbackNote receives the note the fallback prints when it opens
// the project's own endpoint instead of the inherited one. A var so tests can
// capture it.
var envEndpointFallbackNote io.Writer = os.Stderr

// envEndpointFallbackNoteOnce makes the note fire at most once per process:
// a long-running or multi-store process (e.g. a global database open
// alongside a project one) that keeps hitting the same inherited env endpoint
// would otherwise repeat the same advisory on every open. A pointer (not a
// plain sync.Once) so tests that need a fresh note per case can swap it via
// captureFallbackNote without copying the lock sync.Once embeds.
var envEndpointFallbackNoteOnce = &sync.Once{}

// openWithEnvEndpointFallback opens cfg with open and, when the open is
// refused for a project identity mismatch on an endpoint that came from the
// environment (BEADS_DOLT_SERVER_PORT, an agent's inherited server for
// ANOTHER project), retries once on the endpoint the project's own .beads
// explicitly declares, when it declares one that differs. A session that
// carries its own workspace's server in its environment and runs bd inside a
// different checkout otherwise reached the wrong server, and the identity
// guard refused (gascity mc-3gbed). The retry is verified exactly as the
// first open was: a project whose declared endpoint serves another project
// too is refused, with the original error. A note on stderr names the switch.
func openWithEnvEndpointFallback(ctx context.Context, cfg *Config, open func(context.Context, *Config) (*DoltStore, error)) (*DoltStore, error) {
	// L1: cfg.ServerSocket is a shared field; open's own socket-first/
	// TCP-fallback logic (ResolveSocketTransport, gt-28itz) may clear it as a
	// side effect of the call below, when a configured socket is dead but TCP
	// is live. Captured here, before that mutation, so a project that
	// explicitly configured a socket stays excluded from this fallback even
	// though cfg.ServerSocket reads "" by the time envEndpointFallback would
	// otherwise see it.
	originalSocket := cfg.ServerSocket

	store, err := open(ctx, cfg)
	if err == nil || !isProjectIdentityMismatch(err) {
		return store, err
	}
	alt, ok := envEndpointFallback(cfg, originalSocket)
	if !ok {
		return nil, err
	}
	retry := *cfg
	retry.ServerHost, retry.ServerPort, retry.ServerPortSource = alt.host, alt.port, alt.source

	// S4: re-resolve the declared endpoint's own user/password/TLS instead of
	// carrying the env endpoint's forward. Replaying another server's
	// credentials onto this one is the same shape of cross-project leak this
	// fallback exists to fix, and the two servers are not guaranteed to share
	// an operator, let alone a password.
	fileCfg, _ := configfile.Load(cfg.BeadsDir)
	if fileCfg == nil {
		// No metadata.json (or it failed to load): fall back to the same
		// zero-value config every other reader of a missing metadata.json
		// uses (open.go's applyResolvedConfig), so GetDoltServerUser/TLS still
		// resolve their own documented defaults instead of silently leaving
		// the env endpoint's values in place.
		fileCfg = configfile.DefaultConfig()
	}
	retry.ServerUser = fileCfg.GetDoltServerUser()
	retry.ServerTLS = fileCfg.GetDoltServerTLS()
	if envPass := os.Getenv("BEADS_DOLT_PASSWORD"); envPass != "" {
		// An explicit env override is a deliberate operator action, not an
		// ambient value inherited from another workspace; honor it for the
		// retry the same way applyConfigDefaults honors it for the first
		// open. Looked up directly (not via the alt endpoint's own
		// GetDoltServerPasswordForPort) so the retry's password is keyed on
		// alt.host:alt.port specifically, not whatever host cfg's file
		// resolves on its own.
		retry.ServerPassword = envPass
	} else {
		retry.ServerPassword = configfile.LookupCredentialsPassword(alt.host, alt.port)
	}

	// S1: New()'s BEADS_TEST_MODE=1 production-port guard (applyConfigDefaults'
	// rewrite-to-1, then New()'s defense-in-depth panic if still production)
	// ran once already, against cfg, before this fallback was ever reached.
	// This retry calls open directly (newServerMode, not New), bypassing both
	// a second time, so they are reapplied here verbatim: a test process must
	// never land the retry on a production port either.
	if os.Getenv("BEADS_TEST_MODE") == "1" {
		if retry.ServerPort == 0 || isProductionPort(&retry) {
			retry.ServerPort = 1
		}
		if isProductionPort(&retry) {
			panic(buildTestModeProductionPortPanic(&retry))
		}
	}

	altStore, altErr := open(ctx, &retry)
	if altErr != nil {
		// S3: the identity refusal from the env endpoint stays the primary,
		// actionable error (an inherited server for another project); the
		// retry's own failure is attached rather than silently discarded,
		// and both stay reachable with errors.As.
		return nil, errors.Join(err, altErr)
	}
	if w := envEndpointFallbackNote; w != nil && !debug.IsQuiet() && !debug.IsJSON() {
		envEndpointFallbackNoteOnce.Do(func() {
			// retry.ServerHost/Port, not alt.host/port: the S1 guard above may
			// have rewritten retry.ServerPort (BEADS_TEST_MODE=1) after alt was
			// computed, and the note must name the endpoint actually used.
			fmt.Fprintf(w, "bd: the inherited server %s (%s) serves another project; using this project's own endpoint %s from %s\n", //nolint:errcheck // best-effort note
				net.JoinHostPort(cfg.ServerHost, strconv.Itoa(cfg.ServerPort)), envEndpointPortVarName(), net.JoinHostPort(retry.ServerHost, strconv.Itoa(retry.ServerPort)), alt.source)
		})
	}
	*cfg = retry
	return altStore, nil
}

// envEndpointPortVarName names the environment variable that actually
// supplied cfg's port, for the fallback note. BEADS_DOLT_SERVER_PORT is
// checked first, matching applyConfigDefaults' own precedence;
// BEADS_DOLT_PORT is the legacy fallback. envEndpointFallback already
// requires cfg.ServerPortSource == doltserver.PortSourceEnv before this is
// reached, so one of the two was set.
func envEndpointPortVarName() string {
	if os.Getenv("BEADS_DOLT_SERVER_PORT") != "" {
		return "BEADS_DOLT_SERVER_PORT"
	}
	return "BEADS_DOLT_PORT"
}

// envEndpointFallback reports the endpoint to retry when cfg's came from the
// environment: the one the project's own .beads explicitly declares
// (config.yaml dolt.port, else metadata.json dolt_server_port, with the
// matching host, 127.0.0.1 by default), when it differs from cfg's. bd's own
// port-file bookkeeping is not a declaration and does not count; nor does a
// socket, a gateway, the shared server, or an open that may create the
// database (S2: init and bootstrap deliberately ignore config ports, GH#2336,
// and this fallback must never be the thing that creates a database on a
// server nobody declared). originalSocket is cfg.ServerSocket as it stood
// before the first open attempt, since open's own transport resolution may
// have since cleared it (L1).
func envEndpointFallback(cfg *Config, originalSocket string) (declaredEndpoint, bool) {
	if cfg == nil || cfg.ServerPortSource != doltserver.PortSourceEnv || cfg.BeadsDir == "" ||
		originalSocket != "" || cfg.Gateway || cfg.Database == doltserver.GlobalDatabaseName ||
		cfg.CreateIfMissing || doltserver.IsSharedServerModeForDir(cfg.BeadsDir) {
		return declaredEndpoint{}, false
	}
	alt, ok := projectDeclaredEndpoint(cfg.BeadsDir)
	if !ok || (alt.host == cfg.ServerHost && alt.port == cfg.ServerPort) {
		return declaredEndpoint{}, false
	}
	// S4: never retry onto a non-loopback host. The declared endpoint is read
	// from this project's own config, which bd otherwise trusts, but this
	// fallback fires only on an identity refusal — exactly when something is
	// already not as expected — so it stays inside the narrowest blast radius
	// available: the machine bd is already running on.
	if !isLocalHost(alt.host) {
		return declaredEndpoint{}, false
	}
	return alt, true
}

// projectDeclaredEndpoint reads the endpoint beadsDir's own config explicitly
// declares.
func projectDeclaredEndpoint(beadsDir string) (declaredEndpoint, bool) {
	meta, _ := configfile.Load(beadsDir)
	// declaredHost mirrors bd's normal host precedence — metadata.json
	// dolt_server_host before config.yaml dolt.host, matching
	// configfile.Config.GetDoltServerHost — minus the environment.
	// BEADS_DOLT_SERVER_HOST is exactly the kind of value inherited from
	// another workspace that this fallback exists to escape; consulting it
	// here would make the declared and the env-resolved endpoint agree only
	// by coincidence (S4).
	declaredHost := func() string {
		if meta != nil && meta.DoltServerHost != "" {
			return meta.DoltServerHost
		}
		return strings.TrimSpace(config.GetStringFromDir(beadsDir, "dolt.host"))
	}
	if raw := strings.TrimSpace(config.GetStringFromDir(beadsDir, "dolt.port")); raw != "" {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 {
			return declaredEndpoint{host: hostOrLoopback(declaredHost()), port: port, source: doltserver.PortSourceConfigYaml}, true
		}
	}
	if meta != nil && meta.DoltServerPort > 0 {
		return declaredEndpoint{host: hostOrLoopback(declaredHost()), port: meta.DoltServerPort, source: doltserver.PortSourceMetadataJSON}, true
	}
	return declaredEndpoint{}, false
}

func hostOrLoopback(host string) string {
	if host = strings.TrimSpace(host); host != "" {
		return host
	}
	return "127.0.0.1"
}

// EnvEndpointDivergesFromDeclared reports whether cfg's server endpoint came
// from the environment (BEADS_DOLT_SERVER_PORT / legacy BEADS_DOLT_PORT) and
// differs from the endpoint this project's own .beads explicitly declares —
// envEndpointFallback's same eligibility test, without an actual open.
//
// Exported for bd serve's provider arm (cmd/bd/uow_factory.go's
// resolveServerModeUOWTopologyWithTransportResolver), which builds its own
// topology straight from resolveDoltServerConnection rather than through
// dolt.New, so it gets neither openWithEnvEndpointFallback's automatic
// correction nor the identity check that guards it: left alone, serve would
// run MigrateUpWithLock against — and then front an HTTP server on — whatever
// database the inherited endpoint happens to answer with, no project-identity
// check at all (B1, gascity mc-3gbed). serve refuses outright instead, since
// confirming an actual identity mismatch here would need the same migration
// + verification work this narrower fix is deliberately avoiding.
//
// cfg.ServerSocket must be the value resolveDoltServerConnection produced,
// unresolved against the live transport: unlike newServerMode's socket-first/
// TCP-fallback (ResolveSocketTransport), the caller's own transport
// resolution keeps its result in a local variable and never mutates
// cfg.ServerSocket, so there is no earlier snapshot to thread through here.
func EnvEndpointDivergesFromDeclared(cfg *Config) (declared string, ok bool) {
	alt, found := envEndpointFallback(cfg, cfg.ServerSocket)
	if !found {
		return "", false
	}
	return net.JoinHostPort(alt.host, strconv.Itoa(alt.port)), true
}
