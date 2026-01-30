package importer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
	"github.com/steveyegge/beads/internal/validation"
)

// IssueDataChanged checks if an issue's data has changed from the database version
func IssueDataChanged(existing *types.Issue, updates map[string]interface{}) bool {
	fc := newFieldComparator()
	for key, newVal := range updates {
		if fc.checkFieldChanged(key, existing, newVal) {
			return true
		}
	}
	return false
}

// fieldComparator handles comparison logic for different field types
type fieldComparator struct {
	strFrom func(v interface{}) (string, bool)
	intFrom func(v interface{}) (int64, bool)
}

func newFieldComparator() *fieldComparator {
	fc := &fieldComparator{}

	fc.strFrom = func(v interface{}) (string, bool) {
		switch t := v.(type) {
		case string:
			return t, true
		case *string:
			if t == nil {
				return "", true
			}
			return *t, true
		case nil:
			return "", true
		default:
			return "", false
		}
	}

	fc.intFrom = func(v interface{}) (int64, bool) {
		switch t := v.(type) {
		case int:
			return int64(t), true
		case int32:
			return int64(t), true
		case int64:
			return t, true
		case float64:
			if t == float64(int64(t)) {
				return int64(t), true
			}
			return 0, false
		default:
			return 0, false
		}
	}

	return fc
}

func (fc *fieldComparator) equalStr(existingVal string, newVal interface{}) bool {
	s, ok := fc.strFrom(newVal)
	if !ok {
		return false
	}
	return existingVal == s
}

func (fc *fieldComparator) equalPtrStr(existing *string, newVal interface{}) bool {
	s, ok := fc.strFrom(newVal)
	if !ok {
		return false
	}
	if existing == nil {
		return s == ""
	}
	return *existing == s
}

func (fc *fieldComparator) equalStatus(existing types.Status, newVal interface{}) bool {
	switch t := newVal.(type) {
	case types.Status:
		return existing == t
	case string:
		return string(existing) == t
	default:
		return false
	}
}

func (fc *fieldComparator) equalIssueType(existing types.IssueType, newVal interface{}) bool {
	switch t := newVal.(type) {
	case types.IssueType:
		return existing == t
	case string:
		return string(existing) == t
	default:
		return false
	}
}

func (fc *fieldComparator) equalPriority(existing int, newVal interface{}) bool {
	newPriority, ok := fc.intFrom(newVal)
	return ok && int64(existing) == newPriority
}

func (fc *fieldComparator) equalBool(existingVal bool, newVal interface{}) bool {
	switch t := newVal.(type) {
	case bool:
		return existingVal == t
	default:
		return false
	}
}

func (fc *fieldComparator) checkFieldChanged(key string, existing *types.Issue, newVal interface{}) bool {
	switch key {
	case "title":
		return !fc.equalStr(existing.Title, newVal)
	case "description":
		return !fc.equalStr(existing.Description, newVal)
	case "status":
		return !fc.equalStatus(existing.Status, newVal)
	case "priority":
		return !fc.equalPriority(existing.Priority, newVal)
	case "issue_type":
		return !fc.equalIssueType(existing.IssueType, newVal)
	case "design":
		return !fc.equalStr(existing.Design, newVal)
	case "acceptance_criteria":
		return !fc.equalStr(existing.AcceptanceCriteria, newVal)
	case "notes":
		return !fc.equalStr(existing.Notes, newVal)
	case "assignee":
		return !fc.equalStr(existing.Assignee, newVal)
	case "external_ref":
		return !fc.equalPtrStr(existing.ExternalRef, newVal)
	case "pinned":
		return !fc.equalBool(existing.Pinned, newVal)
	default:
		return false
	}
}

