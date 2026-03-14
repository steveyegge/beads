package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/steveyegge/beads/internal/templates/agents"
)

// readFileBytesImpl is used in tests; avoids import cycle.
var readFileBytesImpl = os.ReadFile

// AGENTS.md integration markers for beads section
const (
	agentsBeginMarker = "<!-- BEGIN BEADS INTEGRATION -->"
	agentsEndMarker   = "<!-- END BEADS INTEGRATION -->"
)

var (
	errAgentsFileMissing   = errors.New("agents file not found")
	errBeadsSectionMissing = errors.New("beads section missing")
)

const muxAgentInstructionsURL = "https://mux.coder.com/AGENTS.md"

type agentsEnv struct {
	agentsPath string
	stdout     io.Writer
	stderr     io.Writer
}

type agentsIntegration struct {
	name         string
	setupCommand string
	readHint     string
	docsURL      string
	profile      agents.Profile // "full" or "minimal"; empty defaults to "full"
}

func defaultAgentsEnv() agentsEnv {
	return agentsEnv{
		agentsPath: "AGENTS.md",
		stdout:     os.Stdout,
		stderr:     os.Stderr,
	}
}

// containsBeadsMarker returns true if content contains a BEGIN BEADS INTEGRATION marker
// (either legacy or new format with metadata).
func containsBeadsMarker(content string) bool {
	return strings.Contains(content, "<!-- BEGIN BEADS INTEGRATION")
}

// resolveProfile returns the integration's profile, defaulting to full.
func resolveProfile(integration agentsIntegration) agents.Profile {
	if integration.profile != "" {
		return integration.profile
	}
	return agents.ProfileFull
}

func installAgents(env agentsEnv, integration agentsIntegration) error {
	_, _ = fmt.Fprintf(env.stdout, "Installing %s integration...\n", integration.name)

	profile := resolveProfile(integration)
	beadsSection := agents.RenderSection(profile)

	var currentContent string
	data, err := os.ReadFile(env.agentsPath)
	if err == nil {
		currentContent = string(data)
	} else if !os.IsNotExist(err) {
		_, _ = fmt.Fprintf(env.stderr, "Error: failed to read %s: %v\n", env.agentsPath, err)
		return err
	}

	if currentContent != "" {
		if containsBeadsMarker(currentContent) {
			newContent := updateBeadsSectionWithProfile(currentContent, profile)
			if err := atomicWriteFile(env.agentsPath, []byte(newContent)); err != nil {
				_, _ = fmt.Fprintf(env.stderr, "Error: write %s: %v\n", env.agentsPath, err)
				return err
			}
			_, _ = fmt.Fprintln(env.stdout, "✓ Updated existing beads section in AGENTS.md")
		} else {
			newContent := currentContent + "\n\n" + beadsSection
			if err := atomicWriteFile(env.agentsPath, []byte(newContent)); err != nil {
				_, _ = fmt.Fprintf(env.stderr, "Error: write %s: %v\n", env.agentsPath, err)
				return err
			}
			_, _ = fmt.Fprintln(env.stdout, "✓ Added beads section to existing AGENTS.md")
		}
	} else {
		newContent := createNewAgentsFileWithProfile(profile)
		if err := atomicWriteFile(env.agentsPath, []byte(newContent)); err != nil {
			_, _ = fmt.Fprintf(env.stderr, "Error: write %s: %v\n", env.agentsPath, err)
			return err
		}
		_, _ = fmt.Fprintln(env.stdout, "✓ Created new AGENTS.md with beads integration")
	}

	_, _ = fmt.Fprintf(env.stdout, "\n✓ %s integration installed\n", integration.name)
	_, _ = fmt.Fprintf(env.stdout, "  File: %s\n", env.agentsPath)
	if integration.readHint != "" {
		_, _ = fmt.Fprintf(env.stdout, "\n%s\n", integration.readHint)
	}
	if integration.docsURL != "" {
		_, _ = fmt.Fprintf(env.stdout, "Review guide: %s\n", integration.docsURL)
	}
	_, _ = fmt.Fprintln(env.stdout, "No additional configuration needed!")
	return nil
}

