package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	agents "github.com/steveyegge/beads/internal/templates/agents"
)

// AGENTS.md integration markers for beads section
const (
	agentsBeginMarker = "<!-- BEGIN BEADS INTEGRATION -->"
	agentsEndMarker   = "<!-- END BEADS INTEGRATION -->"
)

var (
	errAgentsFileMissing   = errors.New("agents file not found")
	errBeadsSectionMissing = errors.New("beads section missing")
)

type agentsEnv struct {
	agentsPath   string
	stdout       io.Writer
	stderr       io.Writer
	templateData agents.TemplateData
	templateOpts agents.LoadOptions
}

type agentsIntegration struct {
	name         string
	setupCommand string
	readHint     string
}

func defaultAgentsEnv() agentsEnv {
	return agentsEnv{
		agentsPath:   "AGENTS.md",
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		templateData: agents.TemplateData{Prefix: "bd"},
	}
}

// renderBeadsSection renders the beads integration section from the template
// and extracts the content between the BEGIN/END markers.
func renderBeadsSection(env agentsEnv) (string, error) {
	full, err := agents.Render(env.templateData, env.templateOpts)
	if err != nil {
		return "", err
	}

	start := strings.Index(full, agentsBeginMarker)
	end := strings.Index(full, agentsEndMarker)
	if start == -1 || end == -1 || start > end {
		return "", fmt.Errorf("rendered template missing beads integration markers")
	}

	endOfEndMarker := end + len(agentsEndMarker)
	// Include trailing newline if present
	if endOfEndMarker < len(full) && full[endOfEndMarker] == '\n' {
		endOfEndMarker++
	}

	return full[start:endOfEndMarker], nil
}

func installAgents(env agentsEnv, integration agentsIntegration) error {
	_, _ = fmt.Fprintf(env.stdout, "Installing %s integration...\n", integration.name)

	beadsSection, err := renderBeadsSection(env)
	if err != nil {
		_, _ = fmt.Fprintf(env.stderr, "Error: failed to render template: %v\n", err)
		return err
	}

	var currentContent string
	data, err := os.ReadFile(env.agentsPath)
	if err == nil {
		currentContent = string(data)
	} else if !os.IsNotExist(err) {
		_, _ = fmt.Fprintf(env.stderr, "Error: failed to read %s: %v\n", env.agentsPath, err)
		return err
	}

	if currentContent != "" {
		if strings.Contains(currentContent, agentsBeginMarker) {
			newContent := updateBeadsSection(currentContent, beadsSection)
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
		fullContent, renderErr := agents.Render(env.templateData, env.templateOpts)
		if renderErr != nil {
			_, _ = fmt.Fprintf(env.stderr, "Error: failed to render template: %v\n", renderErr)
			return renderErr
		}
		if err := atomicWriteFile(env.agentsPath, []byte(fullContent)); err != nil {
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
	if strings.Contains(content, agentsBeginMarker) {
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
	if !strings.Contains(content, agentsBeginMarker) {
		_, _ = fmt.Fprintln(env.stdout, "No beads section found in AGENTS.md")
		return nil
	}

	newContent := removeBeadsSection(content)
	trimmed := strings.TrimSpace(newContent)
	if trimmed == "" {
		if err := os.Remove(env.agentsPath); err != nil {
			_, _ = fmt.Fprintf(env.stderr, "Error: failed to remove %s: %v\n", env.agentsPath, err)
			return err
		}
		_, _ = fmt.Fprintf(env.stdout, "✓ Removed %s (file was empty after removing beads section)\n", env.agentsPath)
		return nil
	}

	if err := atomicWriteFile(env.agentsPath, []byte(newContent)); err != nil {
		_, _ = fmt.Fprintf(env.stderr, "Error: write %s: %v\n", env.agentsPath, err)
		return err
	}
	_, _ = fmt.Fprintln(env.stdout, "✓ Removed beads section from AGENTS.md")
	return nil
}

// updateBeadsSection replaces the beads section in existing content
func updateBeadsSection(content, beadsSection string) string {
	start := strings.Index(content, agentsBeginMarker)
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
	start := strings.Index(content, agentsBeginMarker)
	end := strings.Index(content, agentsEndMarker)

	if start == -1 || end == -1 || start > end {
		return content
	}

	// Find the next newline after end marker
	endOfEndMarker := end + len(agentsEndMarker)
	nextNewline := strings.Index(content[endOfEndMarker:], "\n")
	if nextNewline != -1 {
		endOfEndMarker += nextNewline + 1
	}

	// Also remove leading blank lines before the section
	trimStart := start
	for trimStart > 0 && (content[trimStart-1] == '\n' || content[trimStart-1] == '\r') {
		trimStart--
	}

	return content[:trimStart] + content[endOfEndMarker:]
}

