package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
)

// serverModeBeadsDir writes a server-mode metadata.json into a fresh beads dir
// and returns it. Nothing here starts a server: every assertion below is about
// the topology resolution, which happens before anything is dialed.
func serverModeBeadsDir(t *testing.T, cfg *configfile.Config) string {
	t.Helper()
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	require.NoError(t, os.MkdirAll(beadsDir, config.BeadsDirPerm))
	cfg.Backend = configfile.BackendDolt
	cfg.DoltMode = configfile.DoltModeServer
	require.NoError(t, cfg.Save(beadsDir))
	// doltserver.DefaultConfig consults these before metadata.json; an
	// inherited value from the developer's shell would decide the assertions.
	t.Setenv("BEADS_DOLT_SERVER_PORT", "")
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	t.Setenv("BEADS_DOLT_SERVER_DATABASE", "")
	t.Setenv("BEADS_DOLT_CREDENTIAL_COMMAND", "")
	return beadsDir
}

// The proxy serve builds must never reap itself. serve is its only client, and
// serve's pool drops its last connection after 5 minutes of no requests — at
// which point a finite-idle proxy exits and takes the OS-assigned port the
// provider's DSN already pinned, permanently, with serve still answering
// /healthz.
//
// Without the fix this resolves to a zero idle timeout, which
// NewExternalDoltServerUOWProvider turns into the 30s default.
func TestResolveServerModeUOWTopology_ProxyNeverIdlesOut(t *testing.T) {
	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost: "127.0.0.1",
		DoltServerPort: 3521,
		DoltDatabase:   "beads_serve",
	})

	topology, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.NoError(t, err)

	assert.Equal(t, proxy.IdleTimeoutNever, topology.proxyIdle,
		"a finite idle timeout lets the proxy exit during a quiet period and strands serve on a dead port")
	// A zero timeout is the specific value that reads as "use the default"
	// downstream, so pin that it is not what we send.
	assert.NotZero(t, topology.proxyIdle,
		"zero is the sentinel NewExternalDoltServerUOWProvider replaces with the 30s default")
}

// The database the handshake reports and the database the provider opens are
// the same string, and this is where it is decided. --global swaps it, so the
// swap has to be visible to the caller that reports it (runServe).
func TestResolveServerModeUOWTopology_GlobalSelectsTheGlobalDatabase(t *testing.T) {
	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost: "127.0.0.1",
		DoltServerPort: 3521,
		DoltDatabase:   "beads_project",
	})
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")

	old := globalFlag
	globalFlag = true
	t.Cleanup(func() { globalFlag = old })

	topology, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.NoError(t, err)
	assert.Equal(t, "beads_global", topology.database)
}

// B1: an endpoint inherited from the environment (another workspace's server,
// carried in this shell) must refuse rather than silently serve whatever
// database that endpoint happens to answer with. This topology is built
// straight from resolveDoltServerConnection, never through dolt.New, so it
// gets none of openWithEnvEndpointFallback's identity check or correction —
// serve has to refuse outright instead (gascity mc-3gbed).
func TestResolveServerModeUOWTopology_RefusesWhenEnvEndpointDivergesFromDeclared(t *testing.T) {
	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost: "127.0.0.1",
		DoltServerPort: 3521,
		DoltDatabase:   "beads_serve",
	})
	t.Setenv("BEADS_DOLT_SERVER_PORT", "45923")

	_, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to start")
	assert.Contains(t, err.Error(), "BEADS_DOLT_SERVER_PORT")
	assert.Contains(t, err.Error(), "127.0.0.1:3521",
		"the refusal must name this project's own declared endpoint, not just say one exists")
}

