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

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
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
	store, err := open(ctx, cfg)
	if err == nil || !isProjectIdentityMismatch(err) {
		return store, err
	}
	alt, ok := envEndpointFallback(cfg)
	if !ok {
		return nil, err
	}
	retry := *cfg
	retry.ServerHost, retry.ServerPort, retry.ServerPortSource = alt.host, alt.port, alt.source
	altStore, altErr := open(ctx, &retry)
	if altErr != nil {
		return nil, err
	}
	if w := envEndpointFallbackNote; w != nil {
		fmt.Fprintf(w, "bd: the inherited server %s (BEADS_DOLT_SERVER_PORT) serves another project; using this project's own endpoint %s from %s\n", //nolint:errcheck // best-effort note
			net.JoinHostPort(cfg.ServerHost, strconv.Itoa(cfg.ServerPort)), net.JoinHostPort(alt.host, strconv.Itoa(alt.port)), alt.source)
	}
	*cfg = retry
	return altStore, nil
}

// envEndpointFallback reports the endpoint to retry when cfg's came from the
// environment: the one the project's own .beads explicitly declares
// (config.yaml dolt.port, else metadata.json dolt_server_port, with the
// matching host, 127.0.0.1 by default), when it differs from cfg's. bd's own
// port-file bookkeeping is not a declaration and does not count; nor does a
// socket, a gateway, or the shared server.
func envEndpointFallback(cfg *Config) (declaredEndpoint, bool) {
	if cfg == nil || cfg.ServerPortSource != doltserver.PortSourceEnv || cfg.BeadsDir == "" ||
		cfg.ServerSocket != "" || cfg.Gateway || cfg.Database == doltserver.GlobalDatabaseName ||
		doltserver.IsSharedServerModeForDir(cfg.BeadsDir) {
		return declaredEndpoint{}, false
	}
	alt, ok := projectDeclaredEndpoint(cfg.BeadsDir)
	if !ok || (alt.host == cfg.ServerHost && alt.port == cfg.ServerPort) {
		return declaredEndpoint{}, false
	}
	return alt, true
}

// projectDeclaredEndpoint reads the endpoint beadsDir's own config explicitly
// declares.
func projectDeclaredEndpoint(beadsDir string) (declaredEndpoint, bool) {
	if raw := strings.TrimSpace(config.GetStringFromDir(beadsDir, "dolt.port")); raw != "" {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 {
			host := strings.TrimSpace(config.GetStringFromDir(beadsDir, "dolt.host"))
			return declaredEndpoint{host: hostOrLoopback(host), port: port, source: doltserver.PortSourceConfigYaml}, true
		}
	}
	if meta, err := configfile.Load(beadsDir); err == nil && meta != nil && meta.DoltServerPort > 0 {
		return declaredEndpoint{host: hostOrLoopback(meta.DoltServerHost), port: meta.DoltServerPort, source: doltserver.PortSourceMetadataJSON}, true
	}
	return declaredEndpoint{}, false
}

func hostOrLoopback(host string) string {
	if host = strings.TrimSpace(host); host != "" {
		return host
	}
	return "127.0.0.1"
}
