//go:build unix

package main

import (
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	bdRPC "github.com/steveyegge/beads/internal/storage/rpc"
)

// tryDaemonClient attempts to connect to the bdd daemon at beadsDir.
// Returns a daemon-backed storage.Storage and nil error on success.
// Returns nil, nil when the daemon is not in use (mode=off, noDaemon flag, etc.).
// Returns nil, non-nil error only when daemon_mode=always and the daemon is unreachable.
func tryDaemonClient(beadsDir string, cfg *configfile.Config) (storage.Storage, error) {
	if noDaemon || os.Getenv("BEADS_DAEMON_MODE") == "off" {
		return nil, nil
	}
	if cfg == nil {
		return nil, nil
	}
	mode := cfg.GetDaemonMode()
	if mode == configfile.DaemonModeOff {
		return nil, nil
	}

	sockPath, err := GetCreateDaemonEndpoint(beadsDir, cfg)
	if err != nil {
		if mode == configfile.DaemonModeAlways {
			return nil, fmt.Errorf("daemon_mode=always but bdd is not running at %s/bdd.sock\n"+
				"  Start it: bd daemon start\n"+
				"  Or change mode: bd config set daemon_mode auto\n"+
				"  Error: %w", beadsDir, err)
		}
		return nil, nil // daemon_mode=auto: fall through to in-process
	}

	store, err := bdRPC.Dial(sockPath)
	if err != nil {
		if mode == configfile.DaemonModeAlways {
			return nil, fmt.Errorf("daemon_mode=always but cannot dial bdd socket %s: %w", sockPath, err)
		}
		return nil, nil // daemon_mode=auto: fall through to in-process
	}
	return store, nil
}
