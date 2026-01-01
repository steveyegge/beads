// Package main provides the bd-examples CLI tool for running bash example scripts
// from the beads project as a testing/development sandbox.
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// Global flags
var (
	jsonOutput  bool
	verbose     bool
	forceColor  bool
	examplesDir string
)

// Styles for output
var (
	passStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#86b300",
		Dark:  "#c2d94c",
	})
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#f2ae49",
		Dark:  "#ffb454",
	})
	failStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#f07171",
		Dark:  "#f07178",
	})
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#828c99",
		Dark:  "#6c7680",
	})
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#399ee6",
		Dark:  "#59c2ff",
	})
	boldStyle = lipgloss.NewStyle().Bold(true)
)

var rootCmd = &cobra.Command{
	Use:   "bd-examples",
	Short: "Run beads bash examples as a testing sandbox",
	Long: `bd-examples is a CLI tool for running bash example scripts from the beads project.

It provides a safe sandbox environment for testing and development:
  - Dry-run mode by default (no state modifications)
  - Prerequisite checking before execution
  - Timestamped, colored output
  - Isolated sandbox creation for real testing

Examples:
  bd-examples list                    # List all available scripts
  bd-examples check bash-agent        # Check prerequisites
  bd-examples run bash-agent/agent.sh # Dry-run a script
  bd-examples sandbox --issues 10     # Create isolated test environment`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&forceColor, "color", false, "Force color output")
	rootCmd.PersistentFlags().StringVar(&examplesDir, "examples-dir", "", "Path to examples directory (auto-detected if not set)")

	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(sandboxCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, failStyle.Render("Error: "+err.Error()))
		os.Exit(1)
	}
}

// findExamplesDir locates the examples directory
func findExamplesDir() (string, error) {
	if examplesDir != "" {
		if _, err := os.Stat(examplesDir); err != nil {
			return "", fmt.Errorf("specified examples directory not found: %s", examplesDir)
		}
		return examplesDir, nil
	}

	// Try current directory
	if _, err := os.Stat("examples"); err == nil {
		return "examples", nil
	}

	// Try relative to executable
	exe, err := os.Executable()
	if err == nil {
		dir := exe + "/../examples"
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}

	return "", fmt.Errorf("examples directory not found. Use --examples-dir to specify")
}
