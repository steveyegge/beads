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
	Layout      string `json:"layout,omitempty"`  // "" or "v1" = legacy flat, "v2" = var/ layout

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

// DatabasePath returns the path for the database file, using read-both pattern.
// For READS: checks var/ first, falls back to root (handles migration edge cases).
// For NEW files: uses layout preference (var/ if Layout is "v2", else root).
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

	// SQLite (default) - uses var/ layout for volatile files
	db := strings.TrimSpace(c.Database)
	if db == "" {
		db = "beads.db"
	}
	if filepath.IsAbs(db) {
		return db
	}

	// Environment override for emergency fallback
	if os.Getenv("BD_LEGACY_LAYOUT") == "1" {
		return filepath.Join(beadsDir, db)
	}

	varPath := filepath.Join(beadsDir, "var", db)
	rootPath := filepath.Join(beadsDir, db)

	// Read-both: check var/ first, then root (handles migration edge cases)
	if _, err := os.Stat(varPath); err == nil {
		return varPath
	}
	if _, err := os.Stat(rootPath); err == nil {
		return rootPath
	}

	// New file: use layout preference
	if c.useVarLayout(beadsDir) {
		return varPath
	}
	return rootPath
}

// useVarLayout checks if var/ layout is active.
// Primary check uses Layout field, fallback checks var/ directory.
func (c *Config) useVarLayout(beadsDir string) bool {
	if os.Getenv("BD_LEGACY_LAYOUT") == "1" {
		return false
	}

	// Primary: check layout field
	if c.Layout == LayoutV2 {
		return true
	}
	if c.Layout == LayoutV1 || c.Layout != "" {
		return false
	}

	// Fallback: check var/ directory (bootstrap/migration scenarios)
	varDir := filepath.Join(beadsDir, "var")
	info, err := os.Stat(varDir)
	return err == nil && info.IsDir()
}

// Layout version constants
const (
	LayoutV1 = "v1" // Legacy flat layout (or empty string)
	LayoutV2 = "v2" // var/ layout
)

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
func CapabilitiesForBackend(backend string) BackendCapabilities {
	switch strings.TrimSpace(strings.ToLower(backend)) {
	case "", BackendSQLite:
		return BackendCapabilities{SingleProcessOnly: false}
	case BackendDolt:
		return BackendCapabilities{SingleProcessOnly: true}
	default:
		return BackendCapabilities{SingleProcessOnly: true}
	}
}

// GetBackend returns the configured backend type, defaulting to SQLite.
func (c *Config) GetBackend() string {
	if c.Backend == "" {
		return BackendSQLite
	}
	return c.Backend
}
