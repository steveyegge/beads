package main

import (
	"github.com/spf13/cobra"
)

// Hidden aliases for backwards compatibility.
// These commands forward to their admin subcommand equivalents.
// They are hidden from help output but still work for scripts/muscle memory.

var cleanupAliasCmd = &cobra.Command{
	Use:        "cleanup",
	Hidden:     true,
	Deprecated: "use 'bd admin cleanup' instead (will be removed in v1.0.0)",
	Short:      "Alias for 'bd admin cleanup' (deprecated)",
	Long:       cleanupCmd.Long,
	Run: func(cmd *cobra.Command, args []string) {
		cleanupCmd.Run(cmd, args)
	},
}

// NOTE: The top-level "compact" command slot is now used by the Dolt commit
// compaction command (compact_dolt.go). The old issue-level compaction lives
// at "bd admin compact".

var resetAliasCmd = &cobra.Command{
	Use:        "reset",
	Hidden:     true,
	Deprecated: "use 'bd admin reset' instead (will be removed in v1.0.0)",
	Short:      "Alias for 'bd admin reset' (deprecated)",
	Long:       resetCmd.Long,
	Run: func(cmd *cobra.Command, args []string) {
		resetCmd.Run(cmd, args)
	},
}

func init() {
	// Copy flags from original commands to aliases, binding to same global variables
	// This ensures that when the alias command runs, the global flag variables are set correctly

	// Cleanup alias flags - these read from cmd.Flags() in the Run function
	cleanupAliasCmd.Flags().BoolP("force", "f", false, "Actually delete (without this flag, shows error)")
	cleanupAliasCmd.Flags().Bool("dry-run", false, "Preview what would be deleted without making changes")
	cleanupAliasCmd.Flags().Bool("cascade", false, "Recursively delete all dependent issues")
	cleanupAliasCmd.Flags().Int("older-than", 0, "Only delete issues closed more than N days ago (0 = all closed issues)")
	cleanupAliasCmd.Flags().Bool("ephemeral", false, "Only delete closed wisps (transient molecules)")

	// NOTE: compactAliasCmd removed — "compact" slot now used by compact_dolt.go

	// Reset alias flags - these read from cmd.Flags() in the Run function
	resetAliasCmd.Flags().Bool("force", false, "Actually perform the reset (required)")

	// Register hidden aliases on root command
	rootCmd.AddCommand(cleanupAliasCmd)
	// NOTE: compactAliasCmd removed — "compact" slot now used by compact_dolt.go
	rootCmd.AddCommand(resetAliasCmd)
}
