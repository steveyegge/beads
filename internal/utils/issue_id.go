package utils

import (
	"fmt"
	"strings"
)

// ExtractIssuePrefix extracts the prefix from an issue ID like "bd-123" -> "bd"
// Uses the last hyphen before a numeric or hash-like suffix:
//   - "beads-vscode-1" -> "beads-vscode" (numeric suffix)
//   - "web-app-a3f8e9" -> "web-app" (hash suffix with digits)
//   - "my-cool-app-123" -> "my-cool-app" (numeric suffix)
//   - "bd-a3f" -> "bd" (3-char hash)
//
// Falls back to first hyphen when suffix looks like an English word (4+ chars, no digits):
//   - "vc-baseline-test" -> "vc" (word-like suffix: "test" is not a hash)
//   - "bd-multi-part-id" -> "bd" (word-like suffix: "id" is too short but "part-id" path)
//
// This distinguishes hash IDs (which may contain letters but have digits or are 3 chars)
// from multi-part IDs where the suffix after the first hyphen is the entire ID.
func ExtractIssuePrefix(issueID string) string {
	// Try last hyphen first (handles multi-part prefixes like "beads-vscode-1")
	lastIdx := strings.LastIndex(issueID, "-")
	if lastIdx <= 0 {
		return ""
	}

	suffix := issueID[lastIdx+1:]
	if len(suffix) == 0 {
		// Trailing hyphen like "bd-" - return prefix before the hyphen
		return issueID[:lastIdx]
	}

	// Extract the base part before any dot (handle "123.1.2" -> check "123")
	basePart := suffix
	if dotIdx := strings.Index(suffix, "."); dotIdx > 0 {
		basePart = suffix[:dotIdx]
	}

	// Check if this looks like a valid issue ID suffix (numeric or hash-like)
	// Use isLikelyHash which requires digits for 4+ char suffixes to avoid
	// treating English words like "test", "gate", "part" as hash IDs
	if isNumeric(basePart) || isLikelyHash(basePart) {
		return issueID[:lastIdx]
	}

	// Suffix looks like an English word (4+ chars, no digits) or contains special chars
	// Fall back to first hyphen - the entire part after first hyphen is the ID
	firstIdx := strings.Index(issueID, "-")
	if firstIdx <= 0 {
		return ""
	}
	return issueID[:firstIdx]
}

// isNumeric checks if a string contains only digits
func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isLikelyHash checks if a string looks like a hash ID suffix.
// Returns true for base36 strings of 3-8 characters (0-9, a-z).
//
// For 3-char suffixes: accepts all base36 (including all-letter like "bat", "dev").
// For 4+ char suffixes: requires at least one digit to distinguish from English words.
//
// Rationale (word collision probability):
//   - 3-char: 36³ = 46K hashes, ~1000 common words = ~2% (accept false positives)
//   - 4-char: 36⁴ = 1.6M hashes, ~3000 words = ~0.2% (digit requirement is safe)
//   - 5+ char: collision rate negligible
//
// Hash IDs in beads use adaptive length scaling from 3-8 characters.
func isLikelyHash(s string) bool {
	if len(s) < 3 || len(s) > 8 {
		return false
	}
	// 3-char suffixes get a free pass (word collision acceptable)
	// 4+ char suffixes require at least one digit
	hasDigit := len(s) == 3
	// Check if all characters are base36 (0-9, a-z)
	for _, c := range s {
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return hasDigit
}

// ExtractIssueNumber extracts the number from an issue ID like "bd-123" -> 123
func ExtractIssueNumber(issueID string) int {
	idx := strings.LastIndex(issueID, "-")
	if idx < 0 || idx == len(issueID)-1 {
		return 0
	}
	var num int
	_, _ = fmt.Sscanf(issueID[idx+1:], "%d", &num)
	return num
}
