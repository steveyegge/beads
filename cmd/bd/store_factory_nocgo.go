//go:build !cgo

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
	"github.com/steveyegge/beads/internal/storage/dolt"
	pgstore "github.com/steveyegge/beads/internal/storage/postgres"
	pgdsn "github.com/steveyegge/beads/internal/storage/postgres/dsn"
)

func usesSQLServer() bool {
	return true
}

func usesProxiedServer() bool {
	if shouldUseGlobals() {
		return proxiedServerMode
	}
	return cmdCtx != nil && cmdCtx.ProxiedServerMode
}

func newDoltStore(ctx context.Context, cfg *dolt.Config) (storage.DoltStorage, error) {
	if cfg.ProxiedServer {
		// TODO: this should not be a store
		// it should be a uow provider
		return nil, fmt.Errorf("proxy server store should be uow provider")
	}
	if !cfg.ServerMode {
		return nil, fmt.Errorf("%s", nocgoEmbeddedErrMsg)
	}
	return dolt.New(ctx, cfg)
}

// acquireEmbeddedLock returns a no-op lock in non-CGO builds.
func acquireEmbeddedLock(_ string, _ bool) (util.Unlocker, error) {
	return util.NoopLock{}, nil
}

// openConfiguredStore opens storage for beadsDir, probing the bdd daemon first
// when daemon_mode is auto or always. In the nocgo build, embedded Dolt is not
// available; daemon mode is the only no-sql-server path.
func openConfiguredStore(ctx context.Context, beadsDir string, _ bool) (storage.Storage, error) {
	cfg, _ := configfile.Load(beadsDir)
	if daemonStore, err := tryDaemonClient(beadsDir, cfg); daemonStore != nil || err != nil {
		return daemonStore, err
	}
	if cfg != nil && cfg.IsPostgresBackend() {
		return newPostgresStore(ctx, cfg)
	}
	return newDoltStoreFromConfig(ctx, beadsDir)
}

// newPostgresStore applies BEADS_POSTGRES_* env overrides to the stored
// stripped DSN, composes the full connection string (adding password last),
// and opens a Postgres store.
//
// NOTE: be-0w5z7u (BackendInfo resolver) should also call
// dsn.ApplyEnvOverrides so that `bd context` / `bd backend status` reflect
// the runtime target rather than the persisted DSN.
func newPostgresStore(ctx context.Context, cfg *configfile.Config) (*pgstore.Store, error) {
	overriddenDSN, overrideFields := pgdsn.ApplyEnvOverrides(cfg.PostgresDSN)
	password := os.Getenv("BEADS_POSTGRES_PASSWORD")
	fullDSN := pgdsn.Compose(overriddenDSN, password)
	return pgstore.Open(ctx, fullDSN, overriddenDSN, overrideFields)
}

// newDoltStoreFromConfig creates a SQL-server-backed storage backend from config.
func newDoltStoreFromConfig(ctx context.Context, beadsDir string) (storage.DoltStorage, error) {
	cfg, err := configfile.Load(beadsDir)
	if err == nil && cfg != nil && cfg.IsDoltProxiedServerMode() {
		// TODO: this needs to be uow provider
		return nil, fmt.Errorf("proxy server store should be uow provider")
		// 	return newProxiedServerStore(ctx, &dolt.Config{
		// 		BeadsDir:      beadsDir,
		// 		Database:      cfg.GetDoltDatabase(),
		// 		ProxiedServer: true,
		// 	})
	}
	if err == nil && cfg != nil && cfg.IsDoltServerMode() {
		return dolt.NewFromConfig(ctx, beadsDir)
	}
	return nil, fmt.Errorf("%s", nocgoEmbeddedErrMsg)
}

// newReadOnlyStoreFromConfig creates a read-only SQL-server-backed storage backend.
func newReadOnlyStoreFromConfig(ctx context.Context, beadsDir string) (storage.DoltStorage, error) {
	cfg, err := configfile.Load(beadsDir)
	if err == nil && cfg != nil && cfg.IsDoltProxiedServerMode() {
		// TODO: this needs to be uow provider
		return nil, fmt.Errorf("proxy server store needs to be uow provider")
		// return newProxiedServerStore(ctx, &dolt.Config{
		// 	BeadsDir:      beadsDir,
		// 	Database:      cfg.GetDoltDatabase(),
		// 	ProxiedServer: true,
		// 	ReadOnly:      true,
		// })
	}
	if err == nil && cfg != nil && cfg.IsDoltServerMode() {
		return dolt.NewFromConfigWithOptions(ctx, beadsDir, &dolt.Config{ReadOnly: true})
	}
	return nil, fmt.Errorf("%s", nocgoEmbeddedErrMsg)
}

const nocgoEmbeddedErrMsg = `embedded Dolt requires a CGO build, but this bd binary was built with CGO_ENABLED=0.

Three options:

  1. Use the proxied dolt sql-server (no external server, no reinstall):
       bd init --proxied-server
     bd spawns a per-workspace proxy + child dolt sql-server under
     .beads/proxieddb/ and manages their lifecycle for you.

  2. Use external server mode (no reinstall needed):
       bd init --server
     Requires a running 'dolt sql-server'. See docs/DOLT.md.

  3. Reinstall with embedded-mode support:
       brew install beads                              # macOS / Linux
       npm install -g @beads/bd                        # any platform with Node
       curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash

See docs/INSTALLING.md for the full comparison.`
