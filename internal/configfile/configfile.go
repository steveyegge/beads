package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ConfigFileName = "metadata.json"

type Config struct {
	Database    string `json:"database"`
	JSONLExport string `json:"jsonl_export,omitempty"`
	Backend     string `json:"backend,omitempty"` // "sqlite" (default) or "dolt"

	// Dolt remote URL for bootstrap from remote (enables JSONL-free fresh clones)
	// When set and Dolt backend is configured, fresh clones will bootstrap by
	// cloning from this remote instead of requiring JSONL in git.
	// Example: "aws://[bucket:table]/database"
	DoltRemoteURL string `json:"dolt_remote_url,omitempty"`

	// Dolt SQL server mode configuration
	// When enabled (DoltServerEnabled=true or DoltMode="server"), connects to
	// a dolt sql-server via TCP instead of embedded driver.
	// This enables multi-writer support and eliminates lock contention.
	DoltServerEnabled  bool   `json:"dolt_server_enabled,omitempty"` // Legacy: prefer DoltMode
	DoltMode           string `json:"dolt_mode,omitempty"`           // "embedded" (default) or "server"
	DoltServerHost     string `json:"dolt_server_host,omitempty"`    // Default: 127.0.0.1
	DoltServerPort     int    `json:"dolt_server_port,omitempty"`    // Default: 3307
	DoltServerUser     string `json:"dolt_server_user,omitempty"`    // Default: root
	DoltServerPassword string `json:"dolt_server_password,omitempty"` // Or use BEADS_DOLT_PASSWORD env

	// Deletions configuration
	DeletionsRetentionDays int `json:"deletions_retention_days,omitempty"` // 0 means use default (3 days)

	// Deprecated: LastBdVersion is no longer used for version tracking.
	// Version is now stored in .local_version (gitignored) to prevent
	// upgrade notifications firing after git operations reset metadata.json.
	// bd-tok: This field is kept for backwards compatibility when reading old configs.
	LastBdVersion string `json:"last_bd_version,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		Database:    "beads.db",
		JSONLExport: "issues.jsonl", // Canonical name (bd-6xd)
	}
}

func ConfigPath(beadsDir string) string {
	return filepath.Join(beadsDir, ConfigFileName)
}

func Load(beadsDir string) (*Config, error) {
	configPath := ConfigPath(beadsDir)

	data, err := os.ReadFile(configPath) // #nosec G304 - controlled path from config
	if os.IsNotExist(err) {
		// Try legacy config.json location (migration path)
		legacyPath := filepath.Join(beadsDir, "config.json")
		data, err = os.ReadFile(legacyPath) // #nosec G304 - controlled path from config
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading legacy config: %w", err)
		}

		// Migrate: parse legacy config, save as metadata.json, remove old file
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing legacy config: %w", err)
		}

		// Save to new location
		if err := cfg.Save(beadsDir); err != nil {
			return nil, fmt.Errorf("migrating config to metadata.json: %w", err)
		}

		// Remove legacy file (best effort)
		_ = os.Remove(legacyPath)

		return &cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Save(beadsDir string) error {
	configPath := ConfigPath(beadsDir)

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

func (c *Config) DatabasePath(beadsDir string) string {
	backend := c.GetBackend()

	// Treat Database as the on-disk storage location:
	// - SQLite: filename (default: beads.db)
	// - Dolt: directory name (default: dolt)
	//
	// Backward-compat: early dolt configs wrote "beads.db" even when Backend=dolt.
	// In that case, treat it as "dolt".
	if backend == BackendDolt {
		db := strings.TrimSpace(c.Database)
		if db == "" || db == "beads.db" {
			db = "dolt"
		}
		if filepath.IsAbs(db) {
			return db
		}
		return filepath.Join(beadsDir, db)
	}

	// SQLite (default)
	db := strings.TrimSpace(c.Database)
	if db == "" {
		db = "beads.db"
	}
	if filepath.IsAbs(db) {
		return db
	}
	return filepath.Join(beadsDir, db)
}

func (c *Config) JSONLPath(beadsDir string) string {
	if c.JSONLExport == "" {
		return filepath.Join(beadsDir, "issues.jsonl")
	}
	return filepath.Join(beadsDir, c.JSONLExport)
}

// DefaultDeletionsRetentionDays is the default retention period for deletion records.
const DefaultDeletionsRetentionDays = 3

// GetDeletionsRetentionDays returns the configured retention days, or the default if not set.
func (c *Config) GetDeletionsRetentionDays() int {
	if c.DeletionsRetentionDays <= 0 {
		return DefaultDeletionsRetentionDays
	}
	return c.DeletionsRetentionDays
}

// Backend constants
const (
	BackendSQLite = "sqlite"
	BackendDolt   = "dolt"
)

// BackendCapabilities describes behavioral constraints for a storage backend.
//
// This is intentionally small and stable: callers should use these flags to decide
// whether to enable features like daemon/RPC/autostart and process spawning.
//
// NOTE: The embedded Dolt driver is effectively single-writer at the OS-process level.
// Even if multiple goroutines are safe within one process, multiple processes opening
// the same Dolt directory concurrently can cause lock contention and transient
// "read-only" failures. Therefore, Dolt is treated as single-process-only.
type BackendCapabilities struct {
	// SingleProcessOnly indicates the backend must not be accessed from multiple
	// Beads OS processes concurrently (no daemon mode, no RPC client/server split,
	// no helper-process spawning).
	SingleProcessOnly bool
}

// CapabilitiesForBackend returns capabilities for a backend string.
// Unknown backends are treated conservatively as single-process-only.
//
// Note: For Dolt, this returns SingleProcessOnly=true for embedded mode.
// Use Config.GetCapabilities() when you have the full config to properly
// handle server mode (which supports multi-process access).
func CapabilitiesForBackend(backend string) BackendCapabilities {
	switch strings.TrimSpace(strings.ToLower(backend)) {
	case "", BackendSQLite:
		return BackendCapabilities{SingleProcessOnly: false}
	case BackendDolt:
		// Embedded Dolt is single-process-only.
		// Server mode is handled by Config.GetCapabilities().
		return BackendCapabilities{SingleProcessOnly: true}
	default:
		return BackendCapabilities{SingleProcessOnly: true}
	}
}

// GetCapabilities returns the backend capabilities for this config.
// Unlike CapabilitiesForBackend(string), this considers Dolt server mode
// which supports multi-process access.
func (c *Config) GetCapabilities() BackendCapabilities {
	backend := c.GetBackend()
	if backend == BackendDolt && c.IsDoltServerMode() {
		// Server mode supports multi-writer, so NOT single-process-only
		return BackendCapabilities{SingleProcessOnly: false}
	}
	return CapabilitiesForBackend(backend)
}

// GetBackend returns the configured backend type, defaulting to SQLite.
func (c *Config) GetBackend() string {
	if c.Backend == "" {
		return BackendSQLite
	}
	return c.Backend
}

// Dolt mode constants
const (
	DoltModeEmbedded = "embedded"
	DoltModeServer   = "server"
)

// Default Dolt server settings
const (
	DefaultDoltServerHost = "127.0.0.1"
	DefaultDoltServerPort = 3307 // Use 3307 to avoid conflict with MySQL on 3306
	DefaultDoltServerUser = "root"
)

// IsDoltServerMode returns true if Dolt SQL server mode is enabled.
// Server mode connects via TCP instead of embedded driver, enabling multi-writer support.
// Checks both DoltServerEnabled (legacy) and DoltMode (preferred).
func (c *Config) IsDoltServerMode() bool {
	if c.GetBackend() != BackendDolt {
		return false
	}
	// Check both mechanisms for backwards compatibility
	return c.DoltServerEnabled || strings.ToLower(c.DoltMode) == DoltModeServer
}

// GetDoltMode returns the Dolt connection mode, defaulting to embedded.
func (c *Config) GetDoltMode() string {
	if c.DoltMode == "" {
		return DoltModeEmbedded
	}
	return c.DoltMode
}

// GetDoltServerHost returns the Dolt server host, defaulting to 127.0.0.1.
func (c *Config) GetDoltServerHost() string {
	if c.DoltServerHost == "" {
		return DefaultDoltServerHost
	}
	return c.DoltServerHost
}

// GetDoltServerPort returns the Dolt server port, defaulting to 3307.
func (c *Config) GetDoltServerPort() int {
	if c.DoltServerPort <= 0 {
		return DefaultDoltServerPort
	}
	return c.DoltServerPort
}

// GetDoltServerUser returns the Dolt server user, defaulting to root.
func (c *Config) GetDoltServerUser() string {
	if c.DoltServerUser == "" {
		return DefaultDoltServerUser
	}
	return c.DoltServerUser
}

// GetDoltDatabase returns the database name for Dolt server mode.
// This is different from DatabasePath which returns the on-disk path.
// For server mode, Database field contains the database name on the server
// (e.g., "hq", "gastown", "beads"). Defaults to "beads".
func (c *Config) GetDoltDatabase() string {
	db := strings.TrimSpace(c.Database)
	if db == "" || db == "beads.db" || db == "dolt" {
		return "beads"
	}
	// Strip any path components - just want the database name
	return filepath.Base(db)
}

// CapabilitiesForConfig returns capabilities based on full configuration.
// This is preferred over CapabilitiesForBackend when you have the full config,
// as it can account for server mode (which enables multi-process for Dolt).
func CapabilitiesForConfig(cfg *Config) BackendCapabilities {
	if cfg == nil {
		return BackendCapabilities{SingleProcessOnly: false}
	}
	// Dolt in server mode is NOT single-process-only (server handles concurrency)
	if cfg.IsDoltServerMode() {
		return BackendCapabilities{SingleProcessOnly: false}
	}
	return CapabilitiesForBackend(cfg.GetBackend())
}