func checkAgents(env agentsEnv, integration agentsIntegration) error {
	data, err := os.ReadFile(env.agentsPath)
	if os.IsNotExist(err) {
		_, _ = fmt.Fprintln(env.stdout, "✗ AGENTS.md not found")
		_, _ = fmt.Fprintf(env.stdout, "  Run: %s\n", integration.setupCommand)
		return errAgentsFileMissing
	} else if err != nil {
		_, _ = fmt.Fprintf(env.stderr, "Error: failed to read %s: %v\n", env.agentsPath, err)
		return err
	}

	content := string(data)
	if containsBeadsMarker(content) {
		_, _ = fmt.Fprintf(env.stdout, "✓ %s integration installed: %s\n", integration.name, env.agentsPath)
		_, _ = fmt.Fprintln(env.stdout, "  Beads section found in AGENTS.md")
		return nil
	}

	_, _ = fmt.Fprintln(env.stdout, "⚠ AGENTS.md exists but no beads section found")
	_, _ = fmt.Fprintf(env.stdout, "  Run: %s (to add beads section)\n", integration.setupCommand)
	return errBeadsSectionMissing
}

func removeAgents(env agentsEnv, integration agentsIntegration) error {
	_, _ = fmt.Fprintf(env.stdout, "Removing %s integration...\n", integration.name)
	data, err := os.ReadFile(env.agentsPath)
	if os.IsNotExist(err) {
		_, _ = fmt.Fprintln(env.stdout, "No AGENTS.md file found")
		return nil
	} else if err != nil {
		_, _ = fmt.Fprintf(env.stderr, "Error: failed to read %s: %v\n", env.agentsPath, err)
		return err
	}

	content := string(data)
	if !containsBeadsMarker(content) {
		_, _ = fmt.Fprintln(env.stdout, "No beads section found in AGENTS.md")
		return nil
	}

	newContent := removeBeadsSection(content)

	if err := atomicWriteFile(env.agentsPath, []byte(newContent)); err != nil {
		_, _ = fmt.Fprintf(env.stderr, "Error: write %s: %v\n", env.agentsPath, err)
		return err
	}
	_, _ = fmt.Fprintln(env.stdout, "✓ Removed beads section from AGENTS.md")
	return nil
}

// updateBeadsSection replaces the beads section in existing content using the full profile.
// Kept for backward compatibility with existing callers and tests.
func updateBeadsSection(content string) string {
	return updateBeadsSectionWithProfile(content, agents.ProfileFull)
}

// updateBeadsSectionWithProfile replaces the beads section with the given profile.
// Handles both legacy markers (exact match) and new-format markers (prefix match with metadata).
func updateBeadsSectionWithProfile(content string, profile agents.Profile) string {
	beadsSection := agents.RenderSection(profile)

	start := findBeginMarker(content)
	end := strings.Index(content, agentsEndMarker)

	if start == -1 || end == -1 || start > end {
		// Markers not found or invalid, append instead
		return content + "\n\n" + beadsSection
	}

	// Replace section between markers (including end marker line)
	endOfEndMarker := end + len(agentsEndMarker)
	// Find the next newline after end marker
	nextNewline := strings.Index(content[endOfEndMarker:], "\n")
	if nextNewline != -1 {
		endOfEndMarker += nextNewline + 1
	}

	return content[:start] + beadsSection + content[endOfEndMarker:]
}

// removeBeadsSection removes the beads section from content
func removeBeadsSection(content string) string {
	start := findBeginMarker(content)
	end := strings.Index(content, agentsEndMarker)

	if start == -1 || end == -1 || start > end {
		return content
	}

	// Remove exactly the managed section, including a single trailing newline
	// immediately after the end marker if present. We intentionally do NOT trim
	// surrounding whitespace or unrelated content to keep user file content intact.
	endOfEndMarker := end + len(agentsEndMarker)
	if endOfEndMarker < len(content) {
		switch content[endOfEndMarker] {
		case '\r':
			endOfEndMarker++
			if endOfEndMarker < len(content) && content[endOfEndMarker] == '\n' {
				endOfEndMarker++
			}
		case '\n':
			endOfEndMarker++
		}
	}

	return content[:start] + content[endOfEndMarker:]
}

// findBeginMarker returns the index of the BEGIN BEADS INTEGRATION marker in content,
// matching both legacy (exact) and new (with metadata) formats via prefix match.
// Returns -1 if not found.
func findBeginMarker(content string) int {
	return strings.Index(content, "<!-- BEGIN BEADS INTEGRATION")
}

// createNewAgentsFile creates a new AGENTS.md with a basic template using the full profile.
func createNewAgentsFile() string {
	return createNewAgentsFileWithProfile(agents.ProfileFull)
}

// createNewAgentsFileWithProfile creates a new AGENTS.md with the given profile.
func createNewAgentsFileWithProfile(profile agents.Profile) string {
	beadsSection := agents.RenderSection(profile)

	return `# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

` + beadsSection + `

## Build & Test

_Add your build and test commands here_

` + "```bash" + `
# Example:
# npm install
# npm test
` + "```" + `

## Architecture Overview

_Add a brief overview of your project architecture_

## Conventions & Patterns

_Add your project-specific conventions here_
`
}
