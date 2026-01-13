package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ConfigFileName = "metadata.json"

type Config struct {
	Database    string `json:"database"`
	JSONLExport string `json:"jsonl_export,omitempty"`

	// Deletions configuration
	DeletionsRetentionDays int `json:"deletions_retention_days,omitempty"` // 0 means use default (3 days)

	// Namespace configuration (BRANCH_NAMESPACING)
	ProjectName  string `json:"project_name,omitempty"`   // Project identifier for namespacing (e.g., "beads")
	DefaultBranch string `json:"default_branch,omitempty"` // Default branch for new issues (defaults to "main")

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
	return filepath.Join(beadsDir, c.Database)
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

// GetProjectName returns the configured project name, or auto-detects from git if empty.
// Returns empty string if neither is available.
func (c *Config) GetProjectName() string {
	if c.ProjectName != "" {
		return c.ProjectName
	}

	// Auto-detect from git remote
	return detectProjectFromGitRemote()
}

// GetDefaultBranch returns the configured default branch, defaulting to "main".
func (c *Config) GetDefaultBranch() string {
	if c.DefaultBranch != "" {
		return c.DefaultBranch
	}
	return "main"
}

// detectProjectFromGitRemote is a helper to extract project name from git remote URL.
// Returns empty string if git is not available or remote is not configured.
func detectProjectFromGitRemote() string {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	url := strings.TrimSpace(string(output))
	if url == "" {
		return ""
	}

	// Extract repository name from URL
	// Remove .git suffix if present
	if strings.HasSuffix(url, ".git") {
		url = url[:len(url)-4]
	}

	// Handle SSH URLs (git@github.com:user/repo)
	if strings.Contains(url, ":") {
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			url = parts[1]
		}
	}

	// Handle HTTPS URLs (https://github.com/user/repo)
	if strings.Contains(url, "://") {
		parts := strings.SplitN(url, "://", 2)
		if len(parts) == 2 {
			url = parts[1]
		}
	}

	// Extract last component (repo name)
	if strings.Contains(url, "/") {
		parts := strings.Split(url, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// If no slashes (local path), just use basename
	return filepath.Base(url)
}
