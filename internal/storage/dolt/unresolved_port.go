package dolt

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/doltserver"
)

// unresolvedServerPortError reports the open failure for a server-mode
// workspace whose port never resolved: ServerPort 0 (never a dial target —
// it means "not yet resolved", and only auto-start ever fills it in) and no
// socket, on an open that will not start a server. Before this, such an open
// dialed 127.0.0.1:0 and reported "Dolt server unreachable" with "Start the
// server manually: bd dolt start" — both halves wrong when the server is
// externally managed: it may be healthy on its real port, and `bd dolt start`
// does not reconnect to it; it dolt-inits an empty data directory, launches a
// second unsupervised server, and publishes that port, which shadows the real
// database (bd-7pkvvd / PR #7729).
//
// The remedy never sends an externally-managed workspace to `bd dolt start`,
// and never says "auto-start is disabled": that phrase plus "bd dolt start" is
// the pair Gas City's `gc bd` wrapper rewrites, and the new text is accurate
// on its own.
//
// Pure: formats only. Reads env/config only through
// doltserver.IsAutoStartDisabled.
func unresolvedServerPortError(cfg *Config, beadsDir, serverDir string) error {
	if cfg == nil || cfg.ServerSocket != "" || cfg.ServerPort > 0 {
		return nil
	}
	portFile := filepath.Join(serverDir, doltserver.PortFileName)
	var b strings.Builder
	fmt.Fprintf(&b, "no Dolt server port resolved for %s\n\n", beadsDir)
	b.WriteString("No port source produced a server address for this workspace, so there is nothing to connect to.\n")
	b.WriteString("Port sources, in priority order:\n")
	for i, label := range doltserver.PortSourceLabels() {
		if strings.Contains(label, doltserver.PortFileName) {
			// The port file is the one source whose value lives on this
			// machine; name its exact location and that it is missing.
			fmt.Fprintf(&b, "  %d. %s: %s (missing)\n", i+1, label, portFile)
			continue
		}
		fmt.Fprintf(&b, "  %d. %s\n", i+1, label)
	}
	b.WriteString("\n")
	if doltserver.IsAutoStartDisabled() || !cfg.AutoStart {
		// bd does not manage this server (auto-start off, or the open was
		// configured not to auto-start, e.g. an externally-managed server
		// mode), so bd cannot start it and must not try: `bd dolt start`
		// would create a new, empty database rather than reconnect.
		if doltserver.IsAutoStartDisabled() {
			b.WriteString("dolt.auto-start is off, so this server is managed outside bd and bd cannot start it.\n")
		} else {
			b.WriteString("This server is externally managed, so bd cannot start it.\n")
		}
		fmt.Fprintf(&b, "Whoever manages the server must publish its port again — normally by writing %s —\n", portFile)
		b.WriteString("or pin the port with BEADS_DOLT_SERVER_PORT or dolt.port in config.yaml.")
	} else {
		// bd manages this server, but this open may not start one (strict
		// --readonly / DisableAutoStart, or a library Config with no Path).
		b.WriteString("bd manages this server, but this command does not start a Dolt server.\n")
		b.WriteString("Run a command that can, or start one manually:\n  bd dolt start")
	}
	return fmt.Errorf("%s", b.String())
}
