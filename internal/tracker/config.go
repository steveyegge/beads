package tracker

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Config holds configuration for a tracker integration.
// It wraps the config storage and provides a consistent interface
// for accessing tracker-specific settings.
type Config struct {
	// Prefix is the config key prefix for this tracker (e.g., "linear", "jira")
	Prefix string

	// Store provides access to the config storage
	Store ConfigStore

	// Context for config operations
	Ctx context.Context
}

// ConfigStore provides access to the beads configuration system.
type ConfigStore interface {
	GetConfig(ctx context.Context, key string) (string, error)
	SetConfig(ctx context.Context, key, value string) error
	GetAllConfig(ctx context.Context) (map[string]string, error)
}

// NewConfig creates a new tracker config with the given prefix and store.
func NewConfig(ctx context.Context, prefix string, store ConfigStore) *Config {
	return &Config{
		Prefix: prefix,
		Store:  store,
		Ctx:    ctx,
	}
}

// Get retrieves a config value by key, checking both the config store
// and environment variables. The key should not include the tracker prefix.
// Example: cfg.Get("api_key") for "linear" prefix looks up "linear.api_key"
// and falls back to "LINEAR_API_KEY" env var.
func (c *Config) Get(key string) (string, error) {
	fullKey := c.Prefix + "." + key

	// Try config store first
	if c.Store != nil {
		value, err := c.Store.GetConfig(c.Ctx, fullKey)
		if err == nil && value != "" {
			return value, nil
		}
	}

	// Fall back to environment variable
	envKey := c.envVarName(key)
	if envKey != "" {
		if value := os.Getenv(envKey); value != "" {
			return value, nil
		}
	}

	return "", nil
}

// GetRequired is like Get but returns an error if the value is empty.
func (c *Config) GetRequired(key string) (string, error) {
	value, err := c.Get(key)
	if err != nil {
		return "", err
	}
	if value == "" {
		envKey := c.envVarName(key)
		fullKey := c.Prefix + "." + key
		hint := fmt.Sprintf("Run: bd config set %s \"VALUE\"", fullKey)
		if envKey != "" {
			hint += fmt.Sprintf("\nOr: export %s=VALUE", envKey)
		}
		return "", fmt.Errorf("%s not configured\n%s", fullKey, hint)
	}
	return value, nil
}

// Set stores a config value.
func (c *Config) Set(key, value string) error {
	if c.Store == nil {
		return fmt.Errorf("config store not available")
	}
	fullKey := c.Prefix + "." + key
	return c.Store.SetConfig(c.Ctx, fullKey, value)
}

// GetAll returns all config values with the tracker's prefix.
func (c *Config) GetAll() (map[string]string, error) {
	if c.Store == nil {
		return make(map[string]string), nil
	}

	all, err := c.Store.GetAllConfig(c.Ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	prefix := c.Prefix + "."
	for key, value := range all {
		if strings.HasPrefix(key, prefix) {
			shortKey := strings.TrimPrefix(key, prefix)
			result[shortKey] = value
		}
	}
	return result, nil
}

// GetAllConfig implements ConfigLoader interface for passing to FieldMapper.
func (c *Config) GetAllConfig() (map[string]string, error) {
	if c.Store == nil {
		return make(map[string]string), nil
	}
	return c.Store.GetAllConfig(c.Ctx)
}

// envVarName converts a config key to its environment variable name.
// Example: for prefix "linear" and key "api_key", returns "LINEAR_API_KEY"
func (c *Config) envVarName(key string) string {
	// Convert to uppercase and replace dots with underscores
	envKey := strings.ToUpper(c.Prefix + "_" + key)
	envKey = strings.ReplaceAll(envKey, ".", "_")
	return envKey
}

// CommonConfig defines configuration keys used by all trackers.
var CommonConfig = struct {
	APIKey     string
	LastSync   string
	IDMode     string
	HashLength string
}{
	APIKey:     "api_key",
	LastSync:   "last_sync",
	IDMode:     "id_mode",
	HashLength: "hash_length",
}