// RenameImportedIssuePrefixes renames all issues and their references to match the target prefix.
//
// This function handles three ID formats:
//   - Sequential numeric IDs: "old-123" → "new-123"
//   - Hash-based IDs: "old-abc1" → "new-abc1"
//   - Hierarchical IDs: "old-abc1.2.3" → "new-abc1.2.3"
//
// The suffix (everything after "prefix-") is preserved during rename, only the prefix changes.
// This preserves issue identity across prefix renames while maintaining parent-child relationships
// in hierarchical IDs (dots denote subtask nesting, e.g., bd-abc1.2 is child 2 of bd-abc1).
//
// All text references to old IDs in issue fields (title, description, notes, etc.) and
// dependency relationships are updated to use the new IDs.
//
// The knownPrefixes parameter provides prefixes that were detected during the mismatch analysis.
// This is used to correctly identify the prefix for edge cases where ExtractIssuePrefix might
// fail (e.g., "dolt-test-zmermz" where the suffix has no digits and looks like an English word).
func RenameImportedIssuePrefixes(issues []*types.Issue, targetPrefix string, knownPrefixes []string) error {
	// Sort known prefixes by length descending so longer ones are tried first
	// e.g., "dolt-test" should be matched before "dolt"
	sort.Slice(knownPrefixes, func(i, j int) bool {
		return len(knownPrefixes[i]) > len(knownPrefixes[j])
	})

	// Build a mapping of old IDs to new IDs
	idMapping := make(map[string]string)

	for _, issue := range issues {
		oldPrefix := extractPrefixWithKnown(issue.ID, knownPrefixes)
		if oldPrefix == "" {
			return fmt.Errorf("cannot rename issue %s: malformed ID (no hyphen found)", issue.ID)
		}

		if oldPrefix != targetPrefix {
			// Extract the suffix part (everything after prefix-)
			suffix := strings.TrimPrefix(issue.ID, oldPrefix+"-")
			if suffix == "" {
				return fmt.Errorf("cannot rename issue %s: empty suffix", issue.ID)
			}

			// Construct the new ID and validate it's a valid format
			newID := fmt.Sprintf("%s-%s", targetPrefix, suffix)

			// Be permissive during import: if the suffix looks reasonable, allow the rename.
			// The data may contain IDs that don't strictly conform to current validation rules
			// (e.g., agent IDs like gt-gastown-crew-name, or legacy formats).
			// We validate using a hierarchy of checks, accepting if ANY passes:
			// 1. New ID passes full validation (best case)
			// 2. Suffix is valid hash-based format (alphanumeric + dots)
			// 3. Suffix is valid semantic format (type-slug)
			// 4. Suffix is permissively valid (alphanumeric + hyphens + underscores + dots)
			if err := validation.ValidateIssueID(newID); err != nil {
				if !isValidIDSuffix(suffix) && !isValidSemanticSuffix(suffix) && !isPermissiveSuffix(suffix) {
					return fmt.Errorf("cannot rename issue %s: invalid suffix '%s'", issue.ID, suffix)
				}
			}

			idMapping[issue.ID] = newID
		}
	}

	// Now update all issues and their references
	for _, issue := range issues {
		// Update the issue ID itself if it needs renaming
		if newID, ok := idMapping[issue.ID]; ok {
			issue.ID = newID
		}

		// Update all text references in issue fields
		issue.Title = replaceIDReferences(issue.Title, idMapping)
		issue.Description = replaceIDReferences(issue.Description, idMapping)
		if issue.Design != "" {
			issue.Design = replaceIDReferences(issue.Design, idMapping)
		}
		if issue.AcceptanceCriteria != "" {
			issue.AcceptanceCriteria = replaceIDReferences(issue.AcceptanceCriteria, idMapping)
		}
		if issue.Notes != "" {
			issue.Notes = replaceIDReferences(issue.Notes, idMapping)
		}

		// Update dependency references
		for i := range issue.Dependencies {
			if newID, ok := idMapping[issue.Dependencies[i].IssueID]; ok {
				issue.Dependencies[i].IssueID = newID
			}
			if newID, ok := idMapping[issue.Dependencies[i].DependsOnID]; ok {
				issue.Dependencies[i].DependsOnID = newID
			}
		}

		// Update comment references
		for i := range issue.Comments {
			issue.Comments[i].Text = replaceIDReferences(issue.Comments[i].Text, idMapping)
		}
	}

	return nil
}

// replaceIDReferences replaces all old issue ID references with new ones in text
func replaceIDReferences(text string, idMapping map[string]string) string {
	if len(idMapping) == 0 {
		return text
	}

	// Sort old IDs by length descending to handle longer IDs first
	oldIDs := make([]string, 0, len(idMapping))
	for oldID := range idMapping {
		oldIDs = append(oldIDs, oldID)
	}
	sort.Slice(oldIDs, func(i, j int) bool {
		return len(oldIDs[i]) > len(oldIDs[j])
	})

	result := text
	for _, oldID := range oldIDs {
		newID := idMapping[oldID]
		result = replaceBoundaryAware(result, oldID, newID)
	}
	return result
}

// replaceBoundaryAware replaces oldID with newID only when surrounded by boundaries
func replaceBoundaryAware(text, oldID, newID string) string {
	if !strings.Contains(text, oldID) {
		return text
	}

	var result strings.Builder
	i := 0
	for i < len(text) {
		// Find next occurrence
		idx := strings.Index(text[i:], oldID)
		if idx == -1 {
			result.WriteString(text[i:])
			break
		}

		actualIdx := i + idx
		// Check boundary before
		beforeOK := actualIdx == 0 || isBoundary(text[actualIdx-1])
		// Check boundary after
		afterIdx := actualIdx + len(oldID)
		afterOK := afterIdx >= len(text) || isBoundary(text[afterIdx])

		// Write up to this match
		result.WriteString(text[i:actualIdx])

		if beforeOK && afterOK {
			// Valid match - replace
			result.WriteString(newID)
		} else {
			// Invalid match - keep original
			result.WriteString(oldID)
		}

		i = afterIdx
	}

	return result.String()
}

