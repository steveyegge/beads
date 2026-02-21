package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
)

// syncCmd commits pending changes and pushes to Dolt remote.
var syncCmd = &cobra.Command{
	Use:     "sync",
	GroupID: "sync",
	Short:   "Commit pending changes and push to Dolt remote",
	Long: `Commits any pending Dolt changes and pushes to the configured remote.

For Dolt remote operations, use:
  bd dolt push     Push to Dolt remote
  bd dolt pull     Pull from Dolt remote`,
	Run: func(_ *cobra.Command, _ []string) {
		if store == nil {
			return // No database open, nothing to sync
		}
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			return
		}

		// Batch commit: if auto-commit mode is "batch", commit all pending
		// changes as a single logical Dolt commit before sync operations.
		if mode, err := getDoltAutoCommitMode(); err == nil && mode == doltAutoCommitBatch {
			if _, commitErr := store.CommitPending(rootCtx, getActor()); commitErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: batch commit failed: %v\n", commitErr)
			}
		}

		// Mark as explicit commit so PersistentPostRun doesn't double-commit
		commandDidExplicitDoltCommit = true

		// Push to Dolt remote if configured
		if hasRemote, err := store.HasRemote(rootCtx, "origin"); err == nil && hasRemote {
			if err := store.Push(rootCtx); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Dolt push failed: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "Pushed to Dolt git remote\n")
			}
		}
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
