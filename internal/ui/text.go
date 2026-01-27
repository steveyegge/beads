// Package ui provides terminal styling for beads CLI output.
package ui

import (
	"strings"
	"unicode/utf8"
)

// Default truncation settings
const (
	DefaultMaxLines       = 15  // Default max lines for description display
	DefaultContextLines   = 5   // Lines to show at beginning and end when truncating
	DefaultMaxChars       = 500 // Default max chars for inline truncation
	DefaultContextChars   = 200 // Chars to show at beginning and end when truncating
	TruncationPlaceholder = "\n... [truncated - use --full to see complete text] ...\n"
)

// TruncateLines truncates text to maxLines, showing context from beginning and end.
// If the text has fewer lines than maxLines, returns it unchanged.
// Shows contextLines at the beginning and end with a truncation indicator in the middle.
func TruncateLines(text string, maxLines, contextLines int) string {
	if text == "" {
		return text
	}

	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	// No truncation needed
	if totalLines <= maxLines {
		return text
	}

	// Ensure context makes sense
	if contextLines < 1 {
		contextLines = DefaultContextLines
	}
	// If maxLines is too small for context, just show first maxLines
	if maxLines < contextLines*2+3 {
		return strings.Join(lines[:maxLines], "\n") + "\n..."
	}

	// Calculate how many lines to show from each end
	beginLines := contextLines
	endLines := contextLines
	hiddenLines := totalLines - beginLines - endLines

	// Build truncated output
	var result strings.Builder
	result.WriteString(strings.Join(lines[:beginLines], "\n"))
	result.WriteString("\n")
	result.WriteString(RenderMuted(strings.Repeat("─", 40)))
	result.WriteString("\n")
	result.WriteString(RenderMuted(strings.TrimSpace(
		strings.Replace(TruncationPlaceholder, "[truncated",
			"["+string(rune('0'+hiddenLines/100%10))+string(rune('0'+hiddenLines/10%10))+string(rune('0'+hiddenLines%10))+" lines truncated", 1))))
	// Simpler: just show the count
	result.WriteString(RenderMuted(" (" + itoa(hiddenLines) + " lines hidden)"))
	result.WriteString("\n")
	result.WriteString(RenderMuted(strings.Repeat("─", 40)))
	result.WriteString("\n")
	result.WriteString(strings.Join(lines[totalLines-endLines:], "\n"))

	return result.String()
}

// TruncateChars truncates text to maxChars, showing context from beginning and end.
// Uses smart word-boundary truncation to avoid cutting words in half.
func TruncateChars(text string, maxChars, contextChars int) string {
	if text == "" {
		return text
	}

	runeCount := utf8.RuneCountInString(text)

	// No truncation needed
	if runeCount <= maxChars {
		return text
	}

	// Ensure context makes sense
	if contextChars < 50 {
		contextChars = DefaultContextChars
	}
	// Marker takes some space
	markerLen := 50 // approximate length of truncation marker

	// If maxChars is too small, just truncate from end
	if maxChars < contextChars*2+markerLen {
		return truncateAtWordBoundary(text, maxChars-3) + "..."
	}

	// Get beginning and end portions
	runes := []rune(text)
	beginText := string(runes[:contextChars])
	endText := string(runes[runeCount-contextChars:])

	// Try to truncate at word boundaries
	beginText = truncateAtWordBoundary(beginText, contextChars)
	endText = truncateFromWordBoundary(endText, contextChars)

	hiddenChars := runeCount - utf8.RuneCountInString(beginText) - utf8.RuneCountInString(endText)

	return beginText + "\n" + RenderMuted("... ["+itoa(hiddenChars)+" chars hidden] ...") + "\n" + endText
}

// TruncateSimple performs simple end truncation with "..." suffix.
// UTF-8 safe.
func TruncateSimple(text string, maxLen int) string {
	if utf8.RuneCountInString(text) <= maxLen {
		return text
	}
	runes := []rune(text)
	if maxLen <= 3 {
		return "..."
	}
	return string(runes[:maxLen-3]) + "..."
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

	for i, word := range words {
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

		_ = i // suppress unused warning
	}

	return result.String()
}

// truncateAtWordBoundary truncates text to approximately maxLen chars,
// preferring to break at word boundaries.
func truncateAtWordBoundary(text string, maxLen int) string {
	if utf8.RuneCountInString(text) <= maxLen {
		return text
	}

	runes := []rune(text)
	if maxLen >= len(runes) {
		return text
	}

	// Look for last space within maxLen
	lastSpace := -1
	for i := maxLen - 1; i >= maxLen-50 && i >= 0; i-- {
		if runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\t' {
			lastSpace = i
			break
		}
	}

	if lastSpace > 0 {
		return strings.TrimRight(string(runes[:lastSpace]), " \t")
	}

	return string(runes[:maxLen])
}

// truncateFromWordBoundary removes text from the beginning to approximately match maxLen,
// preferring to break at word boundaries.
func truncateFromWordBoundary(text string, maxLen int) string {
	runeCount := utf8.RuneCountInString(text)
	if runeCount <= maxLen {
		return text
	}

	runes := []rune(text)
	startPos := runeCount - maxLen

	// Look for first space after startPos
	for i := startPos; i < startPos+50 && i < runeCount; i++ {
		if runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\t' {
			return strings.TrimLeft(string(runes[i+1:]), " \t")
		}
	}

	return string(runes[startPos:])
}

// ShouldTruncate returns true if text exceeds the given thresholds.
func ShouldTruncate(text string, maxLines, maxChars int) bool {
	if maxChars > 0 && utf8.RuneCountInString(text) > maxChars {
		return true
	}
	if maxLines > 0 && strings.Count(text, "\n")+1 > maxLines {
		return true
	}
	return false
}

// itoa converts int to string without importing strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
