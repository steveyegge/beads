// Package namespace provides sources configuration for managing project origins.
package namespace

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SourceConfig describes where a project's issues come from.
// Like go.mod or package.json, it maps a project identity to actual repositories.
type SourceConfig struct {
	Upstream string `yaml:"upstream"` // Canonical source (e.g., github.com/steveyegge/beads)
	Fork     string `yaml:"fork"`     // User's fork (optional, e.g., github.com/matt/beads)
	Local    string `yaml:"local"`    // Local override (optional, file path)
}

// SourcesConfig is the root configuration for all project sources
type SourcesConfig struct {
	Sources map[string]SourceConfig `yaml:"sources"`
}

// LoadSourcesConfig loads the sources configuration from .beads/sources.yaml
// Returns empty config if file doesn't exist (not an error).
func LoadSourcesConfig(beadsDir string) (*SourcesConfig, error) {
	path := beadsDir + "/sources.yaml"

	// Check if file exists
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config - not an error
			return &SourcesConfig{Sources: make(map[string]SourceConfig)}, nil
		}
		return nil, fmt.Errorf("failed to stat sources.yaml: %w", err)
	}

	// Read and parse the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read sources.yaml: %w", err)
	}

	cfg := &SourcesConfig{Sources: make(map[string]SourceConfig)}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse sources.yaml: %w", err)
	}

	return cfg, nil
}

// SaveSourcesConfig writes the sources configuration to .beads/sources.yaml
func SaveSourcesConfig(beadsDir string, cfg *SourcesConfig) error {
	path := beadsDir + "/sources.yaml"

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal sources.yaml: %w", err)
	}

	// Write to file with 0644 permissions
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write sources.yaml: %w", err)
	}

	return nil
}

// GetSourceURL returns the effective source URL for a project
// Checks: local override → fork → upstream (in that order)
func (cfg *SourceConfig) GetSourceURL() string {
	if cfg.Local != "" {
		return cfg.Local
	}
	if cfg.Fork != "" {
		return cfg.Fork
	}
	return cfg.Upstream
}

// Validate checks that the configuration is valid
func (cfg *SourceConfig) Validate() error {
	if cfg.Upstream == "" {
		return fmt.Errorf("upstream source is required")
	}
	return nil
}

// AddProject adds or updates a project source configuration
func (sc *SourcesConfig) AddProject(project, upstream string) error {
	if !isValidProjectName(project) {
		return fmt.Errorf("invalid project name: %s", project)
	}
	if upstream == "" {
		return fmt.Errorf("upstream is required")
	}

	sc.Sources[project] = SourceConfig{Upstream: upstream}
	return nil
}

// GetProject retrieves a project's source configuration
func (sc *SourcesConfig) GetProject(project string) (SourceConfig, error) {
	cfg, ok := sc.Sources[project]
	if !ok {
		return SourceConfig{}, fmt.Errorf("project not found: %s", project)
	}
	return cfg, nil
}

// SetProjectFork sets the fork for a project
func (sc *SourcesConfig) SetProjectFork(project, fork string) error {
	if !isValidProjectName(project) {
		return fmt.Errorf("invalid project name: %s", project)
	}

	cfg, ok := sc.Sources[project]
	if !ok {
		return fmt.Errorf("project not found: %s", project)
	}

	cfg.Fork = fork
	sc.Sources[project] = cfg
	return nil
}

// SetProjectLocal sets the local override for a project
func (sc *SourcesConfig) SetProjectLocal(project, local string) error {
	if !isValidProjectName(project) {
		return fmt.Errorf("invalid project name: %s", project)
	}

	cfg, ok := sc.Sources[project]
	if !ok {
		return fmt.Errorf("project not found: %s", project)
	}

	cfg.Local = local
	sc.Sources[project] = cfg
	return nil
}

// Example returns an example sources configuration for documentation
func ExampleSourcesConfig() *SourcesConfig {
	return &SourcesConfig{
		Sources: map[string]SourceConfig{
			"beads": {
				Upstream: "github.com/steveyegge/beads",
				Fork:     "github.com/matt/beads",
			},
			"other-project": {
				Upstream: "github.com/other/project",
			},
		},
	}
}
