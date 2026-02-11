package discovery

import (
	"context"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

// ResourceSourceType constants
const (
	SourceTypeLocal = "local"
)

// SourceConfig represents a resource source in config.yaml
type SourceConfig struct {
	Type  string   `yaml:"type"`
	Paths []string `yaml:"paths"`
}

// Config represents the resources section in config.yaml
type Config struct {
	Sources []SourceConfig `yaml:"sources"`
}

// ResourceSource is an interface for resource discovery providers
type ResourceSource interface {
	Name() string
	Discover(ctx context.Context) ([]*types.Resource, error)
}

// DiscoverResources scans configured sources and returns found resources
func DiscoverResources(ctx context.Context) ([]*types.Resource, error) {
	// Parse resources configuration
	var sourcesCfg []SourceConfig
	if err := config.UnmarshalKey("resources.sources", &sourcesCfg); err != nil {
		return nil, fmt.Errorf("failed to parse resource sources: %w", err)
	}

	var allResources []*types.Resource

	for _, src := range sourcesCfg {
		var source ResourceSource

		switch src.Type {
		case SourceTypeLocal:
			source = NewLocalSource(src.Paths)
		case SourceTypeLinear:
			source = NewLinearSource()
		default:
			fmt.Fprintf(os.Stderr, "Warning: unknown resource source type: %s\n", src.Type)
			continue
		}

		res, err := source.Discover(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to discover from source %s: %w", src.Type, err)
		}
		allResources = append(allResources, res...)
	}

	return allResources, nil
}
