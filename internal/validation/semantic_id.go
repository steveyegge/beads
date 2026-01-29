package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// SemanticIDTypeAbbreviations maps full issue types to their semantic ID abbreviations.
// These are 2-4 character type codes used in semantic IDs.
var SemanticIDTypeAbbreviations = map[string]string{
	"task":          "tsk",
	"merge-request": "mr",
	"bug":           "bug",
	"feature":       "feat",
	"event":         "evt",
	"message":       "msg",
	"epic":          "epc",
	"wisp":          "wsp",
	"molecule":      "mol",
	"agent":         "agt",
	"convoy":        "cvy",
	"chore":         "chr",
	"role":          "rol",
}

// SemanticIDAbbreviationToType is the reverse mapping from abbreviation to type.
var SemanticIDAbbreviationToType = map[string]string{}

func init() {
	for typ, abbrev := range SemanticIDTypeAbbreviations {
		SemanticIDAbbreviationToType[abbrev] = typ
	}
}

// semanticIDRegex validates the semantic ID format:
// {rig}-{type}-{slug}[_{n}]
// - rig: 2-4 lowercase letters (e.g., gt, bd, hq)
// - type: 2-4 lowercase letters (e.g., bug, tsk, feat)
// - slug: starts with letter, then 2-45 more alphanumeric/underscore chars
// - optional numeric suffix: _2, _3, etc.
var semanticIDRegex = regexp.MustCompile(`^[a-z]{2,4}-[a-z]{2,4}-[a-z][a-z0-9_]{2,45}(_[0-9]+)?$`)

