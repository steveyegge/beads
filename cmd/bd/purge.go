package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// PurgeResult holds statistics from a purge operation.
type PurgeResult struct {
	PurgedCount       int      `json:"purged_count"`
	DependenciesCount int      `json:"dependencies_count"`
	LabelsCount       int      `json:"labels_count"`
	EventsCount       int      `json:"events_count"`
	SkippedPinned     int      `json:"skipped_pinned,omitempty"`
	Pattern           string   `json:"pattern,omitempty"`
	OlderThanDays     int      `json:"older_than_days,omitempty"`
	DryRun            bool     `json:"dry_run,omitempty"`
	PurgedIDs         []string `json:"purged_ids,omitempty"`
}

var purgeCmd = &cobra.Command{
	Use:     "purge",
	GroupID: "maint",
	Short:   "Permanently delete closed ephemeral beads",
	Long: `Permanently delete closed ephemeral beads and their related data.

Closed ephemeral beads (wisps, transient molecules) accumulate rapidly as
workflow state. Once closed, they have no value. This command hard-deletes
them along with their events, comments, dependencies, and labels.

SELECTOR: ephemeral=1 AND status='closed'

The --pattern flag adds an ID glob filter on top of the ephemeral+closed
selector, useful for targeting specific caller patterns (e.g., "*-wisp-*").

EXAMPLES:
  bd purge --dry-run                     # Preview what would be purged
  bd purge                               # Purge all closed ephemeral beads
  bd purge --pattern "*-wisp-*"          # Only closed ephemerals matching pattern
  bd purge --older-than 7d               # Only items closed 7+ days ago
  bd purge --older-than 30d --dry-run    # Preview with age filter

SAFETY:
- Only targets ephemeral=1 AND status='closed' (never touches persistent beads)
- Skips pinned beads (protected from purge)
- Use --dry-run to preview before purging
- Auto-commits to Dolt after purge`,
	Run: runPurge,
}

func init() {
	purgeCmd.Flags().Bool("dry-run", false, "Preview what would be purged without deleting")
	purgeCmd.Flags().String("pattern", "", "Glob pattern to match issue IDs (e.g., \"*-wisp-*\")")
	purgeCmd.Flags().String("older-than", "", "Only purge items closed more than N days ago (e.g., \"7d\", \"30d\")")
	rootCmd.AddCommand(purgeCmd)
}