// The environment and the declared endpoint agreeing is the ordinary case —
// an operator's shell whose BEADS_DOLT_SERVER_PORT happens to match this
// project's own config.yaml/metadata.json — and must resolve exactly as
// before the B1 fix.
func TestResolveServerModeUOWTopology_AllowsEnvEndpointMatchingDeclared(t *testing.T) {
	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost: "127.0.0.1",
		DoltServerPort: 3521,
		DoltDatabase:   "beads_serve",
	})
	t.Setenv("BEADS_DOLT_SERVER_PORT", "3521")

	topology, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.NoError(t, err)
	assert.Equal(t, 3521, topology.external.Port)
}

// Loopback spellings are not a divergence: env "localhost" and a declared
// "127.0.0.1" (or the reverse) name the same machine, and comparing them
// lexically refused a server that was never actually inherited from another
// workspace (bd serve false refusal, gascity mc-3gbed follow-up).
func TestResolveServerModeUOWTopology_AllowsEnvEndpointLoopbackSpellingMismatch(t *testing.T) {
	for _, c := range []struct {
		name         string
		declaredHost string
		envHost      string
	}{
		{"declared 127.0.0.1, env localhost", "127.0.0.1", "localhost"},
		{"declared localhost, env 127.0.0.1", "localhost", "127.0.0.1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			beadsDir := serverModeBeadsDir(t, &configfile.Config{
				DoltServerHost: c.declaredHost,
				DoltServerPort: 3521,
				DoltDatabase:   "beads_serve",
			})
			t.Setenv("BEADS_DOLT_SERVER_HOST", c.envHost)
			t.Setenv("BEADS_DOLT_SERVER_PORT", "3521")

			topology, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
			require.NoError(t, err,
				"a loopback spelling that differs from the declared one must not read as an inherited server")
			assert.Equal(t, 3521, topology.external.Port)
		})
	}
}

// Shared-server mode is one server fronting every project by design, not a
// cross-project leak: envEndpointFallback already excludes it (S5's
// exclusion coverage), and this refusal must agree rather than block the
// documented shared-server deployment.
func TestResolveServerModeUOWTopology_SharedServerModeIsExcludedFromTheRefusal(t *testing.T) {
	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost: "127.0.0.1",
		DoltServerPort: 3521,
		DoltDatabase:   "beads_project",
	})
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")
	t.Setenv("BEADS_DOLT_SERVER_PORT", "45923")

	_, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.NoError(t, err,
		"shared-server mode's one server for every project is not a cross-project leak; envEndpointFallback excludes it the same way")
}

// The no-metadata.json corner. The CLI store open in this same process takes
// the literal default database name there (main.go's cfg == nil branch), and
// the whole point of routing serve through the same resolution is that the two
// cannot disagree about which database this workspace is.
//
// BEADS_DOLT_SERVER_DATABASE not being honored on that branch is a real gap,
// but it is the CLI's gap: fixing it on serve's side alone would give one
// process two databases.
func TestResolveServerModeUOWTopology_NoMetadataMatchesTheCLIDatabase(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	require.NoError(t, os.MkdirAll(beadsDir, config.BeadsDirPerm))
	t.Setenv("BEADS_DOLT_SERVER_PORT", "3521")
	t.Setenv("BEADS_DOLT_SERVER_HOST", "127.0.0.1")
	t.Setenv("BEADS_DOLT_SERVER_DATABASE", "from_the_environment")
	t.Setenv("BEADS_DOLT_CREDENTIAL_COMMAND", "")

	topology, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.NoError(t, err)
	assert.Equal(t, configfile.DefaultDoltDatabase, topology.database,
		"serve must open the database the CLI opens in this corner; main.go's cfg == nil branch ignores the env var")
}

