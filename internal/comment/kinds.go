package comment

import (
	"regexp"
	"strings"
)

var (
	todoPattern      = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|XXX|BUG)\b`)
	invariantPattern = regexp.MustCompile(`(?i)(^|\s)(@invariant|INVARIANT:|ASSERT:)`)
	referencePattern = regexp.MustCompile(`(?i)(^|\s)(see\s+\S|ref:\s*\S|@links\s+\S)`)
)

// ClassifyComment determines the Kind of a comment based on its content.
// Priority: invariant > todo > reference > doc (for multi-line) / inline.
func ClassifyComment(content string, isDoc bool) Kind {
	trimmed := strings.TrimSpace(content)

	if invariantPattern.MatchString(trimmed) {
		return KindInvariant
	}
	if todoPattern.MatchString(trimmed) {
		return KindTodo
	}
	if referencePattern.MatchString(trimmed) {
		return KindReference
	}
	if isDoc {
		return KindDoc
	}
	return KindInline
}
