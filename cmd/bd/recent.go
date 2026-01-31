package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var (
	recentToday    bool
	recentThisWeek bool
	recentStale    bool
	recentLimit    int
)

// RecentItem represents an item (bead or spec) with modification time
type RecentItem struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Type       string    `json:"type"` // "bead" or "spec"
	Status     string    `json:"status,omitempty"`
	Priority   int       `json:"priority,omitempty"`
	ModifiedAt time.Time `json:"modified_at"`
	IsStale    bool      `json:"is_stale"`
	SpecID     string    `json:"spec_id,omitempty"` // For beads linked to specs
}

var recentCmd = &cobra.Command{
	Use:     "recent",
	GroupID: "views",
	Short:   "Show recently modified beads and specs",
	Long: `Display beads and specs sorted by last modification time.

This command provides a unified view of recent activity across
both issues (beads) and specification documents.

Staleness: Items untouched for 30+ days are flagged as stale.

Examples:
  bd recent               # Show last 20 modified items
  bd recent --today       # Items modified today only
  bd recent --this-week   # Items modified this week
  bd recent --stale       # Show only stale items (30+ days old)
  bd recent --limit 50    # Show more items`,
	Run: runRecent,
}

func init() {
	recentCmd.Flags().BoolVar(&recentToday, "today", false, "Show only items modified today")
	recentCmd.Flags().BoolVar(&recentThisWeek, "this-week", false, "Show only items modified this week")
	recentCmd.Flags().BoolVar(&recentStale, "stale", false, "Show only stale items (30+ days untouched)")
	recentCmd.Flags().IntVarP(&recentLimit, "limit", "n", 20, "Maximum number of items to show")

	rootCmd.AddCommand(recentCmd)
}

func runRecent(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	// Collect all items
	items := []RecentItem{}

	// Get beads
	beadItems, err := getRecentBeadsItems()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get beads: %v\n", err)
	} else {
		items = append(items, beadItems...)
	}

	// Get specs from registry (requires direct DB access)
	specItems, err := getRecentSpecItems(ctx)
	if err == nil {
		items = append(items, specItems...)
	}
	// Note: spec registry errors are silently ignored as it's optional

	// Apply time filters
	now := time.Now()
	items = filterRecentByTime(items, now)

	// Sort by modification time (most recent first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModifiedAt.After(items[j].ModifiedAt)
	})

	// Apply limit
	if len(items) > recentLimit {
		items = items[:recentLimit]
	}

	// Output
	if jsonOutput {
		outputJSON(items)
		return
	}

	if len(items) == 0 {
		fmt.Println("No recent items found")
		return
	}

	printRecentItems(items, now)
}

func getRecentBeadsItems() ([]RecentItem, error) {
	var issues []*types.Issue
	var err error

	if daemonClient != nil {
		// Use daemon RPC - use empty status to get all non-closed issues
		listArgs := &rpc.ListArgs{
			Limit: 500,
		}
		resp, rpcErr := daemonClient.List(listArgs)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if unmarshalErr := json.Unmarshal(resp.Data, &issues); unmarshalErr != nil {
			return nil, unmarshalErr
		}
	} else if store != nil {
		// Direct storage access
		issues, err = store.SearchIssues(rootCtx, "", types.IssueFilter{})
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("no storage available")
	}

	staleThreshold := time.Now().AddDate(0, 0, -30)
	items := make([]RecentItem, 0, len(issues))

	for _, issue := range issues {
		// Skip closed issues
		if issue.Status == types.StatusClosed {
			continue
		}
		item := RecentItem{
			ID:         issue.ID,
			Title:      issue.Title,
			Type:       "bead",
			Status:     string(issue.Status),
			Priority:   issue.Priority,
			ModifiedAt: issue.UpdatedAt,
			IsStale:    issue.UpdatedAt.Before(staleThreshold),
			SpecID:     issue.SpecID,
		}
		items = append(items, item)
	}

	return items, nil
}