func runPurge(cmd *cobra.Command, _ []string) {
	CheckReadonly("purge")

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	pattern, _ := cmd.Flags().GetString("pattern")
	olderThanStr, _ := cmd.Flags().GetString("older-than")

	// Parse --older-than duration
	var olderThanDays int
	if olderThanStr != "" {
		days, err := parseOlderThan(olderThanStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --older-than value %q: %v\n", olderThanStr, err)
			os.Exit(1)
		}
		olderThanDays = days
	}

	// Ensure storage
	if store == nil {
		if err := ensureStoreActive(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	ctx := rootCtx

	// Build filter: ephemeral=1 AND status='closed'
	statusClosed := types.StatusClosed
	ephTrue := true
	filter := types.IssueFilter{
		Status:    &statusClosed,
		Ephemeral: &ephTrue,
	}

	if olderThanDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -olderThanDays)
		filter.ClosedBefore = &cutoff
	}

	// Search for candidates
	candidates, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error searching issues: %v\n", err)
		os.Exit(1)
	}

	// Apply --pattern glob filter on IDs
	if pattern != "" {
		var filtered []*types.Issue
		for _, issue := range candidates {
			matched, _ := filepath.Match(pattern, issue.ID)
			if matched {
				filtered = append(filtered, issue)
			}
		}
		candidates = filtered
	}

	// Filter out pinned issues
	pinnedCount := 0
	var toPurge []*types.Issue
	for _, issue := range candidates {
		if issue.Pinned {
			pinnedCount++
			continue
		}
		toPurge = append(toPurge, issue)
	}

	if pinnedCount > 0 && !jsonOutput {
		fmt.Printf("Skipping %d pinned issue(s) (protected from purge)\n", pinnedCount)
	}

	if len(toPurge) == 0 {
		msg := "No closed ephemeral beads to purge"
		if pattern != "" {
			msg += fmt.Sprintf(" (pattern: %s)", pattern)
		}
		if olderThanDays > 0 {
			msg += fmt.Sprintf(" (older than %dd)", olderThanDays)
		}
		if jsonOutput {
			outputJSON(PurgeResult{PurgedCount: 0, Pattern: pattern, OlderThanDays: olderThanDays})
		} else {
			fmt.Println(msg)
		}
		return
	}

	// Extract IDs
	issueIDs := make([]string, len(toPurge))
	for i, issue := range toPurge {
		issueIDs[i] = issue.ID
	}

	// Type assert to BatchDeleter
	d, ok := store.(storage.BatchDeleter)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: storage backend does not support batch deletion\n")
		os.Exit(1)
	}

	if dryRun {
		// Preview mode
		result, err := d.DeleteIssues(ctx, issueIDs, true, false, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error previewing purge: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(PurgeResult{
				PurgedCount:       result.DeletedCount,
				DependenciesCount: result.DependenciesCount,
				LabelsCount:       result.LabelsCount,
				EventsCount:       result.EventsCount,
				SkippedPinned:     pinnedCount,
				Pattern:           pattern,
				OlderThanDays:     olderThanDays,
				DryRun:            true,
				PurgedIDs:         issueIDs,
			})
		} else {
			fmt.Println(ui.RenderWarn("DRY RUN - no changes will be made"))
			fmt.Printf("\nWould purge: %d closed ephemeral bead(s)\n", result.DeletedCount)
			fmt.Printf("Would remove: %d dependencies, %d labels, %d events\n",
				result.DependenciesCount, result.LabelsCount, result.EventsCount)
			if pattern != "" {
				fmt.Printf("Pattern: %s\n", pattern)
			}
			if olderThanDays > 0 {
				fmt.Printf("Age filter: closed >%d days ago\n", olderThanDays)
			}
		}
		return
	}

	// Execute purge: cascade=true, force=true, dryRun=false
	result, err := d.DeleteIssues(ctx, issueIDs, true, true, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error purging: %v\n", err)
		os.Exit(1)
	}

	// Auto-commit for Dolt
	if commitErr := maybeAutoCommit(ctx, doltAutoCommitParams{
		Command:         "purge",
		IssueIDs:        issueIDs,
		MessageOverride: fmt.Sprintf("bd: purge %d closed ephemeral bead(s)", result.DeletedCount),
	}); commitErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-commit failed: %v\n", commitErr)
	}

	if jsonOutput {
		outputJSON(PurgeResult{
			PurgedCount:       result.DeletedCount,
			DependenciesCount: result.DependenciesCount,
			LabelsCount:       result.LabelsCount,
			EventsCount:       result.EventsCount,
			SkippedPinned:     pinnedCount,
			Pattern:           pattern,
			OlderThanDays:     olderThanDays,
			PurgedIDs:         issueIDs,
		})
	} else {
		fmt.Printf("%s Purged %d closed ephemeral bead(s)\n", ui.RenderPass("✓"), result.DeletedCount)
		fmt.Printf("  Removed %d dependency link(s)\n", result.DependenciesCount)
		fmt.Printf("  Removed %d label(s)\n", result.LabelsCount)
		fmt.Printf("  Removed %d event(s)\n", result.EventsCount)
		if pinnedCount > 0 {
			fmt.Printf("  Skipped %d pinned bead(s)\n", pinnedCount)
		}
	}
}

// parseOlderThan parses a duration string like "7d", "30d" into days.
func parseOlderThan(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	s = strings.ToLower(s)
	if strings.HasSuffix(s, "d") {
		s = strings.TrimSuffix(s, "d")
	}
	var days int
	if _, err := fmt.Sscanf(s, "%d", &days); err != nil {
		return 0, fmt.Errorf("expected number of days (e.g., \"7d\" or \"30d\")")
	}
	if days < 0 {
		return 0, fmt.Errorf("days must be positive")
	}
	return days, nil
}
