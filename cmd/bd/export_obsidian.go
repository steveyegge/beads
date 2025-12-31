package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// obsidianCheckbox maps bd status to Obsidian Tasks checkbox syntax
var obsidianCheckbox = map[types.Status]string{
	types.StatusOpen:       "- [ ]",
	types.StatusInProgress: "- [/]",
	types.StatusBlocked:    "- [c]",
	types.StatusClosed:     "- [x]",
	types.StatusTombstone:  "- [-]",
	types.StatusDeferred:   "- [-]",
	types.StatusPinned:     "- [n]", // Review/attention
	types.StatusHooked:     "- [/]", // Treat as in-progress
}

// obsidianPriority maps bd priority (0-4) to Obsidian priority emoji
var obsidianPriority = []string{
	"🔺", // 0 = critical/highest
	"⏫", // 1 = high
	"🔼", // 2 = medium
	"🔽", // 3 = low
	"⏬", // 4 = backlog/lowest
}

// obsidianTypeTag maps bd issue type to Obsidian tag
var obsidianTypeTag = map[types.IssueType]string{
	types.TypeBug:          "#Bug",
	types.TypeFeature:      "#Feature",
	types.TypeTask:         "#Task",
	types.TypeEpic:         "#Epic",
	types.TypeChore:        "#Chore",
	types.TypeMessage:      "#Message",
	types.TypeMergeRequest: "#MergeRequest",
	types.TypeMolecule:     "#Molecule",
	types.TypeGate:         "#Gate",
	types.TypeAgent:        "#Agent",
	types.TypeRole:         "#Role",
	types.TypeConvoy:       "#Convoy",
	types.TypeEvent:        "#Event",
}

// formatObsidianTask converts a single issue to Obsidian Tasks format
func formatObsidianTask(issue *types.Issue) string {
	var parts []string

	// Checkbox based on status
	checkbox, ok := obsidianCheckbox[issue.Status]
	if !ok {
		checkbox = "- [ ]" // default to open
	}
	parts = append(parts, checkbox)

	// Issue ID
	parts = append(parts, issue.ID)

	// Title
	parts = append(parts, issue.Title)

	// Priority emoji
	if issue.Priority >= 0 && issue.Priority < len(obsidianPriority) {
		parts = append(parts, obsidianPriority[issue.Priority])
	}

	// Type tag
	if tag, ok := obsidianTypeTag[issue.IssueType]; ok {
		parts = append(parts, tag)
	}

	// Labels as tags
	for _, label := range issue.Labels {
		// Sanitize label for tag use (replace spaces with dashes)
		tag := "#" + strings.ReplaceAll(label, " ", "-")
		parts = append(parts, tag)
	}

	// Start date (created_at)
	parts = append(parts, fmt.Sprintf("🛫 %s", issue.CreatedAt.Format("2006-01-02")))

	// End date (closed_at) if closed
	if issue.ClosedAt != nil {
		parts = append(parts, fmt.Sprintf("✅ %s", issue.ClosedAt.Format("2006-01-02")))
	}

	// Blockers as tags (from dependencies)
	for _, dep := range issue.Dependencies {
		if dep.Type == types.DepBlocks {
			parts = append(parts, fmt.Sprintf("#Blocker/%s", dep.DependsOnID))
		}
	}

	return strings.Join(parts, " ")
}

// groupIssuesByDate groups issues by their most recent activity date
func groupIssuesByDate(issues []*types.Issue) map[string][]*types.Issue {
	grouped := make(map[string][]*types.Issue)
	for _, issue := range issues {
		// Use the most recent date: closed_at > updated_at > created_at
		var date time.Time
		if issue.ClosedAt != nil {
			date = *issue.ClosedAt
		} else {
			date = issue.UpdatedAt
		}
		key := date.Format("2006-01-02")
		grouped[key] = append(grouped[key], issue)
	}
	return grouped
}

// writeObsidianExport writes issues in Obsidian Tasks markdown format
func writeObsidianExport(w io.Writer, issues []*types.Issue) error {
	// Write header
	if _, err := fmt.Fprintln(w, "# Changes Log"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	// Group by date
	grouped := groupIssuesByDate(issues)

	// Get sorted dates (most recent first)
	var dates []string
	for date := range grouped {
		dates = append(dates, date)
	}
	// Sort descending
	for i := 0; i < len(dates)-1; i++ {
		for j := i + 1; j < len(dates); j++ {
			if dates[i] < dates[j] {
				dates[i], dates[j] = dates[j], dates[i]
			}
		}
	}

	// Write each date section
	for _, date := range dates {
		if _, err := fmt.Fprintf(w, "## %s\n\n", date); err != nil {
			return err
		}
		for _, issue := range grouped[date] {
			line := formatObsidianTask(issue)
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}

	return nil
}
