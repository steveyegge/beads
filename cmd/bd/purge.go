package main

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Permanently remove closed ephemeral beads",
	Long: `Permanently remove closed ephemeral beads from the database.

Ephemeral beads (wisps, convoys, etc.) accumulate rapidly as transient
workflow state. Once closed, they have no value and waste storage space.

This command finds all beads where ephemeral=true AND status=closed,
then hard-deletes them (no tombstones). CASCADE DELETE removes all
child records (events, dependencies, labels) automatically.

PREVIEW (default):
  bd purge                        Show what would be purged

DRY-RUN:
  bd purge --dry-run              Same as preview, explicit flag

ACTUALLY PURGE:
  bd purge --force                Purge all closed ephemeral beads

FILTERING:
  bd purge --pattern "*-wisp-*"   Only purge matching IDs
  bd purge --older-than 7         Only purge items closed >7 days ago`,
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("purge")

		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		pattern, _ := cmd.Flags().GetString("pattern")
		olderThan, _ := cmd.Flags().GetInt("older-than")

		if store == nil {
			if err := ensureStoreActive(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		ctx := rootCtx

		// Build filter: ephemeral=true, status=closed
		ephTrue := true
		closedStatus := types.StatusClosed
		filter := types.IssueFilter{
			Status:    &closedStatus,
			Ephemeral: &ephTrue,
		}
		if olderThan > 0 {
			cutoff := time.Now().AddDate(0, 0, -olderThan)
			filter.ClosedBefore = &cutoff
		}

		// Search for matching beads
		issues, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error searching for ephemeral beads: %v\n", err)
			os.Exit(1)
		}

		// Apply glob pattern filter if specified
		if pattern != "" {
			filtered := make([]*types.Issue, 0, len(issues))
			for _, iss := range issues {
				matched, err := path.Match(pattern, iss.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: invalid pattern %q: %v\n", pattern, err)
					os.Exit(1)
				}
				if matched {
					filtered = append(filtered, iss)
				}
			}
			issues = filtered
		}

		if len(issues) == 0 {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"purged_count":         0,
					"events_removed":       0,
					"dependencies_removed": 0,
					"labels_removed":       0,
					"pattern":              pattern,
					"dry_run":              dryRun || !force,
				})
			} else {
				fmt.Println("No closed ephemeral beads to purge")
			}
			return
		}

		// Collect IDs
		ids := make([]string, len(issues))
		for i, iss := range issues {
			ids[i] = iss.ID
		}

		// Get stats preview via BatchDeleter dry-run
		var statsResult *types.DeleteIssuesResult
		d, hasBatchDeleter := store.(storage.BatchDeleter)
		if hasBatchDeleter {
			statsResult, err = d.DeleteIssues(ctx, ids, true, false, true)
			if err != nil {
				// Non-fatal: we can still purge, just won't have detailed stats
				statsResult = nil
			}
		}

		// Print preview
		if pattern != "" {
			fmt.Printf("\nFound %d closed ephemeral bead(s) matching %q to purge\n", len(ids), pattern)
		} else {
			fmt.Printf("\nFound %d closed ephemeral bead(s) to purge\n", len(ids))
		}

		if statsResult != nil {
			fmt.Printf("\nWould delete: %d beads\n", statsResult.DeletedCount)
			fmt.Printf("Would remove: %d dependencies, %d labels, %d events\n",
				statsResult.DependenciesCount, statsResult.LabelsCount, statsResult.EventsCount)
		}

		// If neither force nor dry-run, show hint and exit
		if !force && !dryRun {
			fmt.Printf("\nRun with --force to purge, or --dry-run to preview without changes.\n")
			os.Exit(1)
		}

		// Dry-run: stop here
		if dryRun {
			if jsonOutput {
				result := map[string]interface{}{
					"purged_count": len(ids),
					"pattern":      pattern,
					"dry_run":      true,
				}
				if statsResult != nil {
					result["events_removed"] = statsResult.EventsCount
					result["dependencies_removed"] = statsResult.DependenciesCount
					result["labels_removed"] = statsResult.LabelsCount
				}
				outputJSON(result)
			} else {
				fmt.Printf("\n(Dry-run mode - no changes made)\n")
			}
			return
		}

		// Force mode: hard delete each issue
		if !jsonOutput {
			fmt.Printf("\nPurging %d closed ephemeral bead(s)...\n", len(ids))
		}

		purgedCount := 0
		for _, id := range ids {
			if err := deleteIssue(ctx, id); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to delete %s: %v\n", id, err)
				continue
			}
			purgedCount++
		}

		if jsonOutput {
			result := map[string]interface{}{
				"purged_count": purgedCount,
				"pattern":      pattern,
				"dry_run":      false,
			}
			if statsResult != nil {
				result["events_removed"] = statsResult.EventsCount
				result["dependencies_removed"] = statsResult.DependenciesCount
				result["labels_removed"] = statsResult.LabelsCount
			}
			outputJSON(result)
		} else {
			msg := fmt.Sprintf("Purged %d beads", purgedCount)
			if statsResult != nil {
				msg = fmt.Sprintf("Purged %d beads (%d events, %d dependencies, %d labels removed)",
					purgedCount, statsResult.EventsCount, statsResult.DependenciesCount, statsResult.LabelsCount)
			}
			fmt.Printf("%s %s\n", ui.RenderPass("\u2713"), msg)
		}
	},
}

func init() {
	purgeCmd.Flags().BoolP("force", "f", false, "Actually purge (required to make changes)")
	purgeCmd.Flags().Bool("dry-run", false, "Preview what would be purged without deleting")
	purgeCmd.Flags().String("pattern", "", "Glob pattern for filtering by issue ID (e.g., \"*-wisp-*\")")
	purgeCmd.Flags().Int("older-than", 0, "Only purge items closed more than N days ago")
	rootCmd.AddCommand(purgeCmd)
}
