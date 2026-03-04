package main

import "testing"

func TestParseHumanDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"7", 7, false},
		{"30", 30, false},
		{"7d", 7, false},
		{"30d", 30, false},
		{"2w", 14, false},
		{"1w", 7, false},
		{"48h", 2, false},
		{"12h", 1, false}, // rounds up to 1 day minimum
		{"7D", 7, false},
		{"2W", 14, false},
		{"", 0, true},
		{"0", 0, true},
		{"-1", 0, true},
		{"0d", 0, true},
		{"abc", 0, true},
		{"7x", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHumanDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseHumanDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseHumanDuration(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
