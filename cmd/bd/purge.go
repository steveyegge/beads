package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
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

	// Use direct SQL via UnderlyingDB to avoid the slow recursive dependency
	// traversal in DeleteIssues.findAllDependentsRecursiveTx (N+1 BFS queries
	// inside a Dolt transaction hang with hundreds of IDs).
	db := store.UnderlyingDB()
	if db == nil {
		fmt.Fprintf(os.Stderr, "Error: underlying database not available\n")
		os.Exit(1)
	}

	// Build parameterized IN clause for all IDs
	inClause, args := purgeBuildInClause(issueIDs)
	// Double the args for queries that use IN twice (deps: issue_id OR depends_on_id)
	doubledArgs := make([]interface{}, 0, len(args)*2)
	doubledArgs = append(doubledArgs, args...)
	doubledArgs = append(doubledArgs, args...)

	// Count related data for stats
	var depsCount, labelsCount, eventsCount int

	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM dependencies WHERE issue_id IN ("+inClause+") OR depends_on_id IN ("+inClause+")",
		doubledArgs...).Scan(&depsCount); err != nil {
		fmt.Fprintf(os.Stderr, "Error counting dependencies: %v\n", err)
		os.Exit(1)
	}

	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM labels WHERE issue_id IN ("+inClause+")",
		args...).Scan(&labelsCount); err != nil {
		fmt.Fprintf(os.Stderr, "Error counting labels: %v\n", err)
		os.Exit(1)
	}

	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE issue_id IN ("+inClause+")",
		args...).Scan(&eventsCount); err != nil {
		fmt.Fprintf(os.Stderr, "Error counting events: %v\n", err)
		os.Exit(1)
	}

	if dryRun {
		// Preview mode — stats only, no mutations
		if jsonOutput {
			outputJSON(PurgeResult{
				PurgedCount:       len(issueIDs),
				DependenciesCount: depsCount,
				LabelsCount:       labelsCount,
				EventsCount:       eventsCount,
				SkippedPinned:     pinnedCount,
				Pattern:           pattern,
				OlderThanDays:     olderThanDays,
				DryRun:            true,
				PurgedIDs:         issueIDs,
			})
		} else {
			fmt.Println(ui.RenderWarn("DRY RUN - no changes will be made"))
			fmt.Printf("\nWould purge: %d closed ephemeral bead(s)\n", len(issueIDs))
			fmt.Printf("Would remove: %d dependencies, %d labels, %d events\n",
				depsCount, labelsCount, eventsCount)
			if pattern != "" {
				fmt.Printf("Pattern: %s\n", pattern)
			}
			if olderThanDays > 0 {
				fmt.Printf("Age filter: closed >%d days ago\n", olderThanDays)
			}
		}
		return
	}

	// Execute purge via direct SQL.
	// Step 1: Remove incoming dependency refs (depends_on_id has no FK CASCADE).
	if _, err := db.ExecContext(ctx,
		"DELETE FROM dependencies WHERE depends_on_id IN ("+inClause+")",
		args...); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing incoming dependencies: %v\n", err)
		os.Exit(1)
	}

	// Step 2: Hard-delete issues. FK CASCADE automatically removes:
	//   dependencies (issue_id), labels, comments, events,
	//   export_hashes, child_counters, issue_snapshots, compaction_snapshots.
	res, err := db.ExecContext(ctx,
		"DELETE FROM issues WHERE id IN ("+inClause+")",
		args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error purging issues: %v\n", err)
		os.Exit(1)
	}
	deletedCount, _ := res.RowsAffected()

	// Auto-commit for Dolt
	if commitErr := maybeAutoCommit(ctx, doltAutoCommitParams{
		Command:         "purge",
		IssueIDs:        issueIDs,
		MessageOverride: fmt.Sprintf("bd: purge %d closed ephemeral bead(s)", deletedCount),
	}); commitErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-commit failed: %v\n", commitErr)
	}

	if jsonOutput {
		outputJSON(PurgeResult{
			PurgedCount:       int(deletedCount),
			DependenciesCount: depsCount,
			LabelsCount:       labelsCount,
			EventsCount:       eventsCount,
			SkippedPinned:     pinnedCount,
			Pattern:           pattern,
			OlderThanDays:     olderThanDays,
			PurgedIDs:         issueIDs,
		})
	} else {
		fmt.Printf("%s Purged %d closed ephemeral bead(s)\n", ui.RenderPass("✓"), deletedCount)
		fmt.Printf("  Removed %d dependency link(s)\n", depsCount)
		fmt.Printf("  Removed %d label(s)\n", labelsCount)
		fmt.Printf("  Removed %d event(s)\n", eventsCount)
		if pinnedCount > 0 {
			fmt.Printf("  Skipped %d pinned bead(s)\n", pinnedCount)
		}
	}
}

// purgeBuildInClause builds a parameterized IN clause for SQL queries.
func purgeBuildInClause(ids []string) (string, []interface{}) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return strings.Join(placeholders, ","), args
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
