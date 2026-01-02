package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/ui"
)

// landingThePlaneSection is the "landing the plane" instructions for AI agents
// This gets appended to AGENTS.md and @AGENTS.md during bd init
const landingThePlaneSection = `
## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until ` + "`git push`" + ` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ` + "```bash" + `
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ` + "```" + `
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until ` + "`git push`" + ` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
`

// addLandingThePlaneInstructions adds "landing the plane" instructions to AGENTS.md
func addLandingThePlaneInstructions(verbose bool) {
	// File to update (AGENTS.md is the standard comprehensive documentation file)
	agentFile := "AGENTS.md"

	if err := updateAgentFile(agentFile, verbose); err != nil {
		// Non-fatal - continue with other files
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to update %s: %v\n", agentFile, err)
		}
	}
}

// updateAgentFile creates or updates an agent instructions file with landing the plane section
func updateAgentFile(filename string, verbose bool) error {
	// Check if file exists
	//nolint:gosec // G304: filename comes from hardcoded list in addLandingThePlaneInstructions
	content, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		// File doesn't exist - create it with basic structure
		newContent := fmt.Sprintf(`# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `+"`bd onboard`"+` to get started.

## Quick Reference

`+"```bash"+`
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git
`+"```"+`
%s
`, landingThePlaneSection)

		// #nosec G306 - markdown needs to be readable
		if err := os.WriteFile(filename, []byte(newContent), 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", filename, err)
		}
		if verbose {
			fmt.Printf("  %s Created %s with landing-the-plane instructions\n", ui.RenderPass("✓"), filename)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to read %s: %w", filename, err)
	}

	// File exists - check if it already has landing the plane section
	if strings.Contains(string(content), "Landing the Plane") {
		if verbose {
			fmt.Printf("  %s already has landing-the-plane instructions\n", filename)
		}
		return nil
	}

	// Append the landing the plane section
	newContent := string(content)
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += landingThePlaneSection

	// #nosec G306 - markdown needs to be readable
	if err := os.WriteFile(filename, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to update %s: %w", filename, err)
	}
	if verbose {
		fmt.Printf("  %s Added landing-the-plane instructions to %s\n", ui.RenderPass("✓"), filename)
	}
	return nil
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
