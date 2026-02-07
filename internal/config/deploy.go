package config

import (
	"fmt"
	"strconv"
	"strings"
)

// DeployKey describes a deploy.* configuration key.
type DeployKey struct {
	Key         string // Full key name (e.g., "deploy.dolt_host")
	Description string // Human-readable description
	EnvVar      string // Corresponding env var name (empty = no env mapping)
	Secret      bool   // If true, value must come from K8s Secret, not config
	Required    bool   // If true, daemon cannot start in K8s without this
	Default     string // Default value (empty = no default)
	Validate    func(string) error
}

// DeployKeys defines all valid deploy.* configuration keys.
// These are stored in the Dolt config table and read by the daemon after
// connecting to the database. Secrets (passwords, tokens) are NOT stored
// here — they remain in K8s Secrets/ExternalSecrets.
var DeployKeys = []DeployKey{
	// Dolt connection (bootstrap — also available as env vars)
	{
		Key:         "deploy.dolt_host",
		Description: "Dolt server hostname (K8s service name)",
		EnvVar:      "BEADS_DOLT_SERVER_HOST",
		Required:    true,
	},
	{
		Key:         "deploy.dolt_port",
		Description: "Dolt server port",
		EnvVar:      "BEADS_DOLT_SERVER_PORT",
		Default:     "3306",
		Validate:    validatePort,
	},
	{
		Key:         "deploy.dolt_database",
		Description: "Dolt database name",
		EnvVar:      "BEADS_DOLT_DATABASE",
		Default:     "beads",
	},
	{
		Key:         "deploy.dolt_user",
		Description: "Dolt username",
		EnvVar:      "BEADS_DOLT_USER",
		Default:     "root",
	},
	// Daemon network
	{
		Key:         "deploy.daemon_tcp_addr",
		Description: "Daemon TCP listen address",
		EnvVar:      "BD_DAEMON_TCP_ADDR",
		Default:     ":9876",
	},
	{
		Key:         "deploy.daemon_http_addr",
		Description: "Daemon HTTP listen address",
		EnvVar:      "BD_DAEMON_HTTP_ADDR",
		Default:     ":9080",
	},
	{
		Key:         "deploy.daemon_log_level",
		Description: "Daemon log level (debug, info, warn, error)",
		EnvVar:      "BD_LOG_LEVEL",
		Default:     "info",
		Validate:    validateLogLevel,
	},
	{
		Key:         "deploy.daemon_log_json",
		Description: "Enable JSON structured logging",
		EnvVar:      "BD_LOG_JSON",
		Default:     "false",
		Validate:    validateBool,
	},
	// Redis
	{
		Key:         "deploy.redis_url",
		Description: "Redis connection URL",
		EnvVar:      "BD_REDIS_URL",
	},
	{
		Key:         "deploy.redis_namespace",
		Description: "Redis key namespace prefix",
		EnvVar:      "BD_REDIS_NAMESPACE",
	},
	{
		Key:         "deploy.redis_wisp_ttl",
		Description: "Redis wisp TTL duration (e.g., 24h)",
		EnvVar:      "BD_REDIS_WISP_TTL",
	},
	// NATS
	{
		Key:         "deploy.nats_url",
		Description: "NATS server URL",
		EnvVar:      "BD_NATS_URL",
		Default:     "nats://localhost:4222",
	},
	// Slack
	{
		Key:         "deploy.slack_channel",
		Description: "Default Slack channel for decision notifications",
		EnvVar:      "SLACK_CHANNEL",
	},
	// TLS
	{
		Key:         "deploy.tls_enabled",
		Description: "Enable TLS for daemon TCP listener",
		EnvVar:      "BD_DAEMON_TLS_ENABLED",
		Default:     "false",
		Validate:    validateBool,
	},
	// Ingress
	{
		Key:         "deploy.ingress_host",
		Description: "Public ingress hostname",
	},
}

// deployKeyMap is a lookup table built from DeployKeys.
var deployKeyMap map[string]*DeployKey

func init() {
	deployKeyMap = make(map[string]*DeployKey, len(DeployKeys))
	for i := range DeployKeys {
		deployKeyMap[DeployKeys[i].Key] = &DeployKeys[i]
	}
}

// IsDeployKey returns true if the key is in the deploy.* namespace.
func IsDeployKey(key string) bool {
	return strings.HasPrefix(key, "deploy.")
}

// LookupDeployKey returns the DeployKey definition if key is a known deploy.* key.
// Returns nil if the key is not a recognized deploy key.
func LookupDeployKey(key string) *DeployKey {
	return deployKeyMap[key]
}

// ValidateDeployKey checks whether a deploy.* key is known and the value is valid.
// Returns nil if valid, or an error describing the problem.
func ValidateDeployKey(key, value string) error {
	dk := deployKeyMap[key]
	if dk == nil {
		known := make([]string, 0, len(DeployKeys))
		for _, k := range DeployKeys {
			known = append(known, k.Key)
		}
		return fmt.Errorf("unknown deploy key %q; valid keys: %s", key, strings.Join(known, ", "))
	}

	if dk.Secret {
		return fmt.Errorf("key %q is a secret and must not be stored in config (use K8s Secrets instead)", key)
	}

	if dk.Validate != nil {
		if err := dk.Validate(value); err != nil {
			return fmt.Errorf("invalid value for %s: %w", key, err)
		}
	}

	return nil
}

// DeployKeyEnvMap returns a mapping from deploy.* key to environment variable name.
func DeployKeyEnvMap() map[string]string {
	m := make(map[string]string, len(DeployKeys))
	for _, dk := range DeployKeys {
		if dk.EnvVar != "" {
			m[dk.Key] = dk.EnvVar
		}
	}
	return m
}

// Validation helpers

func validatePort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("must be a number, got %q", value)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("must be between 1 and 65535, got %d", port)
	}
	return nil
}

func validateLogLevel(value string) error {
	switch strings.ToLower(value) {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("must be one of: debug, info, warn, error; got %q", value)
	}
}

func validateBool(value string) error {
	switch strings.ToLower(value) {
	case "true", "false", "1", "0", "yes", "no":
		return nil
	default:
		return fmt.Errorf("must be true or false, got %q", value)
	}
}
