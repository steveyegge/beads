package doltserver

import (
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
)

// ServerMode describes who owns and manages the dolt sql-server lifecycle.
type ServerMode int

const (
	// ServerModeOwned means beads auto-starts and manages the server.
	// This is the default for standalone users with no explicit port config.
	ServerModeOwned ServerMode = iota

	// ServerModeExternal means the user manages the server lifecycle
	// (e.g., systemd, Docker, Hosted Dolt, VPS). Beads never starts or
	// stops the server. Determined when metadata.json points at an external
	// SQL server, a runtime port file exists, or shared-server mode is set.
	ServerModeExternal

	// ServerModeEmbedded is the legacy in-process embedded dolt path.
	// Determined when metadata.json dolt_mode is "embedded".
	ServerModeEmbedded
)

// String returns a human-readable label for the server mode.
func (m ServerMode) String() string {
	switch m {
	case ServerModeOwned:
		return "owned"
	case ServerModeExternal:
		return "external"
	case ServerModeEmbedded:
		return "embedded"
	default:
		return fmt.Sprintf("ServerMode(%d)", int(m))
	}
}

// ResolveServerMode determines the server mode from the given beadsDir.
// This is the single source of truth for how the server lifecycle is managed.
// Decision logic (checked in order):
//  1. BEADS_DOLT_SERVER_MODE=1 env var             -> ServerModeExternal
//  2. BEADS_DOLT_SHARED_SERVER env var is set       -> ServerModeExternal
//  3. metadata.json dolt_mode == "embedded"         -> ServerModeEmbedded
//  4. metadata.json dolt_mode == "server" with explicit server connection
//  5. .beads/dolt-server.port exists                -> ServerModeExternal
//  6. metadata.json has explicit dolt_server_port   -> ServerModeExternal
//  7. default                                       -> ServerModeOwned
//
// Runtime env vars (1, 2) take precedence over persisted metadata.json
// to prevent stale dolt_mode=embedded from silently overriding an active
// shared-server or server-mode configuration (GH#2949).
//
// The function loads metadata.json only if the file exists, to avoid
// triggering the legacy config.json -> metadata.json migration side effect.
func ResolveServerMode(beadsDir string) ServerMode {
	// 1. BEADS_DOLT_SERVER_MODE=1 env var -> external (explicit server mode)
	if os.Getenv("BEADS_DOLT_SERVER_MODE") == "1" {
		return ServerModeExternal
	}

	// 2. Shared server mode (env var or config.yaml) -> external.
	// Must be checked before metadata.json so that a stale
	// dolt_mode=embedded cannot override active shared-server intent.
	if IsSharedServerMode() {
		return ServerModeExternal
	}

	var fileCfg *configfile.Config

	// Only load config if metadata.json exists (avoids legacy migration side effect)
	metadataPath := configfile.ConfigPath(beadsDir)
	if _, err := os.Stat(metadataPath); err == nil {
		if cfg, loadErr := configfile.Load(beadsDir); loadErr == nil && cfg != nil {
			fileCfg = cfg
		}
	}

	// 3. Explicit embedded mode in metadata.json
	if fileCfg != nil && strings.ToLower(fileCfg.DoltMode) == configfile.DoltModeEmbedded &&
		fileCfg.DoltMode != "" { // empty defaults to embedded in GetDoltMode, but we treat empty as "unset"
		return ServerModeEmbedded
	}

	// 4. Explicit external server connection in metadata.json -> external.
	// A non-local host, socket, or deprecated metadata port means the server
	// lifecycle is owned outside this project. Without this guard, bd may
	// auto-start a shadow local server and run JSONL recovery against
	// .beads/dolt instead of the configured SQL server.
	if fileCfg != nil && strings.ToLower(fileCfg.DoltMode) == configfile.DoltModeServer {
		if isExplicitExternalHost(fileCfg.DoltServerHost) ||
			fileCfg.DoltServerSocket != "" || fileCfg.DoltServerPort > 0 {
			return ServerModeExternal
		}
	}

	// 5. Runtime port file -> external.
	// The port file is gitignored local state written/maintained by the
	// connection setup path. Treat it as a lifecycle boundary so CLI helpers
	// do not spawn a different server when metadata omits the deprecated
	// dolt_server_port field.
	if readPortFile(beadsDir) > 0 {
		return ServerModeExternal
	}

	// 6. Explicit server port in metadata.json -> external.
	// Kept as a standalone fallback for older metadata that may not set
	// dolt_mode even though it has server connection fields.
	if fileCfg != nil && fileCfg.DoltServerPort > 0 {
		return ServerModeExternal
	}

	// 7. Default: beads owns the server
	return ServerModeOwned
}

func isExplicitExternalHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	switch host {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return false
	default:
		return true
	}
}