func isBoundary(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == '.' || c == '!' || c == '?' || c == ':' || c == ';' || c == '(' || c == ')' || c == '[' || c == ']' || c == '{' || c == '}'
}

// isValidIDSuffix validates the suffix portion of an issue ID (everything after "prefix-").
//
// Beads supports three ID formats, all of which this function must accept:
//   - Sequential numeric: "123", "999" (legacy format)
//   - Hash-based (base36): "abc1", "6we", "zzz" (current format, content-addressed)
//   - Hierarchical: "abc1.2", "6we.2.3" (subtasks, dot-separated child counters)
//
// The dot separator in hierarchical IDs represents parent-child relationships:
// "bd-abc1.2" means child #2 of parent "bd-abc1". Maximum depth is 3 levels.
//
// Rejected: uppercase letters, hyphens (would be confused with prefix separator),
// and special characters.
func isValidIDSuffix(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || c == '.') {
			return false
		}
	}
	return true
}

// isPermissiveSuffix validates a suffix permissively for import.
// This is a fallback for IDs that don't conform to strict formats but exist in data.
// Accepts: lowercase letters, digits, hyphens, underscores, and dots.
// Rejects: empty strings, strings starting/ending with hyphens, double hyphens.
//
// This handles cases like agent IDs (gt-gastown-crew-name) and other legacy formats
// that were created before strict validation was enforced.
func isPermissiveSuffix(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Don't allow leading/trailing hyphens (would create double hyphens in ID)
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	// Check for double hyphens
	if strings.Contains(s, "--") {
		return false
	}
	// Allow alphanumeric, hyphens, underscores, and dots
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// isValidSemanticSuffix validates a semantic ID suffix (type-slug format).
// Semantic IDs have the format: prefix-type-slug[.child] where:
//   - type: 2-4 lowercase letters (e.g., bug, tsk, feat)
//   - slug: starts with letter, then alphanumeric/underscore chars (may have trailing random or _N suffix)
//   - optional child segments: dot-separated (e.g., .1, .2, .child_name)
//
// Examples of valid semantic suffixes:
//   - "bug-fix_login_timeout" → type=bug, slug=fix_login_timeout
//   - "tsk-add_feature_xyz123" → type=tsk, slug=add_feature_xyz123
//   - "mr-merge_main_to_dev.1" → type=mr, slug=merge_main_to_dev, child=1
func isValidSemanticSuffix(s string) bool {
	if len(s) == 0 {
		return false
	}

	// Split on first hyphen to get type and the rest
	idx := strings.Index(s, "-")
	if idx < 2 || idx > 4 {
		return false // type must be 2-4 chars
	}

	typePart := s[:idx]
	rest := s[idx+1:]

	// Validate type part (2-4 lowercase letters)
	for _, c := range typePart {
		if c < 'a' || c > 'z' {
			return false
		}
	}

	// Split off child segments (after dots)
	slugPart := rest
	if dotIdx := strings.Index(rest, "."); dotIdx > 0 {
		slugPart = rest[:dotIdx]
		// Validate child segments (everything after the first dot)
		childPart := rest[dotIdx+1:]
		for _, c := range childPart {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.') {
				return false
			}
		}
	}

	// Validate slug part (starts with letter, then alphanumeric/underscore)
	if len(slugPart) < 3 {
		return false
	}
	if slugPart[0] < 'a' || slugPart[0] > 'z' {
		return false
	}
	for _, c := range slugPart {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// extractPrefixWithKnown extracts the prefix from an issue ID, using known prefixes if available.
//
// This function first tries to match the issue ID against the provided known prefixes
// (sorted by length descending for correct matching). If a known prefix matches, it's used.
// Otherwise, falls back to utils.ExtractIssuePrefix for standard detection.
//
// This fixes edge cases like "dolt-test-zmermz" where:
//   - ExtractIssuePrefix would incorrectly return "dolt" (suffix "zmermz" has no digits)
//   - But if "dolt-test" is in knownPrefixes, we correctly identify the prefix
func extractPrefixWithKnown(issueID string, knownPrefixes []string) string {
	// Try known prefixes first (already sorted by length descending by caller)
	for _, prefix := range knownPrefixes {
		prefixWithHyphen := prefix + "-"
		if strings.HasPrefix(issueID, prefixWithHyphen) {
			// Verify the suffix is valid before accepting this prefix
			suffix := strings.TrimPrefix(issueID, prefixWithHyphen)
			// Extract base part before any dots for validation
			basePart := suffix
			if dotIdx := strings.Index(suffix, "."); dotIdx > 0 {
				basePart = suffix[:dotIdx]
			}
			// Accept both hash-based suffixes (abc123) and semantic suffixes (bug-slug_name)
			if basePart != "" && (isValidIDSuffix(basePart) || isValidSemanticSuffix(suffix)) {
				return prefix
			}
		}
	}

	// Fall back to standard extraction
	return utils.ExtractIssuePrefix(issueID)
}
