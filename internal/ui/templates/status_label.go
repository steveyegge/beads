package templates

import (
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

var statusLabelMap = map[string]string{
	string(types.StatusOpen):       "Ready",
	string(types.StatusInProgress): "In Progress",
	string(types.StatusBlocked):    "Blocked",
	string(types.StatusClosed):     "Done",
}

// StatusLabel returns the human-readable label for a status value.
func StatusLabel(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized == "" {
		return ""
	}
	if label, ok := statusLabelMap[normalized]; ok {
		return label
	}
	return status
}
