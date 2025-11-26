package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/deletions"
)

var (
	deletedSince string
	deletedAll   bool
)

var deletedCmd = &cobra.Command{
	Use:   "deleted [issue-id]",
	Short: "Show deleted issues from the deletions manifest",
	Long: `Show issues that have been deleted and are tracked in the deletions manifest.

This command provides an audit trail of deleted issues, showing:
- Which issues were deleted
- When they were deleted
- Who deleted them
- Optional reason for deletion

Examples:
  bd deleted              # Show recent deletions (last 7 days)
  bd deleted --since=30d  # Show deletions in last 30 days
  bd deleted --all        # Show all tracked deletions
  bd deleted bd-xxx       # Show deletion details for specific issue`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := findBeadsDir()
		if beadsDir == "" {
			fmt.Fprintf(os.Stderr, "Error: not in a beads repository (no .beads directory found)\n")
			os.Exit(1)
		}

		deletionsPath := deletions.DefaultPath(beadsDir)
		result, err := deletions.LoadDeletions(deletionsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading deletions: %v\n", err)
			os.Exit(1)
		}

		// Print any warnings
		for _, w := range result.Warnings {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
		}

		// If looking for specific issue
		if len(args) == 1 {
			issueID := args[0]
			displaySingleDeletion(result.Records, issueID)
			return
		}

		// Filter by time range
		var cutoff time.Time
		if !deletedAll {
			duration, err := parseDuration(deletedSince)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid --since value '%s': %v\n", deletedSince, err)
				os.Exit(1)
			}
			cutoff = time.Now().Add(-duration)
		}

		// Collect and sort records
		var records []deletions.DeletionRecord
		for _, r := range result.Records {
			if deletedAll || r.Timestamp.After(cutoff) {
				records = append(records, r)
			}
		}

		// Sort by timestamp descending (most recent first)
		sort.Slice(records, func(i, j int) bool {
			return records[i].Timestamp.After(records[j].Timestamp)
		})

		if jsonOutput {
			outputJSON(records)
			return
		}

		displayDeletions(records, deletedSince, deletedAll)
	},
}

func displaySingleDeletion(records map[string]deletions.DeletionRecord, issueID string) {
	record, found := records[issueID]
	if !found {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"found": false,
				"id":    issueID,
			})
			return
		}
		fmt.Printf("Issue %s not found in deletions manifest\n", issueID)
		fmt.Println("(This could mean the issue was never deleted, or the deletion record was pruned)")
		return
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"found":  true,
			"record": record,
		})
		return
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("\n%s Deletion record for %s:\n\n", cyan("🗑️"), issueID)
	fmt.Printf("  ID:        %s\n", record.ID)
	fmt.Printf("  Deleted:   %s\n", record.Timestamp.Local().Format("2006-01-02 15:04:05"))
	fmt.Printf("  By:        %s\n", record.Actor)
	if record.Reason != "" {
		fmt.Printf("  Reason:    %s\n", record.Reason)
	}
	fmt.Println()
}

func displayDeletions(records []deletions.DeletionRecord, since string, all bool) {
	if len(records) == 0 {
		green := color.New(color.FgGreen).SprintFunc()
		if all {
			fmt.Printf("\n%s No deletions tracked in manifest\n\n", green("✨"))
		} else {
			fmt.Printf("\n%s No deletions in the last %s\n\n", green("✨"), since)
		}
		return
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	if all {
		fmt.Printf("\n%s All tracked deletions (%d total):\n\n", cyan("🗑️"), len(records))
	} else {
		fmt.Printf("\n%s Deletions in the last %s (%d total):\n\n", cyan("🗑️"), since, len(records))
	}

	for _, r := range records {
		ts := r.Timestamp.Local().Format("2006-01-02 15:04")
		reason := ""
		if r.Reason != "" {
			reason = "  " + r.Reason
		}
		fmt.Printf("  %-12s  %s  %-12s%s\n", r.ID, ts, r.Actor, reason)
	}
	fmt.Println()
}

// parseDuration parses a duration string like "7d", "30d", "2w"
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 7 * 24 * time.Hour, nil // default 7 days
	}

	// Check for special suffixes
	if strings.HasSuffix(s, "d") {
		days := s[:len(s)-1]
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return 0, fmt.Errorf("invalid days format: %s", s)
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}

	if strings.HasSuffix(s, "w") {
		weeks := s[:len(s)-1]
		var w int
		if _, err := fmt.Sscanf(weeks, "%d", &w); err != nil {
			return 0, fmt.Errorf("invalid weeks format: %s", s)
		}
		return time.Duration(w) * 7 * 24 * time.Hour, nil
	}

	// Try standard Go duration
	return time.ParseDuration(s)
}

func init() {
	deletedCmd.Flags().StringVar(&deletedSince, "since", "7d", "Show deletions within this time range (e.g., 7d, 30d, 2w)")
	deletedCmd.Flags().BoolVar(&deletedAll, "all", false, "Show all tracked deletions")
	deletedCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON format")
	rootCmd.AddCommand(deletedCmd)
}
