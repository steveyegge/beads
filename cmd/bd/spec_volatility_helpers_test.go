package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestClassifySpecVolatility(t *testing.T) {
	tests := []struct {
		name       string
		changes    int
		openIssues int
		wantLevel  specVolatilityLevel
	}{
		{"high-by-changes", 5, 0, specVolatilityHigh},
		{"high-by-mixed", 3, 3, specVolatilityHigh},
		{"medium", 2, 1, specVolatilityMedium},
		{"low-by-change", 1, 0, specVolatilityLow},
		{"low-by-open", 0, 1, specVolatilityLow},
		{"stable", 0, 0, specVolatilityStable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySpecVolatility(tt.changes, tt.openIssues); got != tt.wantLevel {
				t.Fatalf("classifySpecVolatility(%d,%d) = %s, want %s", tt.changes, tt.openIssues, got, tt.wantLevel)
			}
		})
	}
}

func TestStartOfWeek(t *testing.T) {
	input := time.Date(2026, 1, 29, 15, 30, 0, 0, time.UTC) // Thursday
	start := startOfWeek(input)
	if start.Weekday() != time.Monday {
		t.Fatalf("startOfWeek weekday = %s, want Monday", start.Weekday())
	}
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		t.Fatalf("startOfWeek not at midnight: %v", start)
	}
}

func TestTrendBar(t *testing.T) {
	bar := trendBar(3, 6)
	if utf8.RuneCountInString(bar) != 10 {
		t.Fatalf("trendBar length = %d, want 10", utf8.RuneCountInString(bar))
	}
	filled := strings.Count(bar, "█")
	empty := strings.Count(bar, "░")
	if filled+empty != 10 {
		t.Fatalf("trendBar segments = %d, want 10", filled+empty)
	}
	if filled == 0 {
		t.Fatalf("trendBar should render at least one filled segment")
	}
}

func TestLinearSlope(t *testing.T) {
	flat := []trendBucket{{Changes: 2}, {Changes: 2}, {Changes: 2}}
	if got := linearSlope(flat); got != 0 {
		t.Fatalf("linearSlope flat = %v, want 0", got)
	}

	up := []trendBucket{{Changes: 1}, {Changes: 2}, {Changes: 3}}
	if got := linearSlope(up); got <= 0 {
		t.Fatalf("linearSlope up = %v, want positive", got)
	}

	down := []trendBucket{{Changes: 3}, {Changes: 2}, {Changes: 1}}
	if got := linearSlope(down); got >= 0 {
		t.Fatalf("linearSlope down = %v, want negative", got)
	}
}

func TestTrendDirection(t *testing.T) {
	if got := trendDirection(0.5); !strings.Contains(got, "INCREASING") {
		t.Fatalf("trendDirection(0.5) = %q, want INCREASING", got)
	}
	if got := trendDirection(-0.5); !strings.Contains(got, "DECREASING") {
		t.Fatalf("trendDirection(-0.5) = %q, want DECREASING", got)
	}
	if got := trendDirection(0.0); !strings.Contains(got, "STABLE") {
		t.Fatalf("trendDirection(0.0) = %q, want STABLE", got)
	}
}
