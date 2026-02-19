package validation

import (
	"regexp"
	"strings"
)

// testIssueTitlePattern matches common test issue title prefixes.
var testIssueTitlePattern = regexp.MustCompile(`^(test|benchmark|sample|tmp|temp|debug|dummy)[-_\s]`)

// IsTestIssueTitle returns true when a title appears to be test/demo data.
// Shared across command paths to keep heuristics consistent.
func IsTestIssueTitle(title string) bool {
	return testIssueTitlePattern.MatchString(strings.ToLower(strings.TrimSpace(title)))
}
