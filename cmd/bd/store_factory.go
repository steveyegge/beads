//go:build cgo

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/db/util"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/storage/postgres/dsn"
)

// isEmbeddedMode returns true when the current session is using the embedded
// Dolt engine (the default). Returns false in server mode (external dolt
// sql-server). Safe to call before store initialization — defaults to true
// (embedded) when the mode hasn't been set yet.
func isEmbeddedMode() bool {
	if shouldUseGlobals() {
		if serverMode {
			return false
		}
	} else if cmdCtx != nil && cmdCtx.ServerMode {
		return false
	}
	// Shared server mode is a form of server mode. This check covers
	// commands that skip DB init (dolt status, dolt start, etc.) where
	// serverMode hasn't been set from metadata.json yet (GH#2946).
	if doltserver.IsSharedServerMode() {
		return false
	}
	return true // default: embedded
}

// newDoltStore creates a storage backend from an explicit dolt.Config. This
// path is used by bootstrap and init flows that have already resolved the
// full Dolt configuration; it does not go through the registry because the
// registry's ConnectionConfig is intentionally smaller than dolt.Config.
func newDoltStore(ctx context.Context, cfg *dolt.Config) (storage.Storage, error) {
	if cfg.ServerMode {
		return dolt.New(ctx, cfg)
	}
	return embeddeddolt.Open(ctx, cfg.BeadsDir, cfg.Database, "main")
}

// acquireEmbeddedLock acquires an exclusive flock on the embeddeddolt data
// directory derived from beadsDir. The caller must defer lock.Unlock().
// Returns a no-op lock when serverMode is true (the server handles its own
// concurrency).
func acquireEmbeddedLock(beadsDir string, serverMode bool) (util.Unlocker, error) {
	if serverMode {
		return util.NoopLock{}, nil
	}
	dataDir := filepath.Join(beadsDir, "embeddeddolt")
	lock, err := util.TryLock(filepath.Join(dataDir, ".lock"))
	if err != nil {
		if lockfile.IsLocked(err) {
			return nil, fmt.Errorf("embeddeddolt: another process holds the exclusive lock on %s; "+
				"the embedded backend supports only one writer at a time — "+
				"use the dolt server backend for concurrent access", dataDir)
		}
		return nil, fmt.Errorf("embeddeddolt: acquiring lock: %w", err)
	}
	return lock, nil
}

// newStoreFromConfig opens a store for the backend recorded in metadata.json.
// Dolt and Postgres dispatch through the same registry; backend-specific
// connection details (Dolt server-mode, Postgres DSN composition) are read
// from metadata.json + env vars here so the registry's ConnectionConfig
// stays minimal.
func newStoreFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return openConfiguredStore(ctx, beadsDir, false)
}

// newReadOnlyStoreFromConfig is the read-only counterpart. Embedded mode
// sanitizes hyphenated database names in memory only — no on-disk migration.
func newReadOnlyStoreFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return openConfiguredStore(ctx, beadsDir, true)
}

// newDoltStoreFromConfig is retained for callers that intentionally pin the
// Dolt backend (bootstrap helpers, Dolt-specific subcommands). New code
// should call newStoreFromConfig instead.
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
