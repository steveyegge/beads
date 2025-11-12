package main

import (
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/cmd/bd/setup"
)

var (
	setupProject bool
	setupCheck   bool
	setupRemove  bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup integration with AI editors",
	Long:  `Setup integration files for AI editors like Claude Code and Cursor.`,
}

var setupCursorCmd = &cobra.Command{
	Use:   "cursor",
	Short: "Setup Cursor IDE integration",
	Long: `Install Beads workflow rules for Cursor IDE.

Creates .cursor/rules/beads.mdc with bd workflow context.
Uses BEGIN/END markers for safe idempotent updates.`,
	Run: func(cmd *cobra.Command, args []string) {
		if setupCheck {
			setup.CheckCursor()
			return
		}

		if setupRemove {
			setup.RemoveCursor()
			return
		}

		setup.InstallCursor()
	},
}

var setupClaudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Setup Claude Code integration",
	Long: `Install Claude Code hooks that auto-inject bd workflow context.

By default, installs hooks globally (~/.claude/settings.json).
Use --project flag to install only for this project.

Hooks call 'bd prime' on SessionStart and PreCompact events to prevent
agents from forgetting bd workflow after context compaction.`,
	Run: func(cmd *cobra.Command, args []string) {
		if setupCheck {
			setup.CheckClaude()
			return
		}

		if setupRemove {
			setup.RemoveClaude(setupProject)
			return
		}

		setup.InstallClaude(setupProject)
	},
}

func init() {
	setupClaudeCmd.Flags().BoolVar(&setupProject, "project", false, "Install for this project only (not globally)")
	setupClaudeCmd.Flags().BoolVar(&setupCheck, "check", false, "Check if Claude integration is installed")
	setupClaudeCmd.Flags().BoolVar(&setupRemove, "remove", false, "Remove bd hooks from Claude settings")

	setupCursorCmd.Flags().BoolVar(&setupCheck, "check", false, "Check if Cursor integration is installed")
	setupCursorCmd.Flags().BoolVar(&setupRemove, "remove", false, "Remove bd rules from Cursor")

	setupCmd.AddCommand(setupClaudeCmd)
	setupCmd.AddCommand(setupCursorCmd)
	rootCmd.AddCommand(setupCmd)
}
