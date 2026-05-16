// Package uimd provides markdown rendering for beads CLI output.
// It is a separate package from internal/ui so that glamour and chroma are
// not pulled into the import graph of callers that never render markdown
// (bd list, bd version, bd ready, etc.).
package uimd

import (
	"os"

	"charm.land/glamour/v2"
	"golang.org/x/term"

	"github.com/steveyegge/beads/internal/ui"
)

// RenderMarkdown renders markdown text using glamour with beads theme colors.
// Returns the rendered markdown or the original text if rendering fails.
// Word wraps at terminal width (or 80 columns if width can't be detected).
func RenderMarkdown(markdown string) string {
	if ui.IsAgentMode() {
		return markdown
	}
	if !ui.ShouldUseColor() {
		return markdown
	}

	const maxReadableWidth = 100
	wrapWidth := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		wrapWidth = w
	}
	if wrapWidth > maxReadableWidth {
		wrapWidth = maxReadableWidth
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return markdown
	}

	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown
	}

	return rendered
}
