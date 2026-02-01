package main

import "testing"

func TestParseSelection(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  []int
	}{
		{"1", 3, []int{0}},
		{"2", 3, []int{1}},
		{"1,2", 3, []int{0, 1}},
		{"1-3", 3, []int{0, 1, 2}},
		{"all", 3, []int{0, 1, 2}},
		{"both", 2, []int{0, 1}},
		{"none", 3, []int{}},
		{"", 3, []int{}},
		{"5", 3, []int{}},
	}

	for _, tt := range tests {
		got := parseSelection(tt.input, tt.max)
		if len(got) != len(tt.want) {
			t.Errorf("parseSelection(%q, %d) = %v, want %v", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestReflectTruncate(t *testing.T) {
	if got := reflectTruncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	if got := reflectTruncate("this is a very long string", 10); got != "this is..." {
		t.Errorf("truncate long = %q", got)
	}
}
