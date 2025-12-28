package main

import (
	"github.com/spf13/cobra"
)

var adminCmd = &cobra.Command{
	Use:     "admin",
	GroupID: "advanced",
	Short:   "Administrative commands for database maintenance",
	Long: `Administrative commands for beads database maintenance.

These commands are for advanced users and should be used carefully:
  cleanup   Delete closed issues and prune expired tombstones
  compact   Compact old closed issues to save space
  reset     Remove all beads data and configuration

For routine operations, prefer 'bd doctor --fix'.`,
}

func init() {
	rootCmd.AddCommand(adminCmd)
	adminCmd.AddCommand(cleanupCmd)
	adminCmd.AddCommand(compactCmd)
	adminCmd.AddCommand(resetCmd)
}
