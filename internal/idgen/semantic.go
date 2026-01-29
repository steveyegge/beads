package idgen

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/steveyegge/beads/internal/validation"
)

// StopWords are common words removed from titles during ID generation.
// These words don't add meaning to the ID.
var StopWords = map[string]bool{
	// Articles
	"a": true, "an": true, "the": true,
	// Prepositions
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"of": true, "with": true, "by": true, "from": true, "as": true,
	// Conjunctions
	"and": true, "or": true, "but": true, "nor": true,
	// Common verbs that don't add meaning
	"is": true, "are": true, "was": true, "were": true,
	"be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true,
	// Other common words
	"this": true, "that": true, "these": true, "those": true,
	"it": true, "its": true,
}

// PriorityPrefixes are words that indicate priority but don't add meaning to the ID.
var PriorityPrefixes = map[string]bool{
	"urgent":   true,
	"critical": true,
	"p0":       true,
	"p1":       true,
	"p2":       true,
	"p3":       true,
	"p4":       true,
	"blocker":  true,
	"hotfix":   true,
}

// nonAlphanumericRegex matches any non-alphanumeric character.
var nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9]+`)

// multipleUnderscoreRegex matches multiple consecutive underscores.
var multipleUnderscoreRegex = regexp.MustCompile(`_+`)

// SemanticIDGenerator generates semantic IDs from titles.
type SemanticIDGenerator struct {
	maxSlugLength int
}

// NewSemanticIDGenerator creates a new generator with default settings.
func NewSemanticIDGenerator() *SemanticIDGenerator {
	return &SemanticIDGenerator{
		maxSlugLength: 46, // Per spec: slug max 46 chars
	}
}

// GenerateSlug converts a title to a slug suitable for a semantic ID.
// The returned slug is lowercase, uses underscores as separators,
// and has stop words removed.
func (g *SemanticIDGenerator) GenerateSlug(title string) string {
	if title == "" {
		return "untitled"
	}

	// Lowercase
	slug := strings.ToLower(title)

	// Replace non-alphanumeric with spaces (to split words)
	slug = nonAlphanumericRegex.ReplaceAllString(slug, " ")

	// Split into words
	words := strings.Fields(slug)

	// Filter out stop words and priority prefixes
	filtered := make([]string, 0, len(words))
	for _, word := range words {
		if !StopWords[word] && !PriorityPrefixes[word] {
			filtered = append(filtered, word)
		}
	}

	// If all words were filtered, use the first word from original
	if len(filtered) == 0 && len(words) > 0 {
		filtered = []string{words[0]}
	}

	// Join with underscores
	slug = strings.Join(filtered, "_")

	// Ensure slug starts with a letter
	if len(slug) > 0 && !unicode.IsLetter(rune(slug[0])) {
		slug = "n" + slug // Prefix with 'n' for numeric starts
	}

	// Truncate to max length
	if len(slug) > g.maxSlugLength {
		// Try to truncate at word boundary
		truncated := slug[:g.maxSlugLength]
		if lastUnderscore := strings.LastIndex(truncated, "_"); lastUnderscore > g.maxSlugLength/2 {
			truncated = truncated[:lastUnderscore]
		}
		slug = truncated
	}

	// Ensure minimum length
	if len(slug) < 3 {
		slug = slug + strings.Repeat("x", 3-len(slug))
	}

	// Clean up any trailing/leading underscores
	slug = strings.Trim(slug, "_")

	// Replace multiple underscores with single
	slug = multipleUnderscoreRegex.ReplaceAllString(slug, "_")

	return slug
}

// GenerateSemanticID generates a complete semantic ID with prefix and type.
// It checks for collisions against existingIDs and adds a numeric suffix if needed.
func (g *SemanticIDGenerator) GenerateSemanticID(prefix, issueType, title string, existingIDs []string) string {
	// Get type abbreviation
	typeAbbrev := validation.SemanticIDTypeAbbreviations[issueType]
	if typeAbbrev == "" {
		typeAbbrev = "tsk" // Default to task
	}

	// Generate base slug
	slug := g.GenerateSlug(title)

	// Build base ID
	baseID := prefix + "-" + typeAbbrev + "-" + slug

	// Check for collisions
	id := baseID
	suffix := 2
	for contains(existingIDs, id) {
		id = baseID + "_" + itoa(suffix)
		suffix++
		if suffix > 99 {
			// Failsafe: if we have 99+ collisions, something is wrong
			break
		}
	}

	return id
}

// GenerateSemanticIDWithCallback generates a semantic ID using a callback
// to check for collisions. This is useful when checking against a database.
func (g *SemanticIDGenerator) GenerateSemanticIDWithCallback(prefix, issueType, title string, exists func(id string) bool) string {
	// Get type abbreviation
	typeAbbrev := validation.SemanticIDTypeAbbreviations[issueType]
	if typeAbbrev == "" {
		typeAbbrev = "tsk" // Default to task
	}

	// Generate base slug
	slug := g.GenerateSlug(title)

	// Build base ID
	baseID := prefix + "-" + typeAbbrev + "-" + slug

	// Check for collisions using callback
	id := baseID
	suffix := 2
	for exists(id) {
		id = baseID + "_" + itoa(suffix)
		suffix++
		if suffix > 99 {
			break
		}
	}

	return id
}

// contains checks if a string is in a slice.
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// itoa converts an int to a string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
