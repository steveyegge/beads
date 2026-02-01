package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var (
	recentToday      bool
	recentThisWeek   bool
	recentStale      bool
	recentLimit      int
	recentShowSkills bool
	recentShowAll    bool
)

// RecentItem represents an item (bead, spec, or skill) with modification time
type RecentItem struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	Type                 string    `json:"type"` // "bead", "spec", or "skill"
	Status               string    `json:"status,omitempty"`
	Priority             int       `json:"priority,omitempty"`
	ModifiedAt           time.Time `json:"modified_at"`
	IsStale              bool      `json:"is_stale"`
	SpecID               string    `json:"spec_id,omitempty"`   // For beads linked to specs
	BeadID               string    `json:"bead_id,omitempty"`   // For skills/specs linked to beads
	Source               string    `json:"source,omitempty"`    // For skills: claude, codex, etc.
	Tier                 string    `json:"tier,omitempty"`      // For skills: must-have, optional
	SkillIDs             []string  `json:"skill_ids,omitempty"` // Skills linked to this bead
	VolatilityLevel      string    `json:"volatility_level,omitempty"`
	VolatilityChanges    int       `json:"volatility_changes,omitempty"`
	VolatilityOpenIssues int       `json:"volatility_open_issues,omitempty"`
}

var recentCmd = &cobra.Command{
	Use:     "recent",
	GroupID: "views",
	Short:   "Show recently modified beads, specs, and skills",
	Long: `Display beads, specs, and skills sorted by last modification time.

This command provides a unified view of recent activity across
issues (beads), specification documents, and skills.

Staleness: Items untouched for 30+ days are flagged as stale.

Examples:
  bd recent               # Show last 20 modified beads and specs
  bd recent --skills      # Include skills in output
  bd recent --all         # Show beads, specs, AND skills with nested view
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
	recentCmd.Flags().BoolVar(&recentShowSkills, "skills", false, "Include skills in output")
	recentCmd.Flags().BoolVar(&recentShowAll, "all", false, "Show beads, specs, and skills with nested view")

	rootCmd.AddCommand(recentCmd)
}

func runRecent(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	// Collect all items
	items := []RecentItem{}

	// Build maps for nested view (--all mode)
	var specToBeadMap map[string]string     // spec_id -> bead_id
	var skillToBeadMap map[string]string    // skill_id -> bead_id
	var beadToSkillsMap map[string][]string // bead_id -> []skill_id

	if recentShowAll {
		specToBeadMap = make(map[string]string)
		skillToBeadMap = make(map[string]string)
		beadToSkillsMap = make(map[string][]string)
	}

	// Get beads
	beadItems, err := getRecentBeadsItems()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get beads: %v\n", err)
	} else {
		items = append(items, beadItems...)
		// Build spec->bead mapping for nested view
		if recentShowAll {
			for _, item := range beadItems {
				if item.SpecID != "" {
					specToBeadMap[item.SpecID] = item.ID
				}
			}
		}
	}

	// Get specs from registry (requires direct DB access)
	specItems, err := getRecentSpecItems(ctx)
	if err == nil {
		// In --all mode, attach bead IDs to specs
		if recentShowAll {
			for i := range specItems {
				if beadID, ok := specToBeadMap[specItems[i].ID]; ok {
					specItems[i].BeadID = beadID
				}
			}
		}
		items = append(items, specItems...)
	}
	// Note: spec registry errors are silently ignored as it's optional

	// Get skills if --skills or --all flag is set
	if recentShowSkills || recentShowAll {
		skillItems, skillBeadMap, beadSkillsMap, skillErr := getRecentSkillItems(ctx)
		if skillErr == nil {
			items = append(items, skillItems...)
			if recentShowAll {
				skillToBeadMap = skillBeadMap
				beadToSkillsMap = beadSkillsMap
				// Attach skill IDs to beads
				for i := range items {
					if items[i].Type == "bead" {
						if skills, ok := beadToSkillsMap[items[i].ID]; ok {
							items[i].SkillIDs = skills
						}
					}
				}
			}
		}
		// Skill errors are silently ignored as skills are optional
	}

	// Apply time filters
	now := time.Now()
	items = filterRecentByTime(items, now)

	annotateRecentVolatility(rootCtx, items)

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

	// Use nested view for --all mode
	if recentShowAll {
		printRecentItemsNested(items, now, specToBeadMap, skillToBeadMap)
	} else {
		printRecentItems(items, now)
	}
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

// getRecentSkillItems returns skills from the skills_manifest table.
// Also returns maps for skill->bead and bead->skills relationships.
func getRecentSkillItems(ctx context.Context) ([]RecentItem, map[string]string, map[string][]string, error) {
	if dbPath == "" {
		return nil, nil, nil, fmt.Errorf("no database path")
	}

	// Open read-only connection for skills tables
	roStore, err := sqlite.NewReadOnlyWithTimeout(ctx, dbPath, lockTimeout)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = roStore.Close() }()

	// Query active skills ordered by last_used_at (most recently used first)
	skills, err := roStore.ListSkills(ctx, sqlite.SkillFilter{Status: "active"})
	if err != nil {
		return nil, nil, nil, err
	}

	staleThreshold := time.Now().AddDate(0, 0, -30)
	items := make([]RecentItem, 0, len(skills))

	for _, skill := range skills {
		// Determine modification time (prefer last_used_at, fallback to created_at)
		modTime := skill.CreatedAt
		if skill.LastUsedAt != nil && !skill.LastUsedAt.IsZero() {
			modTime = *skill.LastUsedAt
		}

		item := RecentItem{
			ID:         skill.ID,
			Title:      skill.Name,
			Type:       "skill",
			Status:     skill.Status,
			ModifiedAt: modTime,
			IsStale:    modTime.Before(staleThreshold),
			Source:     skill.Source,
			Tier:       skill.Tier,
		}
		items = append(items, item)
	}

	// Build skill->bead and bead->skills maps from skill_bead_links
	skillToBeadMap := make(map[string]string)
	beadToSkillsMap := make(map[string][]string)

	db := roStore.UnderlyingDB()
	rows, err := db.QueryContext(ctx, `
		SELECT skill_id, bead_id FROM skill_bead_links ORDER BY linked_at DESC
	`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var skillID, beadID string
			if scanErr := rows.Scan(&skillID, &beadID); scanErr == nil {
				// First bead for each skill (most recent link)
				if _, exists := skillToBeadMap[skillID]; !exists {
					skillToBeadMap[skillID] = beadID
				}
				// Build bead->skills list
				beadToSkillsMap[beadID] = append(beadToSkillsMap[beadID], skillID)
			}
		}
	}

	return items, skillToBeadMap, beadToSkillsMap, nil
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

		// Status/type indicator and label based on type
		indicator, typeLabel := getItemIndicatorAndLabel(item)

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

		volatilityMarker := ""
		if item.VolatilityLevel != "" && (item.Type == "spec" || item.SpecID != "") {
			volatilityMarker = formatRecentVolatilityMarker(item.VolatilityLevel)
		}

		fmt.Printf("%s %s %s - %s%s%s  %s\n",
			indicator, item.ID, typeLabel, title, specLink+volatilityMarker, staleMarker, ui.RenderMuted(timeStr))
	}

	// Enhanced summary
	printRecentSummary(items, now)
}

// getItemIndicatorAndLabel returns the status icon and type label for an item
func getItemIndicatorAndLabel(item RecentItem) (indicator, typeLabel string) {
	switch item.Type {
	case "bead":
		switch item.Status {
		case "in_progress":
			indicator = ui.RenderWarn("◐")
		case "blocked":
			indicator = ui.RenderFail("●")
		case "deferred":
			indicator = ui.RenderMuted("○")
		case "closed":
			indicator = ui.RenderPass("✓")
		default:
			indicator = "○"
		}
		typeLabel = fmt.Sprintf("[P%d]", item.Priority)
	case "spec":
		switch item.Status {
		case "complete":
			indicator = ui.RenderPass("✓")
		case "archived":
			indicator = ui.RenderMuted("○")
		case "in-progress", "in_progress":
			indicator = ui.RenderWarn("◐")
		default:
			indicator = ui.RenderAccent("●")
		}
		typeLabel = "[spec]"
	case "skill":
		switch item.Status {
		case "active":
			indicator = ui.RenderPass("✓")
		case "deprecated":
			indicator = ui.RenderWarn("◐")
		case "archived":
			indicator = ui.RenderMuted("○")
		default:
			indicator = "○"
		}
		// Show tier for skills
		if item.Tier == "must-have" {
			typeLabel = ui.RenderAccent("[skill*]")
		} else {
			typeLabel = "[skill]"
		}
	default:
		indicator = "○"
		typeLabel = "[" + item.Type + "]"
	}
	return indicator, typeLabel
}

// printRecentItemsNested prints items in a nested tree view grouped by bead
func printRecentItemsNested(items []RecentItem, now time.Time, specToBeadMap, skillToBeadMap map[string]string) {
	// Separate items by type
	beads := make(map[string]RecentItem)
	specs := make(map[string]RecentItem)
	skills := make(map[string]RecentItem)

	for _, item := range items {
		switch item.Type {
		case "bead":
			beads[item.ID] = item
		case "spec":
			specs[item.ID] = item
		case "skill":
			skills[item.ID] = item
		}
	}

	// Sort beads by modification time (most recent first)
	beadList := make([]RecentItem, 0, len(beads))
	for _, b := range beads {
		beadList = append(beadList, b)
	}
	sort.Slice(beadList, func(i, j int) bool {
		return beadList[i].ModifiedAt.After(beadList[j].ModifiedAt)
	})

	// Track which specs/skills have been shown (to avoid duplicates)
	shownSpecs := make(map[string]bool)
	shownSkills := make(map[string]bool)

	// Print each bead with its nested specs and skills
	for _, bead := range beadList {
		timeStr := formatRecentRelativeTime(bead.ModifiedAt, now)

		// Priority label with color
		priorityLabel := ui.RenderPriorityCompact(bead.Priority)

		// Status text
		statusText := formatStatusText(bead.Status)

		// Staleness indicator
		staleMarker := ""
		if bead.IsStale {
			staleMarker = ui.RenderFail(" ❄")
		}

		// Title (truncate if needed)
		title := bead.Title
		if len(title) > 45 {
			title = title[:42] + "..."
		}

		fmt.Printf("%s [%s] %s%s  %s  %s\n",
			bead.ID, priorityLabel, title+formatRecentVolatilityMarker(bead.VolatilityLevel), staleMarker, statusText, ui.RenderMuted(timeStr))

		// Find linked spec
		hasChildren := false
		if bead.SpecID != "" {
			if specItem, ok := specs[bead.SpecID]; ok {
				hasChildren = true
				specTimeStr := formatRecentRelativeTime(specItem.ModifiedAt, now)
				specIndicator, _ := getItemIndicatorAndLabel(specItem)
				specStatus := formatStatusText(specItem.Status)

				// Check if there are skills to show after this spec
				hasSkillsAfter := len(bead.SkillIDs) > 0

				treeChar := ui.TreeLast
				if hasSkillsAfter {
					treeChar = "  " + ui.TreeChild[:len(ui.TreeChild)-1]
				}

				fmt.Printf("%s%s %s  %s  %s\n",
					treeChar, specIndicator, specItem.ID, specStatus+formatRecentVolatilityMarker(specItem.VolatilityLevel), ui.RenderMuted(specTimeStr))
				shownSpecs[bead.SpecID] = true

				// Print skills nested under spec if they exist
				if hasSkillsAfter {
					for i, skillID := range bead.SkillIDs {
						if skillItem, ok := skills[skillID]; ok {
							skillTimeStr := formatRecentRelativeTime(skillItem.ModifiedAt, now)
							skillIndicator, _ := getItemIndicatorAndLabel(skillItem)

							isLast := i == len(bead.SkillIDs)-1
							childTreeChar := ui.TreeLast
							if !isLast {
								childTreeChar = "     " + ui.TreeChild[:len(ui.TreeChild)-1]
							} else {
								childTreeChar = "     " + ui.TreeLast
							}

							skillLabel := skillItem.Title
							if skillItem.Tier == "must-have" {
								skillLabel += " " + ui.RenderAccent("*")
							}

							fmt.Printf("%s%s %s %s  %s\n",
								childTreeChar, skillIndicator, skillLabel, ui.RenderMuted("(skill)"), ui.RenderMuted(skillTimeStr))
							shownSkills[skillID] = true
						}
					}
				}
			}
		} else if len(bead.SkillIDs) == 0 {
			// No spec and no skills
			fmt.Printf("  %s%s\n", ui.TreeLast, ui.RenderMuted("(no linked spec)"))
		}

		// Print skills directly under bead if no spec
		if bead.SpecID == "" && len(bead.SkillIDs) > 0 {
			for i, skillID := range bead.SkillIDs {
				if skillItem, ok := skills[skillID]; ok {
					skillTimeStr := formatRecentRelativeTime(skillItem.ModifiedAt, now)
					skillIndicator, _ := getItemIndicatorAndLabel(skillItem)

					isLast := i == len(bead.SkillIDs)-1
					treeChar := ui.TreeLast
					if !isLast {
						treeChar = "  " + ui.TreeChild[:len(ui.TreeChild)-1]
					}

					skillLabel := skillItem.Title
					if skillItem.Tier == "must-have" {
						skillLabel += " " + ui.RenderAccent("*")
					}

					fmt.Printf("  %s%s %s %s  %s\n",
						treeChar, skillIndicator, skillLabel, ui.RenderMuted("(skill)"), ui.RenderMuted(skillTimeStr))
					shownSkills[skillID] = true
				}
			}
		}

		// Add spacing between beads if there were children
		if hasChildren || len(bead.SkillIDs) > 0 {
			// No extra spacing needed, tree structure provides visual separation
		}
	}

	// Show orphan specs (not linked to any bead in our results)
	orphanSpecs := []RecentItem{}
	for specID, specItem := range specs {
		if !shownSpecs[specID] {
			orphanSpecs = append(orphanSpecs, specItem)
		}
	}
	if len(orphanSpecs) > 0 {
		sort.Slice(orphanSpecs, func(i, j int) bool {
			return orphanSpecs[i].ModifiedAt.After(orphanSpecs[j].ModifiedAt)
		})
		fmt.Println()
		fmt.Println(ui.RenderMuted("Unlinked specs:"))
		for _, specItem := range orphanSpecs {
			timeStr := formatRecentRelativeTime(specItem.ModifiedAt, now)
			indicator, _ := getItemIndicatorAndLabel(specItem)
			statusText := formatStatusText(specItem.Status)
			fmt.Printf("  %s %s  %s  %s\n", indicator, specItem.ID, statusText, ui.RenderMuted(timeStr))
		}
	}

	// Show orphan skills (not linked to any bead in our results)
	orphanSkills := []RecentItem{}
	for skillID, skillItem := range skills {
		if !shownSkills[skillID] {
			orphanSkills = append(orphanSkills, skillItem)
		}
	}
	if len(orphanSkills) > 0 {
		sort.Slice(orphanSkills, func(i, j int) bool {
			return orphanSkills[i].ModifiedAt.After(orphanSkills[j].ModifiedAt)
		})
		fmt.Println()
		fmt.Println(ui.RenderMuted("Unlinked skills:"))
		for _, skillItem := range orphanSkills {
			timeStr := formatRecentRelativeTime(skillItem.ModifiedAt, now)
			indicator, _ := getItemIndicatorAndLabel(skillItem)
			tierMarker := ""
			if skillItem.Tier == "must-have" {
				tierMarker = ui.RenderAccent(" *")
			}
			fmt.Printf("  %s %s%s %s  %s\n",
				indicator, skillItem.Title, tierMarker, ui.RenderMuted("("+skillItem.Source+")"), ui.RenderMuted(timeStr))
		}
	}

	// Enhanced summary
	printRecentSummary(items, now)
}

func annotateRecentVolatility(ctx context.Context, items []RecentItem) {
	if len(items) == 0 {
		return
	}
	specIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, item := range items {
		var specID string
		if item.Type == "spec" {
			specID = item.ID
		} else if item.SpecID != "" {
			specID = item.SpecID
		}
		if specID == "" {
			continue
		}
		if _, ok := seen[specID]; ok {
			continue
		}
		seen[specID] = struct{}{}
		specIDs = append(specIDs, specID)
	}
	if len(specIDs) == 0 {
		return
	}
	window, err := parseDurationString("30d")
	if err != nil {
		return
	}
	since := time.Now().UTC().Add(-window).Truncate(time.Second)
	summaries, err := getSpecVolatilitySummaries(ctx, specIDs, since)
	if err != nil {
		return
	}
	for i := range items {
		var specID string
		if items[i].Type == "spec" {
			specID = items[i].ID
		} else if items[i].SpecID != "" {
			specID = items[i].SpecID
		}
		if specID == "" {
			continue
		}
		if summary, ok := summaries[specID]; ok {
			level := classifySpecVolatility(summary.ChangeCount, summary.OpenIssues)
			items[i].VolatilityLevel = string(level)
			items[i].VolatilityChanges = summary.ChangeCount
			items[i].VolatilityOpenIssues = summary.OpenIssues
		}
	}
}

func formatRecentVolatilityMarker(level string) string {
	if level == "" {
		return ""
	}
	switch specVolatilityLevel(level) {
	case specVolatilityHigh:
		return " " + ui.RenderWarn("🔥 volatile")
	case specVolatilityMedium:
		return " " + ui.RenderWarn("🔥 volatile")
	case specVolatilityLow:
		return " " + ui.RenderMuted("⚡ low")
	case specVolatilityStable:
		return " " + ui.RenderPass("⚡ stable")
	default:
		return ""
	}
}

// formatStatusText returns a styled status string
func formatStatusText(status string) string {
	switch status {
	case "in_progress", "in-progress":
		return ui.RenderWarn("◐ in-progress")
	case "blocked":
		return ui.RenderFail("● blocked")
	case "closed", "complete":
		return ui.RenderPass("✓ " + status)
	case "deferred":
		return ui.RenderMuted("❄ deferred")
	case "active":
		return ui.RenderPass("✓ active")
	case "deprecated":
		return ui.RenderWarn("◐ deprecated")
	case "archived":
		return ui.RenderMuted("○ archived")
	case "open", "pending":
		return "○ " + status
	default:
		return "○ " + status
	}
}

// printRecentSummary prints the enhanced summary with counts and momentum
func printRecentSummary(items []RecentItem, now time.Time) {
	// Count by type
	beadCount := 0
	specCount := 0
	skillCount := 0

	// Count by status
	inProgressCount := 0
	pendingCount := 0
	blockedCount := 0

	// Count stale and today
	staleCount := 0
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayCount := 0

	for _, item := range items {
		switch item.Type {
		case "bead":
			beadCount++
			switch item.Status {
			case "in_progress":
				inProgressCount++
			case "open", "pending":
				pendingCount++
			case "blocked":
				blockedCount++
			}
		case "spec":
			specCount++
			if item.Status == "in-progress" || item.Status == "in_progress" {
				inProgressCount++
			}
		case "skill":
			skillCount++
		}

		if item.IsStale {
			staleCount++
		}
		if item.ModifiedAt.After(todayStart) || item.ModifiedAt.Equal(todayStart) {
			todayCount++
		}
	}

	fmt.Println()
	fmt.Println(ui.RenderSeparator())

	// Main summary line
	summaryParts := []string{}
	if beadCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d beads", beadCount))
	}
	if specCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d specs", specCount))
	}
	if skillCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d skills", skillCount))
	}
	fmt.Printf("Summary: %s\n", strings.Join(summaryParts, ", "))

	// Active status breakdown
	activeParts := []string{}
	if inProgressCount > 0 {
		activeParts = append(activeParts, fmt.Sprintf("%d in-progress", inProgressCount))
	}
	if pendingCount > 0 {
		activeParts = append(activeParts, fmt.Sprintf("%d pending", pendingCount))
	}
	if blockedCount > 0 {
		activeParts = append(activeParts, ui.RenderFail(fmt.Sprintf("%d blocked", blockedCount)))
	}
	if len(activeParts) > 0 {
		fmt.Printf("%sActive: %s\n", ui.TreeChild, strings.Join(activeParts, ", "))
	}

	// Stale items
	if staleCount > 0 {
		fmt.Printf("%sStale (30+ days): %s\n", ui.TreeChild, ui.RenderFail(fmt.Sprintf("%d", staleCount)))
	}

	// Momentum (items updated today)
	momentumLabel := fmt.Sprintf("%d items updated today", todayCount)
	if todayCount == 0 {
		momentumLabel = ui.RenderMuted("no updates today")
	} else if todayCount >= 3 {
		momentumLabel = ui.RenderPass(momentumLabel)
	}
	fmt.Printf("%sMomentum: %s\n", ui.TreeLast, momentumLabel)
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
