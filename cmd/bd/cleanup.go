package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Delete closed issues and prune expired tombstones",
	Long: `Delete closed issues and prune expired tombstones to reduce database size.

This command:
1. Converts closed issues to tombstones (soft delete)
2. Prunes expired tombstones (older than 30 days) from issues.jsonl

It does NOT remove temporary files - use 'bd clean' for that.

By default, deletes ALL closed issues. Use --older-than to only delete
issues closed before a certain date.

EXAMPLES:
Delete all closed issues and prune tombstones:
  bd cleanup --force

Delete issues closed more than 30 days ago:
  bd cleanup --older-than 30 --force

Preview what would be deleted/pruned:
  bd cleanup --dry-run
  bd cleanup --older-than 90 --dry-run

SAFETY:
- Requires --force flag to actually delete (unless --dry-run)
- Supports --cascade to delete dependents
- Shows preview of what will be deleted
- Use --json for programmatic output

SEE ALSO:
  bd clean      Remove temporary git merge artifacts
  bd compact    Run compaction on issues`,
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		cascade, _ := cmd.Flags().GetBool("cascade")
		olderThanDays, _ := cmd.Flags().GetInt("older-than")

		// Ensure we have storage
		if daemonClient != nil {
			if err := ensureDirectMode("daemon does not support delete command"); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else if store == nil {
			if err := ensureStoreActive(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		ctx := rootCtx

		// Build filter for closed issues
		statusClosed := types.StatusClosed
		filter := types.IssueFilter{
			Status: &statusClosed,
		}

		// Add age filter if specified
		if olderThanDays > 0 {
			cutoffTime := time.Now().AddDate(0, 0, -olderThanDays)
			filter.ClosedBefore = &cutoffTime
		}

		// Get all closed issues matching filter
		closedIssues, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing issues: %v\n", err)
			os.Exit(1)
		}

		if len(closedIssues) == 0 {
			if jsonOutput {
				result := map[string]interface{}{
					"deleted_count": 0,
					"message":       "No closed issues to delete",
				}
				if olderThanDays > 0 {
					result["filter"] = fmt.Sprintf("older than %d days", olderThanDays)
				}
				output, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(output))
			} else {
				msg := "No closed issues to delete"
				if olderThanDays > 0 {
					msg = fmt.Sprintf("No closed issues older than %d days to delete", olderThanDays)
				}
				fmt.Println(msg)
			}
			return
		}

		// Extract IDs
		issueIDs := make([]string, len(closedIssues))
		for i, issue := range closedIssues {
			issueIDs[i] = issue.ID
		}

		// Show preview
		if !force && !dryRun {
			fmt.Fprintf(os.Stderr, "Would delete %d closed issue(s). Use --force to confirm or --dry-run to preview.\n", len(issueIDs))
			os.Exit(1)
		}

		if !jsonOutput {
			if olderThanDays > 0 {
				fmt.Printf("Found %d closed issue(s) older than %d days\n", len(closedIssues), olderThanDays)
			} else {
				fmt.Printf("Found %d closed issue(s)\n", len(closedIssues))
			}
			if dryRun {
				fmt.Println(color.YellowString("DRY RUN - no changes will be made"))
			}
			fmt.Println()
		}

		// Use the existing batch deletion logic
		deleteBatch(cmd, issueIDs, force, dryRun, cascade, jsonOutput, "cleanup")

		// Also prune expired tombstones (bd-08ea)
		// This runs after closed issues are converted to tombstones, cleaning up old ones
		if dryRun {
			// Preview what tombstones would be pruned
			tombstoneResult, err := previewPruneTombstones()
			if err != nil {
				if !jsonOutput {
					fmt.Fprintf(os.Stderr, "Warning: failed to check tombstones: %v\n", err)
				}
			} else if tombstoneResult != nil && tombstoneResult.PrunedCount > 0 {
				if !jsonOutput {
					fmt.Printf("\nExpired tombstones that would be pruned: %d (older than %d days)\n",
						tombstoneResult.PrunedCount, tombstoneResult.TTLDays)
				}
			}
		} else if force {
			// Actually prune expired tombstones
			tombstoneResult, err := pruneExpiredTombstones()
			if err != nil {
				if !jsonOutput {
					fmt.Fprintf(os.Stderr, "Warning: failed to prune expired tombstones: %v\n", err)
				}
			} else if tombstoneResult != nil && tombstoneResult.PrunedCount > 0 {
				if !jsonOutput {
					green := color.New(color.FgGreen).SprintFunc()
					fmt.Printf("\n%s Pruned %d expired tombstone(s) (older than %d days)\n",
						green("✓"), tombstoneResult.PrunedCount, tombstoneResult.TTLDays)
				}
			}
		}
	},
}

func init() {
	cleanupCmd.Flags().BoolP("force", "f", false, "Actually delete (without this flag, shows error)")
	cleanupCmd.Flags().Bool("dry-run", false, "Preview what would be deleted without making changes")
	cleanupCmd.Flags().Bool("cascade", false, "Recursively delete all dependent issues")
	cleanupCmd.Flags().Int("older-than", 0, "Only delete issues closed more than N days ago (0 = all closed issues)")
	rootCmd.AddCommand(cleanupCmd)
}
