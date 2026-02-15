package hooks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// shellUnsafe matches characters that could enable shell injection.
// We strip these from variable values before expansion.
var shellUnsafe = regexp.MustCompile(`[;&|` + "`" + `$(){}!<>\\'\"\n\r]`)

// SanitizeValue removes shell-unsafe characters from a string.
// This prevents injection when variable values are substituted into commands.
func SanitizeValue(s string) string {
	return shellUnsafe.ReplaceAllString(s, "")
}

// HookVars holds the variables available for expansion in hook commands.
type HookVars struct {
	BeadID       string // ${BEAD_ID}
	BeadRig      string // ${BEAD_RIG}
	BeadTitle    string // ${BEAD_TITLE}
	BeadPriority string // ${BEAD_PRIORITY}
	BeadType     string // ${BEAD_TYPE}
	BeadEvent    string // ${BEAD_EVENT} (create, update, close, comment)
	BeadStatus   string // ${BEAD_STATUS}

	// Update-specific
	BeadField    string // ${BEAD_FIELD} (which field changed)
	BeadOldValue string // ${BEAD_OLD_VALUE}
	BeadNewValue string // ${BEAD_NEW_VALUE}

	// Close-specific
	BeadReason string // ${BEAD_REASON}

	// Comment-specific
	CommentAuthor string // ${COMMENT_AUTHOR}
	CommentBody   string // ${COMMENT_BODY}
}

// VarsFromIssue builds HookVars from an issue and event name.
func VarsFromIssue(issue *types.Issue, event string) *HookVars {
	if issue == nil {
		return &HookVars{BeadEvent: event}
	}

	// Extract rig from issue ID (everything before the last dash + hash)
	rig := extractRig(issue.ID)

	return &HookVars{
		BeadID:       issue.ID,
		BeadRig:      rig,
		BeadTitle:    issue.Title,
		BeadPriority: priorityString(issue.Priority),
		BeadType:     string(issue.IssueType),
		BeadEvent:    event,
		BeadStatus:   string(issue.Status),
		BeadReason:   issue.CloseReason,
	}
}

// ExpandCommand substitutes ${BEAD_*} variables in a command string.
// All values are sanitized before substitution to prevent shell injection.
func ExpandCommand(command string, vars *HookVars) string {
	if vars == nil {
		return command
	}

	replacements := []struct {
		key string
		val string
	}{
		{"${BEAD_ID}", SanitizeValue(vars.BeadID)},
		{"${BEAD_RIG}", SanitizeValue(vars.BeadRig)},
		{"${BEAD_TITLE}", SanitizeValue(vars.BeadTitle)},
		{"${BEAD_PRIORITY}", SanitizeValue(vars.BeadPriority)},
		{"${BEAD_TYPE}", SanitizeValue(vars.BeadType)},
		{"${BEAD_EVENT}", SanitizeValue(vars.BeadEvent)},
		{"${BEAD_STATUS}", SanitizeValue(vars.BeadStatus)},
		{"${BEAD_FIELD}", SanitizeValue(vars.BeadField)},
		{"${BEAD_OLD_VALUE}", SanitizeValue(vars.BeadOldValue)},
		{"${BEAD_NEW_VALUE}", SanitizeValue(vars.BeadNewValue)},
		{"${BEAD_REASON}", SanitizeValue(vars.BeadReason)},
		{"${COMMENT_AUTHOR}", SanitizeValue(vars.CommentAuthor)},
		{"${COMMENT_BODY}", SanitizeValue(vars.CommentBody)},
	}

	result := command
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.key, r.val)
	}
	return result
}

// extractRig extracts the rig name from a bead ID.
// "aegis-abc" → "aegis", "bd-xyz" → "bd"
func extractRig(id string) string {
	// Find the last dash followed by the hash portion
	idx := strings.LastIndex(id, "-")
	if idx <= 0 {
		return ""
	}
	return id[:idx]
}

// priorityString converts a priority int to "P0", "P1", etc.
func priorityString(p int) string {
	return fmt.Sprintf("P%d", p)
}
