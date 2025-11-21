package validation

import (
	"testing"
)

func TestParsePriority(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		// Numeric format
		{"0", 0},
		{"1", 1},
		{"2", 2},
		{"3", 3},
		{"4", 4},

		// P-prefix format (uppercase)
		{"P0", 0},
		{"P1", 1},
		{"P2", 2},
		{"P3", 3},
		{"P4", 4},

		// P-prefix format (lowercase)
		{"p0", 0},
		{"p1", 1},
		{"p2", 2},

		// With whitespace
		{" 1 ", 1},
		{" P1 ", 1},

		// Invalid cases (returns -1)
		{"5", -1},      // Out of range
		{"-1", -1},     // Negative
		{"P5", -1},     // Out of range with prefix
		{"abc", -1},    // Not a number
		{"P", -1},      // Just the prefix
		{"PP1", -1},    // Double prefix
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParsePriority(tt.input)
			if got != tt.expected {
				t.Errorf("ParsePriority(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidatePriority(t *testing.T) {
	tests := []struct {
		input     string
		wantValue int
		wantError bool
	}{
		{"0", 0, false},
		{"2", 2, false},
		{"P1", 1, false},
		{"5", -1, true},
		{"abc", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ValidatePriority(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePriority(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
				return
			}
			if got != tt.wantValue {
				t.Errorf("ValidatePriority(%q) = %d, want %d", tt.input, got, tt.wantValue)
			}
		})
	}
}

func TestValidateIDFormat(t *testing.T) {
	tests := []struct {
		input      string
		wantPrefix string
		wantError  bool
	}{
		{"", "", false},
		{"bd-a3f8e9", "bd", false},
		{"bd-42", "bd", false},
		{"bd-a3f8e9.1", "bd", false},
		{"foo-bar", "foo", false},
		{"nohyphen", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ValidateIDFormat(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateIDFormat(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
				return
			}
			if got != tt.wantPrefix {
				t.Errorf("ValidateIDFormat(%q) = %q, want %q", tt.input, got, tt.wantPrefix)
			}
		})
	}
}

func TestValidatePrefix(t *testing.T) {
	tests := []struct {
		name            string
		requestedPrefix string
		dbPrefix        string
		force           bool
		wantError       bool
	}{
		{"matching prefixes", "bd", "bd", false, false},
		{"empty db prefix", "bd", "", false, false},
		{"mismatched with force", "foo", "bd", true, false},
		{"mismatched without force", "foo", "bd", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePrefix(tt.requestedPrefix, tt.dbPrefix, tt.force)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePrefix() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
