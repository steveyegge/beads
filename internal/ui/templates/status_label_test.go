package templates

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestStatusLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "returns empty string when status empty",
			input: "   ",
			want:  "",
		},
		{
			name:  "normalizes whitespace and casing for open",
			input: "  OPEN ",
			want:  "Ready",
		},
		{
			name:  "maps blocked status from types enum",
			input: string(types.StatusBlocked),
			want:  "Blocked",
		},
		{
			name:  "falls back to original status when unknown",
			input: "snoozed",
			want:  "snoozed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := StatusLabel(tt.input); got != tt.want {
				t.Fatalf("StatusLabel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
