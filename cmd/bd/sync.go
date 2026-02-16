package main

import (
	"github.com/spf13/cobra"
)

// syncCmd is a no-op kept for backward compatibility.
// Models and scripts have "bd sync" in muscle memory from the JSONL era.
// With Dolt-native storage, writes are persisted immediately — there is nothing to sync.
var syncCmd = &cobra.Command{
	Use:     "sync",
	GroupID: "sync",
	Short:   "No-op (Dolt persists writes immediately)",
	Long: `With Dolt-native storage, all writes are persisted immediately.
There is nothing to sync. This command exists for backward compatibility
and returns instantly.

For Dolt remote operations, use:
  bd dolt push     Push to Dolt remote
  bd dolt pull     Pull from Dolt remote

For data interchange:
  bd export        Export database to JSONL
  bd import        Import JSONL into database`,
	Run: func(_ *cobra.Command, _ []string) {
		// Silent no-op. Dolt persists writes immediately.
	},
}

func init() {
	// Keep all legacy flags so old invocations don't error out.
	syncCmd.Flags().StringP("message", "m", "", "Deprecated: no-op")
	syncCmd.Flags().Bool("dry-run", false, "Deprecated: no-op")
	syncCmd.Flags().Bool("no-push", false, "Deprecated: no-op")
	syncCmd.Flags().Bool("import", false, "Deprecated: use 'bd import' instead")
	syncCmd.Flags().Bool("import-only", false, "Deprecated: use 'bd import' instead")
	syncCmd.Flags().Bool("export", false, "Deprecated: use 'bd export' instead")
	syncCmd.Flags().Bool("flush-only", false, "Deprecated: no-op")
	syncCmd.Flags().Bool("pull", false, "Deprecated: use 'bd dolt pull' instead")
	syncCmd.Flags().Bool("no-git-history", false, "Deprecated: no-op")
	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.AddCommand(syncCmd)
}
