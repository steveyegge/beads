package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	agents "github.com/steveyegge/beads/internal/templates/agents"
)

var agentsTemplateCmd = &cobra.Command{
	Use:     "agents-template",
	GroupID: "setup",
	Short:   "Manage the AGENTS.md template",
	Long: `Manage the AGENTS.md template used by bd init.

Use these commands to inspect, scaffold, and compare AGENTS.md templates
before running bd init. Works for both humans and AI agents.

Workflow:
  bd agents-template show          # inspect the current template
  bd agents-template edit          # scaffold + open in $EDITOR
  bd init                          # picks up your customizations`,
}

var agentsTemplateShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the resolved AGENTS.md template to stdout",
	Long: `Print the AGENTS.md template that bd init would use.

Resolves via the lookup chain:
  1. .beads/templates/agents.md.tmpl (project-level)
  2. ~/.config/bd/templates/agents.md.tmpl (user-level)
  3. /etc/bd/templates/agents.md.tmpl (system-level)
  4. Embedded default (fallback)

Use --source to print only the source path instead of the content.`,
	RunE: runAgentsTemplateShow,
}

var agentsTemplateInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold an editable copy of the default template",
	Long: `Copy the embedded default AGENTS.md template to .beads/templates/agents.md.tmpl
for project-level customization.

After scaffolding, edit the file and run bd init — your customizations will
be picked up automatically via the lookup chain.

Skips if the file already exists (use --force to overwrite).`,
	RunE: runAgentsTemplateInit,
}

var agentsTemplateEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the project template in $EDITOR (scaffolding if needed)",
	Long: `Open the project-level AGENTS.md template for editing.

If .beads/templates/agents.md.tmpl does not exist yet, scaffolds it from the
embedded default first (same as bd agents-template init), then opens it in
your editor.

The editor is chosen from $VISUAL, then $EDITOR, falling back to "vi".`,
	RunE: runAgentsTemplateEdit,
}

var agentsTemplateDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show differences between current template and embedded default",
	Long: `Compare the resolved AGENTS.md template against the embedded default.

If no project-level or user-level template exists, reports that the
embedded default is in use. Otherwise shows a unified diff.`,
	RunE: runAgentsTemplateDiff,
}

func init() {
	agentsTemplateShowCmd.Flags().Bool("source", false, "Print only the template source path")

	agentsTemplateInitCmd.Flags().Bool("force", false, "Overwrite existing template file")

	agentsTemplateCmd.AddCommand(agentsTemplateShowCmd)
	agentsTemplateCmd.AddCommand(agentsTemplateInitCmd)
	agentsTemplateCmd.AddCommand(agentsTemplateEditCmd)
	agentsTemplateCmd.AddCommand(agentsTemplateDiffCmd)
	rootCmd.AddCommand(agentsTemplateCmd)
}

func buildLoadOptions() agents.LoadOptions {
	beadsDir := beads.FindBeadsDir()
	return agents.LoadOptions{BeadsDir: beadsDir}
}

func runAgentsTemplateShow(cmd *cobra.Command, _ []string) error {
	sourceOnly, _ := cmd.Flags().GetBool("source")
	opts := buildLoadOptions()

	if sourceOnly {
		fmt.Println(agents.Source(opts))
		return nil
	}

	content, err := agents.Load(opts)
	if err != nil {
		return fmt.Errorf("failed to load template: %w", err)
	}
	fmt.Print(content)
	return nil
}

