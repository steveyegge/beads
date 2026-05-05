//go:build !cgo

// Package doltdriver wires the Dolt backend into the storage registry.
//
// Importing this package for side-effects (blank import) registers the "dolt"
// backend so storage.Open(ctx, storage.BackendDolt, cfg) dispatches here.
//
// Non-CGO build (this file): only external dolt sql-server is reachable;
// embedded mode returns NoCGOEmbeddedErrMsg. The CGO build in doltdriver.go
// supports both modes.
package doltdriver

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

func init() {
	storage.RegisterDriver(storage.BackendDolt, openDoltStore)
}

// openDoltStore is the registered Factory in non-CGO builds. Server-mode
// configurations succeed; anything else returns NoCGOEmbeddedErrMsg with the
// install/server-mode guidance.
func openDoltStore(ctx context.Context, cfg storage.ConnectionConfig) (storage.Storage, error) {
	fileCfg, _ := configfile.Load(cfg.BeadsDir)
	if fileCfg != nil && fileCfg.IsDoltServerMode() {
		if cfg.ReadOnly {
			return dolt.NewFromConfigWithOptions(ctx, cfg.BeadsDir, &dolt.Config{ReadOnly: true})
		}
		return dolt.NewFromConfig(ctx, cfg.BeadsDir)
	}
	return nil, fmt.Errorf("%s", NoCGOEmbeddedErrMsg)
}

// SanitizeDBName replaces characters that are awkward for embedded Dolt
// database names with underscores. Defined in both build variants so cmd/bd
// can use it without a build-tag fork at the call site.
func SanitizeDBName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

// NoCGOEmbeddedErrMsg guides the user either to server mode (no rebuild
// needed) or to an embedded-capable install path. Exported so cmd/bd's
// non-CGO factory helpers can return the same message.
const NoCGOEmbeddedErrMsg = `embedded Dolt requires a CGO build, but this bd binary was built with CGO_ENABLED=0.

Two options:

  1. Use server mode (no reinstall needed):
       bd init --server
     Requires a running 'dolt sql-server'. See docs/DOLT.md.

  2. Reinstall with embedded-mode support:
       brew install beads                              # macOS / Linux
       npm install -g @beads/bd                        # any platform with Node
       curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash

See docs/INSTALLING.md for the full comparison.`