func getRecentSpecItems(ctx context.Context) ([]RecentItem, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("no database path")
	}

	// Open read-only connection for spec registry
	roStore, err := sqlite.NewReadOnlyWithTimeout(ctx, dbPath, lockTimeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = roStore.Close() }()

	specs, err := roStore.ListSpecRegistry(ctx)
	if err != nil {
		return nil, err
	}

	staleThreshold := time.Now().AddDate(0, 0, -30)
	items := make([]RecentItem, 0, len(specs))

	for _, s := range specs {
		// Skip missing specs
		if s.MissingAt != nil {
			continue
		}

		modTime := s.LastScannedAt
		if !s.Mtime.IsZero() {
			modTime = s.Mtime
		}

		// Determine status based on lifecycle
		status := s.Lifecycle
		if status == "" {
			status = "active"
		}

		item := RecentItem{
			ID:         s.SpecID,
			Title:      s.Title,
			Type:       "spec",
			Status:     status,
			ModifiedAt: modTime,
			IsStale:    modTime.Before(staleThreshold),
		}
		items = append(items, item)
	}

	return items, nil
}

func filterRecentByTime(items []RecentItem, now time.Time) []RecentItem {
	// If --stale, filter to only stale items
	if recentStale {
		filtered := make([]RecentItem, 0)
		for _, item := range items {
			if item.IsStale {
				filtered = append(filtered, item)
			}
		}
		return filtered
	}

	// Apply time window filters
	var cutoff time.Time
	if recentToday {
		// Start of today
		cutoff = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	} else if recentThisWeek {
		// Start of this week (Monday)
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday
		}
		cutoff = time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, now.Location())
	} else {
		// No time filter
		return items
	}

	filtered := make([]RecentItem, 0)
	for _, item := range items {
		if item.ModifiedAt.After(cutoff) || item.ModifiedAt.Equal(cutoff) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func printRecentItems(items []RecentItem, now time.Time) {
	for _, item := range items {
		// Format time relative to now
		timeStr := formatRecentRelativeTime(item.ModifiedAt, now)

		// Status/type indicator
		var indicator string
		var typeLabel string
		if item.Type == "bead" {
			switch item.Status {
			case "in_progress":
				indicator = ui.RenderWarn("◐")
			case "blocked":
				indicator = ui.RenderFail("◉")
			case "deferred":
				indicator = ui.RenderMuted("◌")
			default:
				indicator = "○"
			}
			typeLabel = fmt.Sprintf("[P%d]", item.Priority)
		} else {
			// Spec
			switch item.Status {
			case "complete":
				indicator = ui.RenderPass("✓")
			case "archived":
				indicator = ui.RenderMuted("⊘")
			default:
				indicator = ui.RenderAccent("📄")
			}
			typeLabel = "[spec]"
		}

		// Staleness indicator
		staleMarker := ""
		if item.IsStale {
			staleMarker = ui.RenderFail(" ❄")
		}

		// Title (truncate if needed)
		title := item.Title
		if title == "" {
			title = filepath.Base(item.ID)
		}
		if len(title) > 50 {
			title = title[:47] + "..."
		}

		// Spec link for beads
		specLink := ""
		if item.SpecID != "" && item.Type == "bead" {
			specLink = fmt.Sprintf(" → %s", item.SpecID)
		}

		fmt.Printf("%s %s %s - %s%s%s  %s\n",
			indicator, item.ID, typeLabel, title, specLink, staleMarker, ui.RenderMuted(timeStr))
	}

	// Summary
	staleCount := 0
	beadCount := 0
	specCount := 0
	for _, item := range items {
		if item.IsStale {
			staleCount++
		}
		if item.Type == "bead" {
			beadCount++
		} else {
			specCount++
		}
	}

	fmt.Println()
	summary := fmt.Sprintf("%d beads, %d specs", beadCount, specCount)
	if staleCount > 0 {
		summary += fmt.Sprintf(" (%d stale)", staleCount)
	}
	fmt.Println(summary)
}

// formatRecentRelativeTime formats time relative to now (different name to avoid conflict)
func formatRecentRelativeTime(t time.Time, now time.Time) string {
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / (24 * 7))
		if weeks == 1 {
			return "1w ago"
		}
		return fmt.Sprintf("%dw ago", weeks)
	default:
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

// Unused but needed for spec import
var _ = spec.SpecRegistryEntry{}
