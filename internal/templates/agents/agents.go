// Package agents provides embedded AGENTS.md templates for bd init and setup.
package agents

import (
	_ "embed"
	"strings"
)

//go:embed defaults/agents.md.tmpl
var defaultTemplate string

//go:embed defaults/beads-section.md
var beadsSection string

// EmbeddedDefault returns the full AGENTS.md template content.
func EmbeddedDefault() string {
	return defaultTemplate
}

// EmbeddedBeadsSection returns the beads integration section with markers.
// The returned string is trimmed to match the existing agentsBeadsSection behavior
// (no trailing newline after the end marker).
func EmbeddedBeadsSection() string {
	return strings.TrimRight(beadsSection, "\n") + "\n"
}
