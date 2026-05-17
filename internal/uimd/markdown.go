// Package uimd provides markdown rendering for beads CLI output.
// Kept separate from internal/ui so callers that don't need markdown
// don't pull in the chroma/glamour startup cost (~4.6 MB init).
package uimd

import (
	"os"

	"charm.land/glamour/v2"
	"github.com/steveyegge/beads/internal/ui"
	"golang.org/x/term"
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
