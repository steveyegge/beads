// Package factory provides functions for creating storage backends based on configuration.
package factory

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
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

// New creates a storage backend based on the backend type.
// For Dolt, path should be the directory containing the Dolt database.
func New(ctx context.Context, backend, path string) (storage.Storage, error) {
	return NewWithOptions(ctx, backend, path, Options{})
}

// NewWithOptions creates a storage backend with the specified options.
func NewWithOptions(ctx context.Context, backend, path string, opts Options) (storage.Storage, error) {
	switch backend {
	case configfile.BackendDolt, "":
		// Dolt is the default backend - check if it's registered (requires CGO)
		lookupKey := backend
		if lookupKey == "" {
			lookupKey = configfile.BackendDolt
		}
		if factory, ok := backendRegistry[lookupKey]; ok {
			return factory(ctx, path, opts)
		}
		// Dolt not available (no CGO) - provide helpful error
		return nil, fmt.Errorf("dolt backend requires CGO (not available on this build); install from pre-built binaries")
	default:
		// Check if backend is registered
		if factory, ok := backendRegistry[backend]; ok {
			return factory(ctx, path, opts)
		}
		return nil, fmt.Errorf("unknown storage backend: %s (supported: dolt)", backend)
	}
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

	// Use GetBackendFromConfig for robust backend detection.
	// This handles cases where metadata.json has an incorrect backend value
	// by falling back to filesystem detection (gt-q5jzx5, dolt_doctor fix).
	backend := GetBackendFromConfig(beadsDir)
	// Sync cfg.Backend with detected backend so cfg.DatabasePath() computes the
	// correct path. Without this, DefaultConfig() leaves Backend="" which causes
	// GetBackend() to default to "dolt", producing wrong paths for SQLite (gt-seal2b).
	cfg.Backend = backend
	switch backend {
	case configfile.BackendDolt:
		// Merge Dolt server mode config into options (config provides defaults, opts can override)
		// Check server mode: IsDoltServerMode() uses cfg.GetBackend(), but we may have detected
		// Dolt via filesystem when cfg.Backend is wrong. Check server settings directly too.
		isServerMode := cfg.IsDoltServerMode()
		if !isServerMode && (cfg.DoltServerEnabled || cfg.DoltMode == "server" || os.Getenv("BEADS_DOLT_SERVER_MODE") == "1") {
			// Config has server settings but IsDoltServerMode() returned false due to
			// backend mismatch - trust the server settings (dolt_doctor fix)
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
//
// Safety net (gt-q5jzx5): If neither config specifies a backend, detect from
// filesystem - a directory indicates Dolt, a file indicates SQLite.
func GetBackendFromConfig(beadsDir string) string {
	// Server mode env var takes priority: BEADS_DOLT_SERVER_MODE=1 implies Dolt
	// backend. This enables K8s deployments configured entirely via env vars,
	// where no metadata.json or config.yaml may exist.
	if os.Getenv("BEADS_DOLT_SERVER_MODE") == "1" {
		return configfile.BackendDolt
	}

	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		// No metadata.json - fall back to config.yaml
		return getBackendFromYamlConfig()
	}

	// Determine backend: use explicit config if set, else fall back to config.yaml
	backend := cfg.Backend
	if backend == "" {
		// metadata.json exists but Backend is empty - check config.yaml
		// This enables town-level inheritance via viper's directory walking
		backend = getBackendFromYamlConfig()
	}

	return backend
}

// detectBackendFromPath examines the filesystem to detect if a database path
// is a Dolt directory. Returns empty string if undetermined.
// This provides a safety net when config is ambiguous (gt-q5jzx5).
func detectBackendFromPath(dbPath string) string {
	info, err := os.Stat(dbPath)
	if err != nil {
		return "" // Path doesn't exist yet or error - can't determine
	}

	if info.IsDir() {
		// Directories are Dolt databases
		return configfile.BackendDolt
	}

	return ""
}

// getBackendFromYamlConfig returns the storage-backend from config.yaml.
// The config package uses viper which walks up parent directories to find
// .beads/config.yaml, enabling town-level config inheritance.
func getBackendFromYamlConfig() string {
	backend := config.GetString("storage-backend")
	if backend == "" {
		return configfile.BackendDolt
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
