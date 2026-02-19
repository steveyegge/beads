package main

import (
	"testing"
	"time"
)

func TestParseSchedulingFlag(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)

	t.Run("empty returns nil", func(t *testing.T) {
		t.Parallel()
		got, err := parseSchedulingFlag("due", "", now)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil time for empty input, got %v", *got)
		}
	})

	t.Run("valid relative time", func(t *testing.T) {
		t.Parallel()
		got, err := parseSchedulingFlag("defer", "+1h", now)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if got == nil {
			t.Fatal("expected parsed time, got nil")
		}
		want := now.Add(1 * time.Hour)
		if !got.Equal(want) {
			t.Fatalf("got %v, want %v", *got, want)
		}
	})

	t.Run("invalid input returns flag-specific help", func(t *testing.T) {
		t.Parallel()
		_, err := parseSchedulingFlag("until", "nope", now)
		if err == nil {
			t.Fatal("expected error for invalid input")
		}
		if err.Error() == "" {
			t.Fatal("expected non-empty error message")
		}
	})
}
