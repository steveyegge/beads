package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LocalConfig represents the subset of config.yaml fields that need to be read
// directly from the file rather than through the viper singleton. This is needed
// when the CWD has changed since config initialization, or when checking config
// before viper is initialized.
//
// This consolidates duplicate localConfig structs that were in:
// - cmd/bd/autoimport.go
// - cmd/bd/doctor/database.go
//
// Using proper YAML parsing handles edge cases like comments, indentation, and
// special characters that regex-based parsing would miss.
type LocalConfig struct {
	SyncBranch  string `yaml:"sync-branch"`
	NoDb        bool   `yaml:"no-db"`
	IssuePrefix string `yaml:"issue-prefix"`
	Author      string `yaml:"author"`
}

// LoadLocalConfig reads and parses config.yaml directly from the specified beads directory.
// This bypasses the viper singleton and reads the file directly, which is useful when:
// - CWD has changed since config initialization
// - Checking config before viper is initialized
// - Need to read config from a different beads directory than the one viper was initialized with
//
// Returns an empty LocalConfig (not nil) if the file doesn't exist or can't be parsed.
func LoadLocalConfig(beadsDir string) *LocalConfig {
	configPath := filepath.Join(beadsDir, "config.yaml")
	data, err := os.ReadFile(configPath) // #nosec G304 - config file path from beadsDir
	if err != nil {
		return &LocalConfig{}
	}

	var cfg LocalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return &LocalConfig{}
	}

	return &cfg
}

// LoadLocalConfigWithEnv reads config.yaml and applies environment variable overrides.
// Environment variables take precedence over config file values.
//
// Supported environment variables:
// - BEADS_SYNC_BRANCH: overrides sync-branch
func LoadLocalConfigWithEnv(beadsDir string) *LocalConfig {
	cfg := LoadLocalConfig(beadsDir)

	// Apply environment variable overrides
	if envBranch := os.Getenv("BEADS_SYNC_BRANCH"); envBranch != "" {
		cfg.SyncBranch = envBranch
	}

	return cfg
}

// IsNoDbModeConfigured checks if no-db: true is set in config.yaml.
// Uses proper YAML parsing to avoid false matches in comments or nested keys.
//
// This is a convenience function that wraps LoadLocalConfig for the common case
// of just checking the no-db setting.
func IsNoDbModeConfigured(beadsDir string) bool {
	return LoadLocalConfig(beadsDir).NoDb
}

// GetLocalSyncBranch reads sync-branch from the local config.yaml file.
// First checks BEADS_SYNC_BRANCH environment variable, then falls back to config.yaml.
//
// This is a convenience function that wraps LoadLocalConfigWithEnv for the common
// case of just checking the sync-branch setting.
func GetLocalSyncBranch(beadsDir string) string {
	return LoadLocalConfigWithEnv(beadsDir).SyncBranch
}