// legacyIDRegex matches the old random hash-based IDs:
// {prefix}-{hash}[.{child}]
// Examples: bd-abc123, gt-x7q9z, hq-cv-abc.1
var legacyIDRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*-[a-z0-9]+(\.[0-9]+)?$`)

// SemanticIDParseResult contains the parsed components of a semantic ID.
type SemanticIDParseResult struct {
	Prefix      string // e.g., "gt"
	TypeAbbrev  string // e.g., "bug"
	Slug        string // e.g., "fix_login_timeout"
	Suffix      int    // collision suffix, 0 if none (1 = no suffix, 2+ = _2, _3, etc.)
	FullType    string // resolved full type, e.g., "bug" -> "bug", "tsk" -> "task"
	IsLegacy    bool   // true if this is a legacy random ID
}

// ValidateSemanticID validates that an ID follows the semantic ID format.
// It returns nil if valid, or an error describing the validation failure.
// This validates ONLY semantic IDs, not legacy random IDs.
func ValidateSemanticID(id string) error {
	if id == "" {
		return fmt.Errorf("ID cannot be empty")
	}

	if !semanticIDRegex.MatchString(id) {
		return fmt.Errorf("invalid semantic ID format '%s' (expected: prefix-type-slug, e.g., 'gt-bug-fix_login_timeout')", id)
	}

	// Parse and validate the type abbreviation
	parts := strings.SplitN(id, "-", 3)
	if len(parts) < 3 {
		return fmt.Errorf("invalid semantic ID format '%s' (missing components)", id)
	}

	typeAbbrev := parts[1]
	if _, ok := SemanticIDAbbreviationToType[typeAbbrev]; !ok {
		validTypes := make([]string, 0, len(SemanticIDTypeAbbreviations))
		for _, abbrev := range SemanticIDTypeAbbreviations {
			validTypes = append(validTypes, abbrev)
		}
		return fmt.Errorf("invalid type abbreviation '%s' in ID '%s' (valid: %s)", typeAbbrev, id, strings.Join(validTypes, ", "))
	}

	return nil
}

// ParseSemanticID parses a semantic ID into its components.
// Returns an error if the ID is not a valid semantic ID.
func ParseSemanticID(id string) (*SemanticIDParseResult, error) {
	if err := ValidateSemanticID(id); err != nil {
		return nil, err
	}

	parts := strings.SplitN(id, "-", 3)
	result := &SemanticIDParseResult{
		Prefix:     parts[0],
		TypeAbbrev: parts[1],
		Suffix:     1, // Default: no suffix means "first" instance
	}

	// Extract slug and optional collision suffix.
	// Collision suffixes are _2, _3, ..., _99 (limited range to distinguish from
	// numbers that are part of the slug like fix_issue_123).
	// First instance has no suffix, so suffix=1 means "no collision suffix".
	slugPart := parts[2]
	if idx := strings.LastIndex(slugPart, "_"); idx > 0 {
		potentialSuffix := slugPart[idx+1:]
		// Only treat as collision suffix if:
		// 1. It's a number in range 2-99 (collision suffixes are small)
		// 2. The remaining slug would still be valid (>= 3 chars)
		if isNumeric(potentialSuffix) && len(potentialSuffix) <= 2 {
			var suffixNum int
			fmt.Sscanf(potentialSuffix, "%d", &suffixNum)
			if suffixNum >= 2 && suffixNum <= 99 && idx >= 3 {
				result.Slug = slugPart[:idx]
				result.Suffix = suffixNum
			} else {
				result.Slug = slugPart
			}
		} else {
			result.Slug = slugPart
		}
	} else {
		result.Slug = slugPart
	}

	// Resolve full type name
	result.FullType = SemanticIDAbbreviationToType[result.TypeAbbrev]

	return result, nil
}

// IsSemanticID checks if an ID follows the semantic ID format.
// Returns true for semantic IDs, false for legacy random IDs.
// An ID is only considered semantic if it matches the regex AND has a valid type abbreviation.
func IsSemanticID(id string) bool {
	if !semanticIDRegex.MatchString(id) {
		return false
	}
	// Must also have a valid type abbreviation
	parts := strings.SplitN(id, "-", 3)
	if len(parts) < 3 {
		return false
	}
	_, validType := SemanticIDAbbreviationToType[parts[1]]
	return validType
}

// IsLegacyID checks if an ID follows the legacy random hash format.
// Returns true for legacy IDs like "bd-abc123" or "gt-x7q9z.1".
func IsLegacyID(id string) bool {
	if IsSemanticID(id) {
		return false // Semantic IDs are not legacy
	}
	return legacyIDRegex.MatchString(id)
}

// ValidateIssueID validates an issue ID, accepting both semantic and legacy formats.
// This is the main entry point for ID validation that maintains backward compatibility.
func ValidateIssueID(id string) error {
	if id == "" {
		return fmt.Errorf("ID cannot be empty")
	}

	// Try semantic validation first
	// A valid semantic ID must match the regex AND have a valid type abbreviation
	if IsSemanticID(id) {
		if err := ValidateSemanticID(id); err == nil {
			return nil // Valid semantic ID
		}
		// If regex matched but type is invalid, fall through to legacy check
		// This handles compound prefixes like hq-cv-abc123
	}

	// Accept legacy IDs
	if IsLegacyID(id) {
		return nil
	}

	return fmt.Errorf("invalid ID format '%s' (must be semantic format 'prefix-type-slug' or legacy format 'prefix-hash')", id)
}

// ValidateSlug validates a semantic ID slug component.
// Slugs must:
// - Start with a lowercase letter
// - Contain only lowercase letters, numbers, and underscores
// - Be 3-46 characters long (without any numeric suffix)
func ValidateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug cannot be empty")
	}

	if len(slug) < 3 {
		return fmt.Errorf("slug too short '%s' (minimum 3 characters)", slug)
	}

	if len(slug) > 46 {
		return fmt.Errorf("slug too long '%s' (maximum 46 characters)", slug)
	}

	if slug[0] < 'a' || slug[0] > 'z' {
		return fmt.Errorf("slug must start with a lowercase letter (got '%c')", slug[0])
	}

	slugRegex := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	if !slugRegex.MatchString(slug) {
		return fmt.Errorf("slug '%s' contains invalid characters (only lowercase letters, numbers, and underscores allowed)", slug)
	}

	return nil
}

// ReservedWords are words that cannot be used as standalone slugs.
// These conflict with CLI command names or have special meaning.
var ReservedWords = []string{
	"new", "list", "show", "create", "delete", "update", "close",
	"all", "none", "true", "false",
	"help", "version", "config",
}

// IsReservedWord checks if a slug is a reserved word.
func IsReservedWord(slug string) bool {
	for _, word := range ReservedWords {
		if slug == word {
			return true
		}
	}
	return false
}

// isNumeric checks if a string contains only digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
