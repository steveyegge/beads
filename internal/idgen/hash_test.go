package idgen

import (
	"testing"
	"time"
)

func TestGenerateHashIDMatchesJiraVector(t *testing.T) {
	timestamp := time.Date(2024, 1, 2, 3, 4, 5, 6*1_000_000, time.UTC)
	prefix := "bd"
	title := "Fix login"
	description := "Details"
	creator := "jira-import"

	tests := map[int]string{
		3: "bd-vju",
		4: "bd-8d8e",
		5: "bd-bi3tk",
		6: "bd-8bi3tk",
		7: "bd-r5sr6bm",
		8: "bd-8r5sr6bm",
	}

	for length, expected := range tests {
		got := GenerateHashID(prefix, title, description, creator, timestamp, length, 0)
		if got != expected {
			t.Fatalf("length %d: got %s, want %s", length, got, expected)
		}
	}
}

// TestGenerateHashID_NormalizesPrefix verifies that trailing dashes are stripped
// from prefixes to prevent double-dash IDs like "hq--abc" (gt-bl4vnm)
func TestGenerateHashID_NormalizesPrefix(t *testing.T) {
	timestamp := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name           string
		prefix         string
		expectedPrefix string // prefix part of generated ID (before first dash+hash)
	}{
		{"no trailing dash", "hq", "hq"},
		{"trailing dash stripped", "hq-", "hq"},
		{"gt prefix no dash", "gt", "gt"},
		{"gt prefix with dash", "gt-", "gt"},
		{"bd prefix no dash", "bd", "bd"},
		{"bd prefix with dash", "bd-", "bd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := GenerateHashID(tt.prefix, "Test title", "desc", "creator", timestamp, 6, 0)
			// Check that ID starts with expected prefix followed by single dash
			if id[:len(tt.expectedPrefix)+1] != tt.expectedPrefix+"-" {
				t.Errorf("ID %q should start with %q-", id, tt.expectedPrefix)
			}
			// Verify no double dash
			if len(id) > len(tt.expectedPrefix)+1 && id[len(tt.expectedPrefix):len(tt.expectedPrefix)+2] == "--" {
				t.Errorf("ID %q has double dash", id)
			}
		})
	}
}
