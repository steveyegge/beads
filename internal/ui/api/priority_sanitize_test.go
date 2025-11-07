package api

import "testing"

func TestSanitizePriorityClampsToKnownTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input int
		want  string
	}{
		{input: -5, want: "p?"},
		{input: -1, want: "p?"},
		{input: 0, want: "p0"},
		{input: 1, want: "p1"},
		{input: 2, want: "p2"},
		{input: 3, want: "p3"},
		{input: 4, want: "p4"},
		{input: 5, want: "p?"},
		{input: 12, want: "p?"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := sanitizePriority(tc.input); got != tc.want {
				t.Fatalf("sanitizePriority(%d) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatPriorityLabelHandlesOutOfRangeValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input int
		want  string
	}{
		{input: -3, want: "P?"},
		{input: -1, want: "P?"},
		{input: 0, want: "P0"},
		{input: 1, want: "P1"},
		{input: 2, want: "P2"},
		{input: 3, want: "P3"},
		{input: 4, want: "P4"},
		{input: 5, want: "P?"},
		{input: 42, want: "P?"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := formatPriorityLabel(tc.input); got != tc.want {
				t.Fatalf("formatPriorityLabel(%d) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
