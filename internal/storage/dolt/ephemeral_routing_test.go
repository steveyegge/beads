package dolt

import (
	"testing"
)

func TestIsEphemeralID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"bd-wisp-abc123", true},
		{"bd-wisp-8ajy9h", true},
		{"gt-wisp-patrol1", true},
		{"bd-1234", false},
		{"bd-abc", false},
		{"", false},
		{"wisp", false},
	}

	for _, tt := range tests {
		if got := IsEphemeralID(tt.id); got != tt.want {
			t.Errorf("IsEphemeralID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestPartitionIDs(t *testing.T) {
	ids := []string{"bd-wisp-a", "bd-123", "bd-wisp-b", "bd-456"}
	eph, dolt := partitionIDs(ids)
	if len(eph) != 2 {
		t.Errorf("ephemeral count %d, want 2", len(eph))
	}
	if len(dolt) != 2 {
		t.Errorf("dolt count %d, want 2", len(dolt))
	}
}

func TestAllEphemeral(t *testing.T) {
	if allEphemeral(nil) {
		t.Error("nil should return false")
	}
	if allEphemeral([]string{}) {
		t.Error("empty should return false")
	}
	if !allEphemeral([]string{"bd-wisp-a", "bd-wisp-b"}) {
		t.Error("all wisp should return true")
	}
	if allEphemeral([]string{"bd-wisp-a", "bd-123"}) {
		t.Error("mixed should return false")
	}
}
