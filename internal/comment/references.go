package comment

import (
	"regexp"
	"strings"
)

// Reference extraction patterns.
var refPatterns = []struct {
	re         *regexp.Regexp
	targetType RefTargetType
}{
	// "See foo.go:barFunc" or "See foo.go:123"
	{regexp.MustCompile(`(?i)\bsee\s+(\w[\w./]*\.go):(\w+|\d+)`), RefFile},
	// "See ValidateToken" (function reference without file, requires uppercase start, 3+ chars)
	{regexp.MustCompile(`\b[Ss]ee\s+([A-Z][a-zA-Z0-9_]{2,})\b`), RefFunction},
	// "Ref: SPEC_NAME.md" or "Ref: docs/FOO.md#section"
	{regexp.MustCompile(`(?i)\bref:\s*([^\s]+\.md(?:#[\w-]+)?)`), RefSpec},
	// "@links target"
	{regexp.MustCompile(`@links\s+(\S+)`), RefFile},
	// URLs
	{regexp.MustCompile(`https?://[^\s)>\]]+`), RefURL},
}

// functionRefExclusions filters out common words that follow "See" but aren't function names.
// GitHub issue shorthand (GH#), decision references, prose words.
var functionRefExclusions = map[string]bool{
	"EXTENDING": true, "Decision": true, "Also": true, "Note": true,
	"The": true, "This": true, "That": true, "These": true, "Those": true,
	"For": true, "From": true, "When": true, "Where": true, "Which": true,
	"Above": true, "Below": true, "Section": true, "Chapter": true,
	"Gas": true, "Town": true, "How": true, "Why": true, "What": true,
	"See": true, "Use": true, "New": true, "Old": true, "All": true,
	"JSONL": true, "JSON": true, "HTML": true, "HTTP": true, "HTTPS": true,
	"SQL": true, "API": true, "RPC": true, "CLI": true, "URL": true,
}

// ghIssuePattern matches "GH#123" — GitHub issue references, not function names.
var ghIssuePattern = regexp.MustCompile(`^GH#\d+`)

// ExtractReferences finds all cross-references in a comment's content.
func ExtractReferences(content string) []Ref {
	var refs []Ref
	seen := make(map[string]bool)

	for _, p := range refPatterns {
		matches := p.re.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			target := m[0]
			if p.targetType != RefURL && len(m) > 1 {
				target = m[1]
				if len(m) > 2 {
					target = m[1] + ":" + m[2]
				}
			}

			// Filter false-positive function references.
			if p.targetType == RefFunction {
				if functionRefExclusions[target] {
					continue
				}
				// Check if "See TARGET#" pattern (GitHub issue ref like "See GH#804").
				idx := strings.Index(m[0], target)
				if idx >= 0 {
					after := content[strings.Index(content, m[0])+idx+len(target):]
					if len(after) > 0 && after[0] == '#' {
						continue
					}
				}
			}

			key := string(p.targetType) + ":" + target
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, Ref{
				TargetType: p.targetType,
				Target:     target,
				Status:     RefUnknown,
			})
		}
	}
	return refs
}

// tagPattern matches @-tags: @tagname value
var tagPattern = regexp.MustCompile(`@(\w+)\s+(.+?)(?:\n|$)`)

// ExtractTags finds all @-tags in a comment.
func ExtractTags(content string) []Tag {
	var tags []Tag
	matches := tagPattern.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		tags = append(tags, Tag{
			Name:  m[1],
			Value: m[2],
		})
	}
	return tags
}
