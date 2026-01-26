// Package factory provides functions for creating storage backends based on configuration.
package factory

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
)

// BackendFactory is a function that creates a storage backend
type BackendFactory func(ctx context.Context, path string, opts Options) (storage.Storage, error)

// backendRegistry holds registered backend factories
var backendRegistry = make(map[string]BackendFactory)

// RegisterBackend registers a storage backend factory
func RegisterBackend(name string, factory BackendFactory) {
	backendRegistry[name] = factory
}

// Options configures how the storage backend is opened
type Options struct {
	ReadOnly    bool
	LockTimeout time.Duration
	IdleTimeout time.Duration // Connection idle timeout (for connection pooling)

	// Dolt server mode options (federation)
	ServerMode     bool   // Connect to dolt sql-server instead of embedded
	ServerHost     string // Server host (default: 127.0.0.1)
	ServerPort     int    // Server port (default: 3307)
	ServerUser     string // MySQL user (default: root)
	ServerPassword string // Server password (or use BEADS_DOLT_PASSWORD env)
	Database       string // Database name for Dolt server mode (default: beads)
}

// New creates a storage backend based on the backend type.
// For SQLite, path should be the full path to the .db file.
// For Dolt, path should be the directory containing the Dolt database.
func New(ctx context.Context, backend, path string) (storage.Storage, error) {
	return NewWithOptions(ctx, backend, path, Options{})
}

// NewWithOptions creates a storage backend with the specified options.
func NewWithOptions(ctx context.Context, backend, path string, opts Options) (storage.Storage, error) {
	switch backend {
	case configfile.BackendSQLite, "":
		if opts.ReadOnly {
			if opts.LockTimeout > 0 {
				return sqlite.NewReadOnlyWithTimeout(ctx, path, opts.LockTimeout)
			}
			return sqlite.NewReadOnly(ctx, path)
		}
		if opts.LockTimeout > 0 {
			return sqlite.NewWithTimeout(ctx, path, opts.LockTimeout)
		}
		return sqlite.New(ctx, path)
	default:
		// Check if backend is registered (e.g., dolt with CGO)
		if factory, ok := backendRegistry[backend]; ok {
			return factory(ctx, path, opts)
		}
		// Provide helpful error for dolt on systems without CGO
		if backend == configfile.BackendDolt {
			return nil, fmt.Errorf("dolt backend requires CGO (not available on this build); use sqlite backend or install from pre-built binaries")
		}
		return nil, fmt.Errorf("unknown storage backend: %s (supported: sqlite, dolt)", backend)
	}
}

// NewFromConfig creates a storage backend based on the metadata.json configuration.
// beadsDir is the path to the .beads directory.
func NewFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return NewFromConfigWithOptions(ctx, beadsDir, Options{})
}

// NewFromConfigWithOptions creates a storage backend with options from metadata.json.
func NewFromConfigWithOptions(ctx context.Context, beadsDir string, opts Options) (storage.Storage, error) {
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	backend := cfg.GetBackend()
	switch backend {
	case configfile.BackendSQLite:
		return NewWithOptions(ctx, backend, cfg.DatabasePath(beadsDir), opts)
	case configfile.BackendDolt:
		// Merge Dolt server mode config into options (config provides defaults, opts can override)
		if cfg.IsDoltServerMode() {
			opts.ServerMode = true
			if opts.ServerHost == "" {
				opts.ServerHost = cfg.GetDoltServerHost()
			}
			if opts.ServerPort == 0 {
				opts.ServerPort = cfg.GetDoltServerPort()
			}
			if opts.ServerUser == "" {
				opts.ServerUser = cfg.GetDoltServerUser()
			}
			if opts.Database == "" {
				opts.Database = cfg.GetDoltDatabase()
			}
			// Password from config (env var is usually preferred)
			if opts.ServerPassword == "" && cfg.DoltServerPassword != "" {
				opts.ServerPassword = cfg.DoltServerPassword
			}
		}
		return NewWithOptions(ctx, backend, cfg.DatabasePath(beadsDir), opts)
	default:
		return nil, fmt.Errorf("unknown storage backend in config: %s", backend)
	}
}

// GetBackendFromConfig returns the backend type from metadata.json, falling back
// to config.yaml's storage-backend setting if metadata.json doesn't specify one.
// This enables town-level config inheritance: when town's config.yaml has
// storage-backend: dolt, rig-level workspaces will inherit it even if their
// local metadata.json doesn't have Backend set. (hq-5813b7)
func GetBackendFromConfig(beadsDir string) string {
	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		// No metadata.json - fall back to config.yaml
		return getBackendFromYamlConfig()
	}

	// If metadata.json has an explicit backend, use it
	if cfg.Backend != "" {
		return cfg.Backend
	}

	// metadata.json exists but Backend is empty - check config.yaml
	// This enables town-level inheritance via viper's directory walking
	return getBackendFromYamlConfig()
}

// getBackendFromYamlConfig returns the storage-backend from config.yaml.
// The config package uses viper which walks up parent directories to find
// .beads/config.yaml, enabling town-level config inheritance.
func getBackendFromYamlConfig() string {
	backend := config.GetString("storage-backend")
	if backend == "" {
		return configfile.BackendSQLite
	}
	return backend
}

// LoadConfig loads and returns the config from the specified beads directory.
// Returns nil if config doesn't exist or can't be loaded.
func LoadConfig(beadsDir string) *configfile.Config {
	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return nil
	}
	return cfg
}

// GetCapabilitiesFromConfig returns backend capabilities based on full config.
// This accounts for server mode (Dolt with server is NOT single-process-only).
func GetCapabilitiesFromConfig(beadsDir string) configfile.BackendCapabilities {
	cfg := LoadConfig(beadsDir)
	return configfile.CapabilitiesForConfig(cfg)
}
