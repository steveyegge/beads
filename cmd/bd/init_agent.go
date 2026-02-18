package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agents "github.com/steveyegge/beads/internal/templates/agents"
	"github.com/steveyegge/beads/internal/ui"
)

// addAgentsInstructions generates AGENTS.md from the agents template during bd init.
// The template is resolved via the lookup chain (see internal/templates/agents).
func addAgentsInstructions(verbose bool, prefix string, beadsDir string, explicitTemplate string) {
	agentFile := "AGENTS.md"

	data := agents.TemplateData{
		Prefix:       prefix,
		ProjectName:  filepath.Base(mustGetwd()),
		BeadsVersion: Version,
	}

	opts := agents.LoadOptions{
		ExplicitPath: explicitTemplate,
		BeadsDir:     beadsDir,
	}

	if err := writeAgentsFile(agentFile, data, opts, verbose); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to update %s: %v\n", agentFile, err)
		}
	}
}

// writeAgentsFile creates or updates AGENTS.md using the template.
// If the file doesn't exist, it renders the full template.
// If it exists and already has a beads section, it's left alone.
// If it exists without a beads section, the rendered content is appended.
func writeAgentsFile(filename string, data agents.TemplateData, opts agents.LoadOptions, verbose bool) error {
	//nolint:gosec // G304: filename comes from hardcoded caller
	content, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		// File doesn't exist — render full template and write it
		rendered, renderErr := agents.Render(data, opts)
		if renderErr != nil {
			return fmt.Errorf("failed to render template: %w", renderErr)
		}

		// #nosec G306 - markdown needs to be readable
		if err := os.WriteFile(filename, []byte(rendered), 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", filename, err)
		}
		if verbose {
			source := agents.Source(opts)
			fmt.Printf("  %s Created %s (from %s)\n", ui.RenderPass("✓"), filename, source)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to read %s: %w", filename, err)
	}

	// File exists — check if it already has beads or landing-the-plane content
	contentStr := string(content)
	if strings.Contains(contentStr, "BEGIN BEADS INTEGRATION") || strings.Contains(contentStr, "Landing the Plane") {
		if verbose {
			fmt.Printf("  %s already has agent instructions\n", filename)
		}
		return nil
	}

	// Append the rendered template
	rendered, renderErr := agents.Render(data, opts)
	if renderErr != nil {
		return fmt.Errorf("failed to render template: %w", renderErr)
	}

	if !strings.HasSuffix(contentStr, "\n") {
		contentStr += "\n"
	}
	contentStr += "\n" + rendered

	// #nosec G306 - markdown needs to be readable
	if err := os.WriteFile(filename, []byte(contentStr), 0644); err != nil {
		return fmt.Errorf("failed to update %s: %w", filename, err)
	}
	if verbose {
		fmt.Printf("  %s Added agent instructions to %s\n", ui.RenderPass("✓"), filename)
	}
	return nil
}

// mustGetwd returns the current working directory or "." on error.
func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// setupClaudeSettings creates or updates .claude/settings.local.json with onboard instruction
func setupClaudeSettings(verbose bool) error {
	claudeDir := ".claude"
	settingsPath := filepath.Join(claudeDir, "settings.local.json")

	// Create .claude directory if it doesn't exist
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Check if settings.local.json already exists
	var existingSettings map[string]interface{}
	// #nosec G304 - user config path
	if content, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(content, &existingSettings); err != nil {
			// Don't silently overwrite - the user has a file with invalid JSON
			// that likely contains important settings they don't want to lose
			return fmt.Errorf("existing %s contains invalid JSON: %w\nPlease fix the JSON syntax manually before running bd init", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		// File exists but couldn't be read (permissions issue, etc.)
		return fmt.Errorf("failed to read existing %s: %w", settingsPath, err)
	} else {
		// File doesn't exist - create new empty settings
		existingSettings = make(map[string]interface{})
	}

	// Add or update the prompt with onboard instruction
	onboardPrompt := "Before starting any work, run 'bd onboard' to understand the current project state and available issues."

	// Check if prompt already contains onboard instruction
	if promptValue, exists := existingSettings["prompt"]; exists {
		if promptStr, ok := promptValue.(string); ok {
			if strings.Contains(promptStr, "bd onboard") {
				if verbose {
					fmt.Printf("Claude settings already configured with bd onboard instruction\n")
				}
				return nil
			}
			// Update existing prompt to include onboard instruction
			existingSettings["prompt"] = promptStr + "\n\n" + onboardPrompt
		} else {
			// Existing prompt is not a string, replace it
			existingSettings["prompt"] = onboardPrompt
		}
	} else {
		// Add new prompt with onboard instruction
		existingSettings["prompt"] = onboardPrompt
	}

	// Write updated settings
	updatedContent, err := json.MarshalIndent(existingSettings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings JSON: %w", err)
	}

	// #nosec G306 - config file needs 0644
	if err := os.WriteFile(settingsPath, updatedContent, 0644); err != nil {
		return fmt.Errorf("failed to write claude settings: %w", err)
	}

	if verbose {
		fmt.Printf("Configured Claude settings with bd onboard instruction\n")
	}

	return nil
}
