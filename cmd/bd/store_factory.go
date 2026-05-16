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
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	pgstore "github.com/steveyegge/beads/internal/storage/postgres"
	pgdsn "github.com/steveyegge/beads/internal/storage/postgres/dsn"
)

func usesSQLServer() bool {
	if shouldUseGlobals() {
		if serverMode || proxiedServerMode {
			return true
		}
	} else if cmdCtx != nil && (cmdCtx.ServerMode || cmdCtx.ProxiedServerMode) {
		return true
	}
	if doltserver.IsSharedServerMode() {
		return true
	}
	return false // default: embedded
}

func usesProxiedServer() bool {
	if shouldUseGlobals() {
		return proxiedServerMode
	}
	return cmdCtx != nil && cmdCtx.ProxiedServerMode
}

// newDoltStore creates a storage backend from an explicit config.
// Used by bd init and PersistentPreRun.
func newDoltStore(ctx context.Context, cfg *dolt.Config) (storage.DoltStorage, error) {
	if cfg.ProxiedServer {
		// TODO: this should not be a store
		// it should be a uow provider
		return nil, fmt.Errorf("proxy server store should be uow provider")
	}
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

// openConfiguredStore opens storage for beadsDir, probing the bdd daemon
// first when daemon_mode is auto or always. Returns a storage.Storage that may
// be a daemon client (satisfies Storage + StoreLocator only) or a local
// DoltStorage (satisfies all Storage + DoltStorage sub-interfaces). Callers
// that need DoltStorage capabilities should type-assert; the assertion fails
// in daemon mode, signaling that --no-daemon or direct-mode is required.
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
// and opens a Postgres store. The override field list is forwarded to the
// Store so that downstream surfaces (bd context, bd backend status) can
// report which fields were overridden.
//
// On connection failure the error includes the redacted target address and
// the list of applied overrides (NFR-4):
//
//	postgres unreachable: staging.db:5433/mybd (overrides applied: host) — err=...
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

// newDoltStoreFromConfig creates a storage backend from the beads directory's
// persisted metadata.json configuration. Uses embedded Dolt by default;
// connects to dolt sql-server when dolt_mode is "server".
//
// For embedded mode, legacy hyphenated database names (pre-GH#2142) are
// auto-sanitized to underscores and the fix is persisted to metadata.json.
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
	database := configfile.DefaultDoltDatabase
	if cfg != nil {
		database = cfg.GetDoltDatabase()
	}
	if sanitized := sanitizeDBName(database); sanitized != database {
		if err := migrateHyphenatedDB(beadsDir, cfg, database, sanitized); err != nil {
			return nil, fmt.Errorf("auto-sanitize database name %q → %q: %w", database, sanitized, err)
		}
		database = sanitized
	}
	return embeddeddolt.Open(ctx, beadsDir, database, "main")
}

// migrateHyphenatedDB renames a legacy hyphenated database directory and
// persists the sanitized name to metadata.json so subsequent opens use it.
// This handles projects initialized before GH#2142 that upgrade to
// embedded-mode-default builds (GH#3231).
func migrateHyphenatedDB(beadsDir string, cfg *configfile.Config, oldName, newName string) error {
	dataDir := filepath.Join(beadsDir, "embeddeddolt")
	oldDir := filepath.Join(dataDir, oldName)
	newDir := filepath.Join(dataDir, newName)

	oldExists := false
	if info, err := os.Stat(oldDir); err == nil && info.IsDir() {
		oldExists = true
	}

	if oldExists {
		_, newErr := os.Stat(newDir)
		switch {
		case newErr == nil:
			return fmt.Errorf("cannot auto-migrate database: both %q and %q exist under %s; remove one manually and retry",
				oldName, newName, dataDir)
		case !os.IsNotExist(newErr):
			return fmt.Errorf("checking target directory %q: %w", newDir, newErr)
		default:
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("renaming database directory: %w", err)
			}
			fmt.Fprintf(os.Stderr, "bd: migrated database directory %q → %q (GH#3231)\n", oldName, newName)
		}
	}

	if cfg != nil && cfg.DoltDatabase != newName {
		cfg.DoltDatabase = newName
		if err := cfg.Save(beadsDir); err != nil {
			return fmt.Errorf("persisting sanitized database name to metadata.json: %w", err)
		}
		fmt.Fprintf(os.Stderr, "bd: updated metadata.json dolt_database %q → %q (GH#3231)\n", oldName, newName)
	}
	return nil
}

// newReadOnlyStoreFromConfig creates a read-only storage backend from the beads
// directory's persisted metadata.json configuration.
//
// For embedded mode, invalid characters (hyphens, dots) are sanitized in-memory
// only — no directory renames or metadata.json writes. This prevents cross-repo
// hydration from mutating foreign projects (GH#3231).
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
	database := configfile.DefaultDoltDatabase
	if cfg != nil {
		database = cfg.GetDoltDatabase()
	}
	if sanitized := sanitizeDBName(database); sanitized != database {
		database = sanitized
	}
	return embeddeddolt.Open(ctx, beadsDir, database, "main")
}
