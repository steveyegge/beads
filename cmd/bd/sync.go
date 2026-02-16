package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:     "sync",
	GroupID: "sync",
	Short:   "Sync Dolt database (commit, push, pull)",
	Long: `Sync operations for the Dolt database.

By default, commits pending changes and pushes to the Dolt remote.

Commands:
  bd sync              Commit and push to Dolt remote
  bd sync --pull       Pull from Dolt remote
  bd sync --import     Import from JSONL file into database
  bd sync --export     Export database to JSONL file

Examples:
  bd sync                        # Commit + push
  bd sync --pull                 # Pull from remote
  bd sync --import               # Import JSONL → Dolt
  bd sync --export               # Export Dolt → JSONL
  bd sync -m "updated beads"     # Custom commit message`,
	Run: func(cmd *cobra.Command, _ []string) {
		CheckReadonly("sync")
		ctx := rootCtx

		message, _ := cmd.Flags().GetString("message")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		noPush, _ := cmd.Flags().GetBool("no-push")
		importFlag, _ := cmd.Flags().GetBool("import")
		importOnly, _ := cmd.Flags().GetBool("import-only")
		exportFlag, _ := cmd.Flags().GetBool("export")
		flushOnly, _ := cmd.Flags().GetBool("flush-only")
		pullFlag, _ := cmd.Flags().GetBool("pull")

		// Aliases
		if importOnly {
			importFlag = true
		}
		if flushOnly {
			exportFlag = true
		}

		if err := ensureStoreActive(); err != nil {
			FatalError("failed to initialize store: %v", err)
		}

		// Import mode: JSONL → Dolt
		if importFlag {
			jsonlPath := findJSONLPath()
			if jsonlPath == "" {
				FatalError("no JSONL file found (no .beads directory)")
			}
			if dryRun {
				fmt.Println("→ [DRY RUN] Would import from JSONL")
				return
			}
			fmt.Println("→ Importing from JSONL...")
			if err := importFromJSONL(ctx, jsonlPath); err != nil {
				FatalError("importing: %v", err)
			}
			fmt.Println("✓ Import complete")
			return
		}

		// Export mode: Dolt → JSONL
		if exportFlag {
			jsonlPath := findJSONLPath()
			if jsonlPath == "" {
				FatalError("no JSONL file found (no .beads directory)")
			}
			if dryRun {
				fmt.Println("→ [DRY RUN] Would export to JSONL")
				return
			}
			fmt.Println("→ Exporting to JSONL...")
			if err := exportToJSONL(ctx, jsonlPath); err != nil {
				FatalError("exporting: %v", err)
			}
			fmt.Println("✓ Export complete")
			return
		}

		// Pull mode: Pull from Dolt remote
		if pullFlag {
			if dryRun {
				fmt.Println("→ [DRY RUN] Would pull from Dolt remote")
				return
			}
			fmt.Println("→ Pulling from Dolt remote...")
			if err := store.Pull(ctx); err != nil {
				if strings.Contains(err.Error(), "remote") {
					fmt.Println("⚠ No Dolt remote configured")
				} else {
					FatalError("dolt pull failed: %v", err)
				}
			} else {
				fmt.Println("✓ Pulled from Dolt remote")
			}
			return
		}

		// Default: Commit + Push to Dolt remote
		if dryRun {
			fmt.Println("→ [DRY RUN] Would commit and push to Dolt remote")
			return
		}

		// Commit
		fmt.Println("→ Committing to Dolt...")
		commandDidExplicitDoltCommit = true
		commitMsg := message
		if commitMsg == "" {
			commitMsg = "bd sync"
		}
		if err := store.Commit(ctx, commitMsg); err != nil {
			if !strings.Contains(err.Error(), "nothing to commit") {
				FatalError("dolt commit failed: %v", err)
			}
			fmt.Println("  Nothing to commit")
		}

		// Push
		if !noPush {
			fmt.Println("→ Pushing to Dolt remote...")
			if err := store.Push(ctx); err != nil {
				if strings.Contains(err.Error(), "remote") {
					fmt.Println("⚠ No Dolt remote configured, skipping push")
				} else {
					FatalError("dolt push failed: %v", err)
				}
			} else {
				fmt.Println("✓ Pushed to Dolt remote")
			}
		}

		fmt.Println("\n✓ Sync complete")
	},
}

func init() {
	syncCmd.Flags().StringP("message", "m", "", "Commit message (default: 'bd sync')")
	syncCmd.Flags().Bool("dry-run", false, "Preview sync without making changes")
	syncCmd.Flags().Bool("no-push", false, "Skip pushing to remote")
	syncCmd.Flags().Bool("import", false, "Import from JSONL into database")
	syncCmd.Flags().Bool("import-only", false, "Import from JSONL into database (alias for --import)")
	syncCmd.Flags().Bool("export", false, "Export database to JSONL")
	syncCmd.Flags().Bool("flush-only", false, "Export database to JSONL (alias for --export)")
	syncCmd.Flags().Bool("pull", false, "Pull from Dolt remote")
	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.AddCommand(syncCmd)
}
