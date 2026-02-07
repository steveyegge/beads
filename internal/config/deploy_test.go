package config

import (
	"testing"
)

func TestIsDeployKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"deploy.dolt_host", true},
		{"deploy.anything", true},
		{"deploy.", true},
		{"jira.url", false},
		{"status.custom", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsDeployKey(tt.key); got != tt.want {
				t.Errorf("IsDeployKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestLookupDeployKey(t *testing.T) {
	// Known key
	dk := LookupDeployKey("deploy.dolt_host")
	if dk == nil {
		t.Fatal("expected deploy.dolt_host to be a known key")
	}
	if dk.EnvVar != "BEADS_DOLT_SERVER_HOST" {
		t.Errorf("expected EnvVar BEADS_DOLT_SERVER_HOST, got %s", dk.EnvVar)
	}

	// Unknown key
	dk = LookupDeployKey("deploy.nonexistent")
	if dk != nil {
		t.Error("expected nil for unknown key")
	}
}

func TestValidateDeployKey_Known(t *testing.T) {
	// Valid host
	if err := ValidateDeployKey("deploy.dolt_host", "my-dolt-service"); err != nil {
		t.Errorf("unexpected error for valid host: %v", err)
	}

	// Valid port
	if err := ValidateDeployKey("deploy.dolt_port", "3306"); err != nil {
		t.Errorf("unexpected error for valid port: %v", err)
	}

	// Invalid port
	if err := ValidateDeployKey("deploy.dolt_port", "not-a-number"); err == nil {
		t.Error("expected error for non-numeric port")
	}

	// Port out of range
	if err := ValidateDeployKey("deploy.dolt_port", "99999"); err == nil {
		t.Error("expected error for port out of range")
	}

	// Valid log level
	if err := ValidateDeployKey("deploy.daemon_log_level", "debug"); err != nil {
		t.Errorf("unexpected error for valid log level: %v", err)
	}

	// Invalid log level
	if err := ValidateDeployKey("deploy.daemon_log_level", "verbose"); err == nil {
		t.Error("expected error for invalid log level")
	}

	// Valid bool
	if err := ValidateDeployKey("deploy.tls_enabled", "true"); err != nil {
		t.Errorf("unexpected error for valid bool: %v", err)
	}

	// Invalid bool
	if err := ValidateDeployKey("deploy.tls_enabled", "maybe"); err == nil {
		t.Error("expected error for invalid bool")
	}
}

func TestValidateDeployKey_Unknown(t *testing.T) {
	err := ValidateDeployKey("deploy.unknown_key", "value")
	if err == nil {
		t.Error("expected error for unknown deploy key")
	}
}

func TestDeployKeyEnvMap(t *testing.T) {
	m := DeployKeyEnvMap()

	if m["deploy.dolt_host"] != "BEADS_DOLT_SERVER_HOST" {
		t.Errorf("expected BEADS_DOLT_SERVER_HOST, got %s", m["deploy.dolt_host"])
	}
	if m["deploy.redis_url"] != "BD_REDIS_URL" {
		t.Errorf("expected BD_REDIS_URL, got %s", m["deploy.redis_url"])
	}

	// Keys without env var should not appear
	if _, ok := m["deploy.ingress_host"]; ok {
		t.Error("deploy.ingress_host has no env var, should not be in map")
	}
}

func TestAllDeployKeysHaveDescriptions(t *testing.T) {
	for _, dk := range DeployKeys {
		if dk.Description == "" {
			t.Errorf("deploy key %q has no description", dk.Key)
		}
	}
}

func TestDeployKeyNoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, dk := range DeployKeys {
		if seen[dk.Key] {
			t.Errorf("duplicate deploy key: %s", dk.Key)
		}
		seen[dk.Key] = true
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"3306", true},
		{"1", true},
		{"65535", true},
		{"0", false},
		{"65536", false},
		{"-1", false},
		{"abc", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := validatePort(tt.value)
			if tt.valid && err != nil {
				t.Errorf("validatePort(%q) unexpected error: %v", tt.value, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("validatePort(%q) expected error, got nil", tt.value)
			}
		})
	}
}

func TestValidateLogLevel(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		if err := validateLogLevel(level); err != nil {
			t.Errorf("validateLogLevel(%q) unexpected error: %v", level, err)
		}
	}
	if err := validateLogLevel("trace"); err == nil {
		t.Error("expected error for invalid log level 'trace'")
	}
}

func TestValidateBool(t *testing.T) {
	for _, val := range []string{"true", "false", "1", "0", "yes", "no"} {
		if err := validateBool(val); err != nil {
			t.Errorf("validateBool(%q) unexpected error: %v", val, err)
		}
	}
	if err := validateBool("maybe"); err == nil {
		t.Error("expected error for invalid bool 'maybe'")
	}
}
