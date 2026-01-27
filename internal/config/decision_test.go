package config

import (
	"testing"
	"time"
)

func TestDecisionDefaults(t *testing.T) {
	// Reset and reinitialize config to get fresh defaults
	ResetForTesting()
	if err := Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Test default routes
	defaultRoutes := GetStringSlice(KeyDecisionRoutesDefault)
	if len(defaultRoutes) != 2 || defaultRoutes[0] != "email" || defaultRoutes[1] != "webhook" {
		t.Errorf("Default routes = %v, want [email webhook]", defaultRoutes)
	}

	urgentRoutes := GetStringSlice(KeyDecisionRoutesUrgent)
	if len(urgentRoutes) != 3 || urgentRoutes[0] != "email" || urgentRoutes[1] != "sms" || urgentRoutes[2] != "webhook" {
		t.Errorf("Urgent routes = %v, want [email sms webhook]", urgentRoutes)
	}

	// Test default timeout
	timeout := GetDecisionDefaultTimeout()
	if timeout != 24*time.Hour {
		t.Errorf("DefaultTimeout = %v, want 24h", timeout)
	}

	// Test remind interval
	interval := GetDecisionRemindInterval()
	if interval != 4*time.Hour {
		t.Errorf("RemindInterval = %v, want 4h", interval)
	}

	// Test max reminders
	maxReminders := GetDecisionMaxReminders()
	if maxReminders != 3 {
		t.Errorf("MaxReminders = %d, want 3", maxReminders)
	}

	// Test max iterations
	maxIterations := GetDecisionMaxIterations()
	if maxIterations != 3 {
		t.Errorf("MaxIterations = %d, want 3", maxIterations)
	}

	// Test auto-accept on max
	autoAccept := GetDecisionAutoAcceptOnMax()
	if autoAccept != false {
		t.Errorf("AutoAcceptOnMax = %v, want false", autoAccept)
	}
}

func TestGetDecisionSettings(t *testing.T) {
	// Reset and reinitialize config to get fresh defaults
	ResetForTesting()
	if err := Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	settings := GetDecisionSettings()

	// Check routes
	if len(settings.Routes.Default) != 2 {
		t.Errorf("Routes.Default length = %d, want 2", len(settings.Routes.Default))
	}
	if len(settings.Routes.Urgent) != 3 {
		t.Errorf("Routes.Urgent length = %d, want 3", len(settings.Routes.Urgent))
	}

	// Check behavior settings
	if settings.Settings.DefaultTimeout != 24*time.Hour {
		t.Errorf("Settings.DefaultTimeout = %v, want 24h", settings.Settings.DefaultTimeout)
	}
	if settings.Settings.RemindInterval != 4*time.Hour {
		t.Errorf("Settings.RemindInterval = %v, want 4h", settings.Settings.RemindInterval)
	}
	if settings.Settings.MaxReminders != 3 {
		t.Errorf("Settings.MaxReminders = %d, want 3", settings.Settings.MaxReminders)
	}
	if settings.Settings.MaxIterations != 3 {
		t.Errorf("Settings.MaxIterations = %d, want 3", settings.Settings.MaxIterations)
	}
	if settings.Settings.AutoAcceptOnMax != false {
		t.Errorf("Settings.AutoAcceptOnMax = %v, want false", settings.Settings.AutoAcceptOnMax)
	}
}

func TestGetDecisionRoutes(t *testing.T) {
	// Reset and reinitialize config to get fresh defaults
	ResetForTesting()
	if err := Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	tests := []struct {
		name     string
		priority int
		wantLen  int
		wantType string // "default" or "urgent"
	}{
		{"P0 uses urgent", 0, 3, "urgent"},
		{"P1 uses urgent", 1, 3, "urgent"},
		{"P2 uses default", 2, 2, "default"},
		{"P3 uses default", 3, 2, "default"},
		{"P4 uses default", 4, 2, "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := GetDecisionRoutes(tt.priority)
			if len(routes) != tt.wantLen {
				t.Errorf("GetDecisionRoutes(%d) length = %d, want %d", tt.priority, len(routes), tt.wantLen)
			}
		})
	}
}

func TestDecisionConfigKeys(t *testing.T) {
	// Verify config keys are correct
	tests := []struct {
		key  string
		want string
	}{
		{KeyDecisionRoutesDefault, "decision.routes.default"},
		{KeyDecisionRoutesUrgent, "decision.routes.urgent"},
		{KeyDecisionDefaultTimeout, "decision.settings.default-timeout"},
		{KeyDecisionRemindInterval, "decision.settings.remind-interval"},
		{KeyDecisionMaxReminders, "decision.settings.max-reminders"},
		{KeyDecisionMaxIterations, "decision.settings.max-iterations"},
		{KeyDecisionAutoAcceptOnMax, "decision.settings.auto-accept-on-max"},
	}

	for _, tt := range tests {
		if tt.key != tt.want {
			t.Errorf("Key %q != expected %q", tt.key, tt.want)
		}
	}
}
