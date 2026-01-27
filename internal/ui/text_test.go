package ui

import (
	"strings"
	"testing"
)

func TestTruncateSimple(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short text unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "truncate with ellipsis",
			input:  "hello world",
			maxLen: 8,
			want:   "hello...",
		},
		{
			name:   "very short maxLen",
			input:  "hello world",
			maxLen: 3,
			want:   "...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "unicode chars",
			input:  "héllo wörld",
			maxLen: 8,
			want:   "héllo...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateSimple(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateSimple(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestShouldTruncate(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLines int
		maxChars int
		want     bool
	}{
		{
			name:     "short text no truncation",
			text:     "hello",
			maxLines: 10,
			maxChars: 100,
			want:     false,
		},
		{
			name:     "exceeds char limit",
			text:     strings.Repeat("a", 200),
			maxLines: 0,
			maxChars: 100,
			want:     true,
		},
		{
			name:     "exceeds line limit",
			text:     "a\nb\nc\nd\ne\nf",
			maxLines: 3,
			maxChars: 0,
			want:     true,
		},
		{
			name:     "empty text",
			text:     "",
			maxLines: 10,
			maxChars: 100,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldTruncate(tt.text, tt.maxLines, tt.maxChars)
			if got != tt.want {
				t.Errorf("ShouldTruncate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateLines(t *testing.T) {
	// Create text with 20 lines
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + itoa(i+1)
	}
	longText := strings.Join(lines, "\n")

	tests := []struct {
		name         string
		text         string
		maxLines     int
		contextLines int
		wantPrefix   string // First few chars to check
		wantSuffix   string // Last few chars to check
		wantContains string // Something in the middle (truncation marker)
	}{
		{
			name:         "short text unchanged",
			text:         "line 1\nline 2\nline 3",
			maxLines:     10,
			contextLines: 2,
			wantPrefix:   "line 1\nline 2\nline 3",
		},
		{
			name:         "truncate long text",
			text:         longText,
			maxLines:     15,
			contextLines: 5,
			wantPrefix:   "line 1",
			wantSuffix:   "line 20",
			wantContains: "hidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateLines(tt.text, tt.maxLines, tt.contextLines)
			if tt.wantPrefix != "" && !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("TruncateLines() should start with %q, got %q", tt.wantPrefix, got[:min(len(got), 50)])
			}
			if tt.wantSuffix != "" && !strings.HasSuffix(strings.TrimSpace(got), tt.wantSuffix) {
				t.Errorf("TruncateLines() should end with %q, got %q", tt.wantSuffix, got[max(0, len(got)-50):])
			}
			if tt.wantContains != "" && !strings.Contains(got, tt.wantContains) {
				t.Errorf("TruncateLines() should contain %q", tt.wantContains)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxWidth int
		wantLine int // Number of lines expected
	}{
		{
			name:     "short line unchanged",
			text:     "hello world",
			maxWidth: 80,
			wantLine: 1,
		},
		{
			name:     "wrap long line",
			text:     "the quick brown fox jumps over the lazy dog",
			maxWidth: 20,
			wantLine: 3, // Should wrap into multiple lines
		},
		{
			name:     "preserve newlines",
			text:     "line 1\nline 2",
			maxWidth: 80,
			wantLine: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapText(tt.text, tt.maxWidth)
			gotLines := strings.Count(got, "\n") + 1
			if gotLines != tt.wantLine {
				t.Errorf("WrapText() got %d lines, want %d lines\nOutput: %q", gotLines, tt.wantLine, got)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
