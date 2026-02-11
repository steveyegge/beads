package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
	"gopkg.in/yaml.v3"
)

// LocalSource discovers resources from local files
type LocalSource struct {
	Paths []string
}

// NewLocalSource creates a new LocalSource
func NewLocalSource(paths []string) *LocalSource {
	return &LocalSource{Paths: paths}
}

// Name returns the source name
func (l *LocalSource) Name() string {
	return types.ResourceSourceLocal
}

// Discover scans directories for resource definition files
func (l *LocalSource) Discover(ctx context.Context) ([]*types.Resource, error) {
	var resources []*types.Resource

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	for _, path := range l.Paths {
		// Resolve relative paths
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}

		err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			// Only look for .yaml or .json files
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".yaml" && ext != ".yml" && ext != ".json" {
				return nil
			}

			// Try to parse as a resource
			res, err := parseResourceFile(path)
			if err != nil {
				// Log warning but continue? For now, skip files that don't match expected schema
				return nil
			}

			resources = append(resources, res)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("error walking path %s: %w", path, err)
		}
	}

	return resources, nil
}

// parseResourceFile parses a single resource file
// Expects YAML/JSON with: name, type, identifier, tags, etc.
func parseResourceFile(path string) (*types.Resource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Name       string   `yaml:"name" json:"name"`
		Type       string   `yaml:"type" json:"type"`
		Identifier string   `yaml:"identifier" json:"identifier"`
		Tags       []string `yaml:"tags" json:"tags"`
		// Capture remaining fields as config JSON
		Config map[string]interface{} `yaml:",inline" json:"-"`
	}

	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// Basic validation
	if raw.Name == "" || raw.Type == "" || raw.Identifier == "" {
		return nil, fmt.Errorf("missing required fields")
	}

	// Serialize config to JSON
	// We need to re-parse into map to get everything, or use a struct that captures "everything else"
	// Simpler approach: unmarshal into map, remove known fields, marshal rest
	var fullMap map[string]interface{}
	if err := yaml.Unmarshal(data, &fullMap); err != nil {
		return nil, err
	}
	delete(fullMap, "name")
	delete(fullMap, "type")
	delete(fullMap, "identifier")
	delete(fullMap, "tags")

	configJSON, err := json.Marshal(fullMap)
	if err != nil {
		return nil, err
	}

	return &types.Resource{
		Name:       raw.Name,
		Type:       raw.Type,
		Identifier: raw.Identifier,
		Source:     types.ResourceSourceLocal,
		Config:     string(configJSON),
		IsActive:   true,
		Tags:       raw.Tags,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, nil
}