func runAgentsTemplateInit(cmd *cobra.Command, _ []string) error {
	force, _ := cmd.Flags().GetBool("force")
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return fmt.Errorf("not in a beads workspace (no .beads directory found)\nRun 'bd init' first to create a workspace")
	}

	tmplDir := filepath.Join(beadsDir, "templates")
	tmplPath := filepath.Join(tmplDir, "agents.md.tmpl")

	// Check if file already exists
	if !force {
		if _, err := os.Stat(tmplPath); err == nil {
			fmt.Fprintf(os.Stderr, "Template already exists: %s\n", tmplPath)
			fmt.Fprintf(os.Stderr, "Use --force to overwrite.\n")
			return nil
		}
	}

	// Get embedded default
	content, err := agents.EmbeddedContent()
	if err != nil {
		return fmt.Errorf("failed to read embedded template: %w", err)
	}

	// Create templates directory
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		return fmt.Errorf("failed to create templates directory: %w", err)
	}

	// Write template
	if err := os.WriteFile(tmplPath, []byte(content), 0644); err != nil { //nolint:gosec // G306: template needs to be readable
		return fmt.Errorf("failed to write template: %w", err)
	}

	fmt.Printf("Created %s\n", tmplPath)
	fmt.Println("Edit this file, then run 'bd init' to use your customizations.")
	return nil
}

func runAgentsTemplateEdit(_ *cobra.Command, _ []string) error {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return fmt.Errorf("not in a beads workspace (no .beads directory found)\nRun 'bd init' first to create a workspace")
	}

	tmplDir := filepath.Join(beadsDir, "templates")
	tmplPath := filepath.Join(tmplDir, "agents.md.tmpl")

	// Scaffold if the file doesn't exist yet.
	if _, err := os.Stat(tmplPath); os.IsNotExist(err) {
		content, err := agents.EmbeddedContent()
		if err != nil {
			return fmt.Errorf("failed to read embedded template: %w", err)
		}
		if err := os.MkdirAll(tmplDir, 0750); err != nil {
			return fmt.Errorf("failed to create templates directory: %w", err)
		}
		if err := os.WriteFile(tmplPath, []byte(content), 0644); err != nil { //nolint:gosec // G306: template needs to be readable
			return fmt.Errorf("failed to write template: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Scaffolded %s\n", tmplPath)
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, tmplPath) //nolint:gosec // G204: editor is from user's env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runAgentsTemplateDiff(_ *cobra.Command, _ []string) error {
	opts := buildLoadOptions()

	current, err := agents.Load(opts)
	if err != nil {
		return fmt.Errorf("failed to load current template: %w", err)
	}

	embedded, err := agents.EmbeddedContent()
	if err != nil {
		return fmt.Errorf("failed to load embedded default: %w", err)
	}

	if current == embedded {
		source := agents.Source(opts)
		fmt.Printf("No customizations (using %s)\n", source)
		return nil
	}

	source := agents.Source(opts)
	printUnifiedDiff("embedded (default)", source, embedded, current)
	return nil
}

// printUnifiedDiff prints a simple unified diff between two strings.
func printUnifiedDiff(nameA, nameB, a, b string) {
	linesA := strings.Split(a, "\n")
	linesB := strings.Split(b, "\n")

	fmt.Printf("--- %s\n", nameA)
	fmt.Printf("+++ %s\n", nameB)

	// Simple line-by-line diff: find contiguous blocks of changes.
	// This is not a full Myers diff but sufficient for template comparison.
	i, j := 0, 0
	for i < len(linesA) || j < len(linesB) {
		if i < len(linesA) && j < len(linesB) && linesA[i] == linesB[j] {
			fmt.Printf(" %s\n", linesA[i])
			i++
			j++
		} else {
			// Find the end of the differing block
			// Look ahead for a resync point
			synced := false
			for lookA := i; lookA < len(linesA) && lookA < i+10; lookA++ {
				for lookB := j; lookB < len(linesB) && lookB < j+10; lookB++ {
					if linesA[lookA] == linesB[lookB] {
						// Print removals and additions up to sync point
						for i < lookA {
							fmt.Printf("-%s\n", linesA[i])
							i++
						}
						for j < lookB {
							fmt.Printf("+%s\n", linesB[j])
							j++
						}
						synced = true
						break
					}
				}
				if synced {
					break
				}
			}
			if !synced {
				// No sync found in lookahead — emit one line from each
				if i < len(linesA) {
					fmt.Printf("-%s\n", linesA[i])
					i++
				}
				if j < len(linesB) {
					fmt.Printf("+%s\n", linesB[j])
					j++
				}
			}
		}
	}
}
