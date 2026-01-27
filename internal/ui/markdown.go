// Package ui provides terminal styling for beads CLI output.
package ui

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// getWrapWidth returns the terminal width for word wrapping.
// Caps at 100 chars for readability.
func getWrapWidth() int {
	const maxReadableWidth = 100
	wrapWidth := 80 // default if terminal size unavailable
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		wrapWidth = w
	}
	if wrapWidth > maxReadableWidth {
		wrapWidth = maxReadableWidth
	}
	return wrapWidth
}

// WrapText wraps text at word boundaries to fit within maxWidth.
// Preserves existing line breaks.
func WrapText(text string, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 80
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}
		result.WriteString(wrapLine(line, maxWidth))
	}

	return result.String()
}

// wrapLine wraps a single line at word boundaries.
func wrapLine(line string, maxWidth int) string {
	if utf8.RuneCountInString(line) <= maxWidth {
		return line
	}

	var result strings.Builder
	words := strings.Fields(line)
	currentLen := 0

	for _, word := range words {
		wordLen := utf8.RuneCountInString(word)

		// If this is first word on line, add it even if too long
		if currentLen == 0 {
			result.WriteString(word)
			currentLen = wordLen
			continue
		}

		// Check if word fits on current line (with space)
		if currentLen+1+wordLen <= maxWidth {
			result.WriteString(" ")
			result.WriteString(word)
			currentLen += 1 + wordLen
		} else {
			// Start new line
			result.WriteString("\n")
			result.WriteString(word)
			currentLen = wordLen
		}
	}

	return result.String()
}

// RenderMarkdown renders markdown text using glamour with beads theme colors.
// Returns the rendered markdown or the original text if rendering fails.
// Word wraps at terminal width (or 80 columns if width can't be detected).
func RenderMarkdown(markdown string) string {
	wrapWidth := getWrapWidth()

	// In agent mode, wrap text but skip glamour styling
	if IsAgentMode() {
		return WrapText(markdown, wrapWidth)
	}

	// If colors are disabled, wrap text but skip glamour styling
	if !ShouldUseColor() {
		return WrapText(markdown, wrapWidth)
	}

	// Create renderer with auto-detected style (respects terminal light/dark mode)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		// fallback to raw markdown on error
		return markdown
	}

	rendered, err := renderer.Render(markdown)
	if err != nil {
		// fallback to raw markdown on error
		return markdown
	}

	return rendered
}
