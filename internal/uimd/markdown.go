// Package uimd provides markdown rendering for beads CLI output.
// Keep this separate from internal/ui so non-markdown ui consumers do not
// inherit the glamour/chroma dependency graph.
// This package may depend on internal/ui for terminal policy checks, but
// internal/ui must not import internal/uimd.
package uimd

import (
	"os"

	"charm.land/glamour/v2"
	"github.com/steveyegge/beads/internal/ui"
	"golang.org/x/term"
)

// RenderMarkdown renders markdown text using glamour's auto-detected terminal style.
// Returns the rendered markdown or the original text if rendering fails.
// Word wraps at terminal width (or 80 columns if width can't be detected).
func RenderMarkdown(markdown string) string {
	if ui.IsAgentMode() {
		return markdown
	}
	if !ui.ShouldUseColor() {
		return markdown
	}

	// Cap at 100 chars for readability; wider lines are harder to scan.
	const maxReadableWidth = 100
	wrapWidth := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		wrapWidth = w
	}
	if wrapWidth > maxReadableWidth {
		wrapWidth = maxReadableWidth
	}

	renderer, err := glamour.NewTermRenderer(
		// No style is specified so glamour can auto-detect light/dark terminals.
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