// A stale socket path with a live TCP server is a workspace the CLI works in:
// dolt's store open probes the socket and transparently falls back to TCP. Serve
// has to make the same call or the parity this resolution exists for is a claim
// rather than a fact.
//
// Without the fix the socket wins unconditionally and serve fails to connect in
// a workspace where every CLI command succeeds.
func TestResolveServerModeUOWTopology_FallsBackToTCPWhenTheSocketIsDead(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost:   "127.0.0.1",
		DoltServerPort:   port,
		DoltServerSocket: filepath.Join(t.TempDir(), "not-a-live-socket.sock"),
		DoltDatabase:     "beads_serve",
	})
	t.Setenv("BEADS_DOLT_SERVER_PORT", strconv.Itoa(port))

	topology, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.NoError(t, err)

	assert.Empty(t, topology.external.Socket,
		"a dead socket must not win over a live TCP port; the CLI falls back and serve has to agree")
	assert.Equal(t, port, topology.external.Port)
	assert.Equal(t, "127.0.0.1", topology.external.Host)
}

// Socket selection belongs to the Dolt transport policy. The UOW topology
// must preserve the policy's answer, even when that answer is a socket that
// cannot be probed by this test.
func TestResolveServerModeUOWTopology_KeepsSocketSelectedByTransportPolicy(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "not-a-live-socket.sock")
	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost:   "127.0.0.1",
		DoltServerPort:   3521,
		DoltServerSocket: socket,
		DoltDatabase:     "beads_serve",
	})

	resolverCalls := 0
	topology, err := resolveServerModeUOWTopologyWithTransportResolver(context.Background(), beadsDir,
		func(gotSocket, gotHost string, gotPort int, gotTimeout time.Duration) string {
			resolverCalls++
			assert.Equal(t, socket, gotSocket)
			assert.Equal(t, "127.0.0.1", gotHost)
			assert.Equal(t, 3521, gotPort)
			assert.Equal(t, serverModeSocketProbeTimeout, gotTimeout)
			return socket
		})
	require.NoError(t, err)
	require.Equal(t, 1, resolverCalls, "transport policy must be invoked exactly once")
	assert.Equal(t, socket, topology.external.Socket)
	assert.Zero(t, topology.external.Host, "a selected socket is the transport; host must stay unset")
	assert.Zero(t, topology.external.Port, "a selected socket is the transport; port must stay unset")
}

// The gateway refusal must be decided from the CONFIGURATION, never from the
// result of running the credential command. Reaching it any other way means
// every refused `bd serve` has already spawned the operator's command and
// minted a short-lived identity token it is about to throw away.
//
// The command here fails loudly if it ever runs: without the fix the test sees
// that failure instead of the refusal.
func TestResolveServerModeUOWTopology_RefusesGatewayWithoutRunningTheCommand(t *testing.T) {
	beadsDir := serverModeBeadsDir(t, &configfile.Config{
		DoltServerHost: "127.0.0.1",
		DoltServerPort: 3521,
		DoltDatabase:   "beads_serve",
	})
	t.Setenv("BEADS_DOLT_CREDENTIAL_COMMAND", "exit 17")

	_, err := resolveServerModeUOWTopology(context.Background(), beadsDir)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "BEADS_DOLT_CREDENTIAL_COMMAND",
		"the refusal has to name what to change")
	assert.Contains(t, err.Error(), "short-lived")
	assert.NotContains(t, err.Error(), "resolving dolt credential command",
		"that wrapper means the command was executed before the refusal was decided")
	assert.NotContains(t, err.Error(), "17",
		"the command's own exit status can only appear if the command ran")
}

// The refusal names the knob to change and what serve cannot do, and stays a
// plain error: storage.ErrUnsupported carries a BACKEND, and typing this one
// would tell a caller that bd serve does not support dolt.
func TestErrServeGatewayCredential_SaysWhatItCannotDo(t *testing.T) {
	err := errServeGatewayCredential()
	require.Error(t, err)

	var unsupported *storage.ErrUnsupported
	assert.False(t, errors.As(err, &unsupported),
		"this is not a backend limitation: bd serve supports dolt, it cannot hold a refreshing identity")
	assert.Contains(t, err.Error(), "BEADS_DOLT_CREDENTIAL_COMMAND")
	assert.Contains(t, err.Error(), "cannot refresh it")
}
