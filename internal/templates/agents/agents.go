// Package agents provides template loading for AGENTS.md.
//
// The AGENTS.md file is generated during bd init and bd setup to provide
// AI coding agents with project instructions. Instead of hardcoded Go
// string constants, this package loads from an editable file.
//
// Lookup chain (highest to lowest priority):
//  1. Explicit path (from --agents-template flag or init.agents-template config)
//  2. .beads/templates/agents.md.tmpl (project-level, version-controlled)
//  3. ~/.config/bd/templates/agents.md.tmpl (user-level)
//  4. /etc/bd/templates/agents.md.tmpl (system-level)
//  5. Embedded default (fallback)
package agents

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/debug"
)

//go:embed defaults/agents.md.tmpl
var defaultTemplate embed.FS

const templateFile = "agents.md.tmpl"

// LoadOptions configures template resolution.
type LoadOptions struct {
	// ExplicitPath overrides the lookup chain entirely.
	// Set from --agents-template flag or init.agents-template config.
	ExplicitPath string

	// BeadsDir is the project .beads/ directory path.
	// Used for project-level template lookup (.beads/templates/).
	BeadsDir string
}

// Load resolves and returns the AGENTS.md content from the lookup chain.
func Load(opts LoadOptions) (string, error) {
	content, source, err := resolve(opts)
	if err != nil {
		return "", fmt.Errorf("failed to resolve template: %w", err)
	}

	debug.Logf("template: loaded %s from %s", templateFile, source)
	return string(content), nil
}

// Source returns the path or description of where the template would be loaded from,
// without reading it. Useful for diagnostics.
func Source(opts LoadOptions) string {
	_, source, err := resolve(opts)
	if err != nil {
		return "not found"
	}
	return source
}

// resolve walks the lookup chain and returns template content and its source.
func resolve(opts LoadOptions) ([]byte, string, error) {
	// 1. Explicit path (highest priority)
	if opts.ExplicitPath != "" {
		content, err := os.ReadFile(opts.ExplicitPath) //nolint:gosec // G304: user-specified template path
		if err != nil {
			return nil, "", fmt.Errorf("explicit template path %s: %w", opts.ExplicitPath, err)
		}
		return content, opts.ExplicitPath, nil
	}

	// 2. Project-level: .beads/templates/agents.md.tmpl
	if opts.BeadsDir != "" {
		path := filepath.Join(opts.BeadsDir, "templates", templateFile)
		if content, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: project template path
			return content, path, nil
		}
	}

	// 3. User-level: ~/.config/bd/templates/agents.md.tmpl
	if configDir, err := os.UserConfigDir(); err == nil {
		path := filepath.Join(configDir, "bd", "templates", templateFile)
		if content, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: user config template path
			return content, path, nil
		}
	}

	// 4. System-level: /etc/bd/templates/agents.md.tmpl
	{
		path := filepath.Join("/etc", "bd", "templates", templateFile)
		if content, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: system template path
			return content, path, nil
		}
	}

	// 5. Embedded default (fallback)
	content, err := defaultTemplate.ReadFile("defaults/" + templateFile)
	if err != nil {
		return nil, "", fmt.Errorf("embedded default template not found: %w", err)
	}

	return content, "embedded:agents.md.tmpl", nil
}
