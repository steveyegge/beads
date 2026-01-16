package main

import (
	"testing"
)

func TestIsValidCommitSHA(t *testing.T) {
	tests := []struct {
		name     string
		sha      string
		expected bool
	}{
		{"valid full SHA", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", true},
		{"valid short SHA", "a1b2c3d", true},
		{"valid 8 char SHA", "a1b2c3d4", true},
		{"too short", "a1b2c3", false},
		{"too long", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c", false},
		{"invalid chars", "g1b2c3d", false},
		{"mixed case valid", "A1B2c3D4e5F6a1b2", true},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidCommitSHA(tt.sha)
			if result != tt.expected {
				t.Errorf("isValidCommitSHA(%q) = %v, want %v", tt.sha, result, tt.expected)
			}
		})
	}
}

func TestFindMatchingCommit(t *testing.T) {
	commits := []string{
		"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		"b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
		"c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
	}

	tests := []struct {
		name     string
		prefix   string
		expected string
	}{
		{"full SHA match", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"},
		{"short prefix match", "a1b2c3d", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"},
		{"uppercase prefix match", "A1B2C3D", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"},
		{"second commit match", "b2c3d4", "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3"},
		{"no match", "xxxxxx", ""},
		{"empty commits", "a1b2c3d", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input []string
			if tt.name != "empty commits" {
				input = commits
			}
			result := findMatchingCommit(input, tt.prefix)
			if result != tt.expected {
				t.Errorf("findMatchingCommit(%v, %q) = %q, want %q", input, tt.prefix, result, tt.expected)
			}
		})
	}
}
