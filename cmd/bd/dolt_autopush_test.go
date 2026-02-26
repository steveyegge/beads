package main

import (
	"testing"
)

func TestGetDoltAutoPushMode(t *testing.T) {
	old := doltAutoPush
	defer func() { doltAutoPush = old }()

	doltAutoPush = "on"
	mode, err := getDoltAutoPushMode()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != doltAutoPushOn {
		t.Fatalf("expected on, got %q", mode)
	}

	doltAutoPush = "off"
	mode, err = getDoltAutoPushMode()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != doltAutoPushOff {
		t.Fatalf("expected off, got %q", mode)
	}

	// Empty defaults to off
	doltAutoPush = ""
	mode, err = getDoltAutoPushMode()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != doltAutoPushOff {
		t.Fatalf("expected off for empty, got %q", mode)
	}

	// Invalid mode
	doltAutoPush = "invalid"
	_, err = getDoltAutoPushMode()
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestCompareDoltVersion(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.82.4", "1.82.4", 0},
		{"1.82.5", "1.82.4", 1},
		{"1.82.3", "1.82.4", -1},
		{"1.83.0", "1.82.4", 1},
		{"2.0.0", "1.82.4", 1},
		{"1.82.0", "1.82.4", -1},
		{"0.9.0", "1.0.0", -1},
		// Pre-release suffix stripped
		{"1.82.4-rc1", "1.82.4", 0},
		// Leading "v" tolerated
		{"v1.82.4", "1.82.4", 0},
		// Malformed → 0.0.0
		{"", "1.0.0", -1},
		{"abc", "0.0.0", 0},
	}
	for _, tt := range tests {
		got := compareDoltVersion(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareDoltVersion(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
