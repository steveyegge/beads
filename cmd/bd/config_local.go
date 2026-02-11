package main

import (
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/syncbranch"
	"gopkg.in/yaml.v3"
)

// localConfig represents the subset of config.yaml we need for auto-import, no-db, and prefer-dolt detection.
// Using proper YAML parsing handles edge cases like comments, indentation, and special characters.
type localConfig struct {
	SyncBranch string `yaml:"sync-branch"`
	NoDb       bool   `yaml:"no-db"`
	PreferDolt bool   `yaml:"prefer-dolt"`
}

// isNoDbModeConfigured checks if no-db: true is set in config.yaml.
// Uses proper YAML parsing to avoid false matches in comments or nested keys.
func isNoDbModeConfigured(beadsDir string) bool {
	configPath := filepath.Join(beadsDir, "config.yaml")
	data, err := os.ReadFile(configPath) // #nosec G304 - config file path from beadsDir
	if err != nil {
		return false
	}

	var cfg localConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		debug.Logf("Warning: failed to parse config.yaml for no-db check: %v", err)
		return false
	}

	return cfg.NoDb
}

// isPreferDoltConfigured checks if prefer-dolt: true is set in config.yaml.
// Uses proper YAML parsing to avoid false matches in comments or nested keys.
func isPreferDoltConfigured(beadsDir string) bool {
	configPath := filepath.Join(beadsDir, "config.yaml")
	data, err := os.ReadFile(configPath) // #nosec G304 - config file path from beadsDir
	if err != nil {
		return false
	}

	var cfg localConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		debug.Logf("Warning: failed to parse config.yaml for prefer-dolt check: %v", err)
		return false
	}

	return cfg.PreferDolt
}

// getLocalSyncBranch reads sync-branch from the local config.yaml file.
// This reads directly from the file rather than using cached config to handle
// cases where CWD has changed since config initialization.
func getLocalSyncBranch(beadsDir string) string {
	// First check environment variable (highest priority)
	if envBranch := os.Getenv(syncbranch.EnvVar); envBranch != "" {
		return envBranch
	}

	// Read config.yaml directly from the .beads directory
	configPath := filepath.Join(beadsDir, "config.yaml")
	data, err := os.ReadFile(configPath) // #nosec G304 - config file path from findBeadsDir
	if err != nil {
		return ""
	}

	// Parse YAML properly to handle edge cases (comments, indentation, special chars)
	var cfg localConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		debug.Logf("Warning: failed to parse config.yaml for sync-branch: %v", err)
		return ""
	}

	return cfg.SyncBranch
}
