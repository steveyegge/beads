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
