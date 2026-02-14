// Package factory provides functions for creating storage backends based on configuration.
package factory

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/storage"
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

	// Remote daemon guard bypass (gt-57wsnm)
	// When BD_DAEMON_HOST is set, direct database access is normally blocked.
	// Set this to true to allow direct access (e.g., for daemon process itself).
	AllowWithRemoteDaemon bool
}

// New creates a Dolt storage backend.
// Path should be the directory containing the Dolt database.
func New(ctx context.Context, backend, path string) (storage.Storage, error) {
	return NewWithOptions(ctx, backend, path, Options{})
}

// NewWithOptions creates a storage backend with the specified options.
// Dolt is the only supported backend. SQLite has been removed (bd-i0r5c).
func NewWithOptions(ctx context.Context, backend, path string, opts Options) (storage.Storage, error) {
	// Normalize: empty string and "dolt" both mean Dolt
	lookupKey := backend
	if lookupKey == "" || lookupKey == configfile.BackendDolt {
		lookupKey = configfile.BackendDolt
	}

	// Legacy SQLite references: log warning but attempt Dolt anyway.
	// Old metadata.json files may still say "sqlite" — callers should
	// run `bd migrate --to-dolt` to update their workspace.
	if backend == configfile.BackendSQLite {
		debug.Logf("warning: SQLite backend requested but no longer supported; falling back to Dolt (run 'bd migrate --to-dolt')")
		lookupKey = configfile.BackendDolt
	}

	if factory, ok := backendRegistry[lookupKey]; ok {
		return factory(ctx, path, opts)
	}

	return nil, fmt.Errorf("dolt backend requires CGO (not available on this build); install from pre-built binaries")
}

// NewFromConfig creates a storage backend based on the metadata.json configuration.
// beadsDir is the path to the .beads directory.
func NewFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return NewFromConfigWithOptions(ctx, beadsDir, Options{})
}

// NewFromConfigWithOptions creates a storage backend with options from metadata.json.
func NewFromConfigWithOptions(ctx context.Context, beadsDir string, opts Options) (storage.Storage, error) {
	// Guard: Block direct database access when using a remote daemon (gt-57wsnm)
	// BD_DAEMON_HOST indicates the client is connecting to a remote daemon,
	// so local filesystem database access would be incorrect (and may auto-start
	// a local Dolt server). Callers should use daemon RPC instead.
	if remoteHost := os.Getenv("BD_DAEMON_HOST"); remoteHost != "" && !opts.AllowWithRemoteDaemon {
		return nil, fmt.Errorf("direct database access blocked: BD_DAEMON_HOST=%s is set (use daemon RPC instead); if local access is genuinely needed, set AllowWithRemoteDaemon option", remoteHost)
	}

	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	// Backend is always Dolt. Ensure cfg.Backend is set so cfg.DatabasePath()
	// computes the correct path (directory, not file).
	cfg.Backend = configfile.BackendDolt

	// Merge Dolt server mode config into options
	isServerMode := cfg.IsDoltServerMode()
	if !isServerMode && (cfg.DoltServerEnabled || cfg.DoltMode == "server" || os.Getenv("BEADS_DOLT_SERVER_MODE") == "1") {
		isServerMode = true
	}
	if isServerMode {
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
		if opts.ServerPassword == "" && cfg.DoltServerPassword != "" {
			opts.ServerPassword = cfg.DoltServerPassword
		}
	}
	return NewWithOptions(ctx, configfile.BackendDolt, cfg.DatabasePath(beadsDir), opts)
}

// GetBackendFromConfig returns the backend type. Always returns Dolt since
// SQLite has been removed (bd-i0r5c). Kept for API compatibility — callers
// still call this to determine backend, and old metadata.json files may
// have "sqlite" which we silently upgrade to "dolt".
func GetBackendFromConfig(beadsDir string) string {
	return configfile.BackendDolt
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
