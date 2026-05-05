//go:build cgo

// Package doltdriver wires the Dolt backend into the storage registry.
//
// Importing this package for side-effects (blank import) registers the "dolt"
// backend so storage.Open(ctx, storage.BackendDolt, cfg) dispatches here.
// The package owns the metadata.json-driven embedded/server dispatch that
// previously lived in cmd/bd/store_factory.go; cmd/bd is now a thin wrapper.
//
// CGO build (this file): supports both embedded Dolt (default) and external
// dolt sql-server, picked from metadata.json. The non-CGO build in
// doltdriver_nocgo.go supports only server mode.
package doltdriver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
)

func init() {
	storage.RegisterDriver(storage.BackendDolt, openDoltStore)
}

// openDoltStore is the registered Factory for the "dolt" backend. It reads
// metadata.json under cfg.BeadsDir to decide between server-mode and embedded
// dispatch, and ignores cfg.DSN (Dolt addresses its server via metadata fields).
//
// cfg.Database, when non-empty, overrides the embedded database name read from
// metadata.json. For server mode the database name is resolved by
// dolt.NewFromConfig from metadata.json directly.
//
// cfg.ReadOnly controls two things: server-mode uses a read-only options
// variant, and embedded mode skips the on-disk hyphenated-DB migration write
// (it still sanitizes the in-memory name so opens succeed against legacy data).
func openDoltStore(ctx context.Context, cfg storage.ConnectionConfig) (storage.Storage, error) {
	fileCfg, _ := configfile.Load(cfg.BeadsDir)

	if fileCfg != nil && fileCfg.IsDoltServerMode() {
		if cfg.ReadOnly {
			return dolt.NewFromConfigWithOptions(ctx, cfg.BeadsDir, &dolt.Config{ReadOnly: true})
		}
		return dolt.NewFromConfig(ctx, cfg.BeadsDir)
	}

	database := configfile.DefaultDoltDatabase
	if fileCfg != nil {
		database = fileCfg.GetDoltDatabase()
	}
	if cfg.Database != "" {
		database = cfg.Database
	}
	if sanitized := SanitizeDBName(database); sanitized != database {
		if !cfg.ReadOnly {
			if err := migrateHyphenatedDB(cfg.BeadsDir, fileCfg, database, sanitized); err != nil {
				return nil, fmt.Errorf("auto-sanitize database name %q → %q: %w", database, sanitized, err)
			}
		}
		database = sanitized
	}
	return embeddeddolt.Open(ctx, cfg.BeadsDir, database, "main")
}

// SanitizeDBName replaces characters that are awkward for embedded Dolt
// database names with underscores. Exported so cmd/bd and tests can apply
// the same normalization without duplicating the rule.
func SanitizeDBName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

// migrateHyphenatedDB renames a legacy hyphenated database directory and
// persists the sanitized name to metadata.json so subsequent opens use it.
// Handles projects initialized before GH#2142 that upgrade to embedded-mode-default
// builds (GH#3231). Caller must already know that oldName != newName.
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
