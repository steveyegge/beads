package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestShouldCheckGate(t *testing.T) {
	tests := []struct {
		name       string
		awaitType  string
		typeFilter string
		want       bool
	}{
		// Empty filter matches all
		{"empty filter matches gh:run", "gh:run", "", true},
		{"empty filter matches gh:pr", "gh:pr", "", true},
		{"empty filter matches timer", "timer", "", true},
		{"empty filter matches human", "human", "", true},

		// "all" filter matches all
		{"all filter matches gh:run", "gh:run", "all", true},
		{"all filter matches gh:pr", "gh:pr", "all", true},
		{"all filter matches timer", "timer", "all", true},

		// "gh" filter matches all GitHub types
		{"gh filter matches gh:run", "gh:run", "gh", true},
		{"gh filter matches gh:pr", "gh:pr", "gh", true},
		{"gh filter does not match timer", "timer", "gh", false},
		{"gh filter does not match human", "human", "gh", false},

		// Exact type filters
		{"gh:run filter matches gh:run", "gh:run", "gh:run", true},
		{"gh:run filter does not match gh:pr", "gh:pr", "gh:run", false},
		{"gh:pr filter matches gh:pr", "gh:pr", "gh:pr", true},
		{"gh:pr filter does not match gh:run", "gh:run", "gh:pr", false},
		{"timer filter matches timer", "timer", "timer", true},
		{"timer filter does not match gh:run", "gh:run", "timer", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := &types.Issue{
				AwaitType: tt.awaitType,
			}
			got := shouldCheckGate(gate, tt.typeFilter)
			if got != tt.want {
				t.Errorf("shouldCheckGate(%q, %q) = %v, want %v",
					tt.awaitType, tt.typeFilter, got, tt.want)
			}
		})
	}
}
