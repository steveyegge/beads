//go:build !unix

package main

import (
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
)

// tryDaemonClient is a no-op on non-unix platforms where the bdd daemon
// is not supported (no Unix socket available).
func tryDaemonClient(_ string, _ *configfile.Config) (storage.Storage, error) {
	return nil, nil
}
