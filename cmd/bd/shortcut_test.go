package main

import "testing"

func TestIsValidShortcutUUID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid uuid with hyphens", "5e330b96-ac5f-44b1-9c7e-a034627c81c8", true},
		{"valid uuid without hyphens", "5e330b96ac5f44b19c7ea034627c81c8", true},
		{"valid uuid uppercase", "5E330B96-AC5F-44B1-9C7E-A034627C81C8", true},
		{"empty string", "", false},
		{"too short", "5e330b96", false},
		{"too long", "5e330b96-ac5f-44b1-9c7e-a034627c81c8-extra", false},
		{"invalid characters", "5e330b96-xxxx-44b1-9c7e-a034627c81c8", false},
		{"mention name", "engineering", false},
		{"team name with spaces", "Engineering Team", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidShortcutUUID(tt.input)
			if got != tt.want {
				t.Errorf("isValidShortcutUUID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
