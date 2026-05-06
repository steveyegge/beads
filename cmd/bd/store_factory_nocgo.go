//go:build !cgo

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/db/util"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/storage/doltdriver"
	"github.com/steveyegge/beads/internal/storage/postgres/dsn"
)

// isEmbeddedMode returns false in non-CGO builds since embedded Dolt
// requires CGO. Only server mode is available.
func isEmbeddedMode() bool {
	return false
}

// newDoltStore creates a server-mode storage backend. Embedded Dolt is not
// available without CGO.
func newDoltStore(ctx context.Context, cfg *dolt.Config) (storage.Storage, error) {
	if !cfg.ServerMode {
		return nil, fmt.Errorf("%s", doltdriver.NoCGOEmbeddedErrMsg)
	}
	return dolt.New(ctx, cfg)
}

// acquireEmbeddedLock returns a no-op lock in non-CGO builds.
func acquireEmbeddedLock(_ string, _ bool) (util.Unlocker, error) {
	return util.NoopLock{}, nil
}

// newStoreFromConfig opens a store for the backend recorded in metadata.json.
// Same dispatch shape as the cgo variant, but without the embedded-Dolt path.
func newStoreFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return openConfiguredStore(ctx, beadsDir, false)
}

// newReadOnlyStoreFromConfig is the read-only counterpart.
func newReadOnlyStoreFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return openConfiguredStore(ctx, beadsDir, true)
}

// newDoltStoreFromConfig is retained for callers that intentionally pin
// the Dolt backend (federation, remote-store, dolt-only subcommands).
// New code should call newStoreFromConfig instead.
func newDoltStoreFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return storage.Open(ctx, storage.BackendDolt, storage.ConnectionConfig{BeadsDir: beadsDir})
}

func openConfiguredStore(ctx context.Context, beadsDir string, readOnly bool) (storage.Storage, error) {
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return nil, err
	}

	backend := storage.BackendDolt
	var connCfg storage.ConnectionConfig
	connCfg.BeadsDir = beadsDir
	connCfg.ReadOnly = readOnly

	if cfg != nil {
		switch cfg.GetBackend() {
		case configfile.BackendPostgres:
			backend = storage.BackendPostgres
			if cfg.PostgresDSN == "" {
				return nil, fmt.Errorf("metadata.json: backend=postgres but postgres_dsn is empty (run `bd init --backend=postgres --dsn=...`)")
			}
			connCfg.DSN = dsn.Compose(cfg.PostgresDSN, os.Getenv("BEADS_POSTGRES_PASSWORD"))
		case configfile.BackendDolt:
			// Dolt's doltdriver factory reads metadata.json itself for
			// dolt_mode / dolt_server_* fields — connCfg stays minimal.
		}
	}

	return storage.Open(ctx, backend, connCfg)
}
