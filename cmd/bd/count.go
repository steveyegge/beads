package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/util"
)

var countCmd = &cobra.Command{
	Use:     "count",
	GroupID: "views",
	Short:   "Count issues matching filters",
	Long: `Count issues matching the specified filters.

By default, returns the total count of issues matching the filters.
Use --by-* flags to group counts by different attributes.

Examples:
  bd count                          # Count all issues
  bd count --status open            # Count open issues
  bd count --by-status              # Group count by status
  bd count --by-priority            # Group count by priority
  bd count --by-type                # Group count by issue type
  bd count --by-assignee            # Group count by assignee
  bd count --by-label               # Group count by label
  bd count --assignee alice --by-status  # Count alice's issues by status
`,
	Run: func(cmd *cobra.Command, args []string) {
		status, _ := cmd.Flags().GetString("status")
		assignee, _ := cmd.Flags().GetString("assignee")
		issueType, _ := cmd.Flags().GetString("type")
		labels, _ := cmd.Flags().GetStringSlice("label")
		labelsAny, _ := cmd.Flags().GetStringSlice("label-any")
		titleSearch, _ := cmd.Flags().GetString("title")
		idFilter, _ := cmd.Flags().GetString("id")
		allFlag, _ := cmd.Flags().GetBool("all")
		includeGates, _ := cmd.Flags().GetBool("include-gates")
		includeTemplates, _ := cmd.Flags().GetBool("include-templates")

		// Pattern matching flags
		titleContains, _ := cmd.Flags().GetString("title-contains")
		descContains, _ := cmd.Flags().GetString("desc-contains")
		notesContains, _ := cmd.Flags().GetString("notes-contains")

		// Date range flags
		createdAfter, _ := cmd.Flags().GetString("created-after")
		createdBefore, _ := cmd.Flags().GetString("created-before")
		updatedAfter, _ := cmd.Flags().GetString("updated-after")
		updatedBefore, _ := cmd.Flags().GetString("updated-before")
		closedAfter, _ := cmd.Flags().GetString("closed-after")
		closedBefore, _ := cmd.Flags().GetString("closed-before")

		// Empty/null check flags
		emptyDesc, _ := cmd.Flags().GetBool("empty-description")
		noAssignee, _ := cmd.Flags().GetBool("no-assignee")
		noLabels, _ := cmd.Flags().GetBool("no-labels")

		// Priority range flags
		priorityMin, _ := cmd.Flags().GetInt("priority-min")
		priorityMax, _ := cmd.Flags().GetInt("priority-max")

		// Group by flags
		byStatus, _ := cmd.Flags().GetBool("by-status")
		byPriority, _ := cmd.Flags().GetBool("by-priority")
		byType, _ := cmd.Flags().GetBool("by-type")
		byAssignee, _ := cmd.Flags().GetBool("by-assignee")
		byLabel, _ := cmd.Flags().GetBool("by-label")

		// Determine groupBy value
		groupBy := ""
		groupCount := 0
		if byStatus {
			groupBy = "status"
			groupCount++
		}
		if byPriority {
			groupBy = "priority"
			groupCount++
		}
		if byType {
			groupBy = "type"
			groupCount++
		}
		if byAssignee {
			groupBy = "assignee"
			groupCount++
		}
		if byLabel {
			groupBy = "label"
			groupCount++
		}

		if groupCount > 1 {
			fmt.Fprintf(os.Stderr, "Error: only one --by-* flag can be specified\n")
			os.Exit(1)
		}

		// Normalize labels
		labels = util.NormalizeLabels(labels)
		labelsAny = util.NormalizeLabels(labelsAny)

		// Default filters to match bd list behavior (gt-w676pl.1)
		// Exclude closed issues by default unless explicit --status or --all
		var defaultExcludeStatus []string
		if status == "" && !allFlag {
			defaultExcludeStatus = []string{"closed"}
		}
		// Exclude gate issues by default unless --include-gates or --type gate
		var defaultExcludeTypes []string
		if !includeGates && issueType != "gate" {
			defaultExcludeTypes = []string{"gate"}
		}

		requireDaemon("count")
		{
			countArgs := &rpc.CountArgs{
				Status:    status,
				IssueType: issueType,
				Assignee:  assignee,
				GroupBy:   groupBy,
			}
			if cmd.Flags().Changed("priority") {
				priority, _ := cmd.Flags().GetInt("priority")
				countArgs.Priority = &priority
			}
			if len(labels) > 0 {
				countArgs.Labels = labels
			}
			if len(labelsAny) > 0 {
				countArgs.LabelsAny = labelsAny
			}
			if titleSearch != "" {
				countArgs.Query = titleSearch
			}
			if idFilter != "" {
				ids := util.NormalizeLabels(strings.Split(idFilter, ","))
				if len(ids) > 0 {
					countArgs.IDs = ids
				}
			}

			// Pattern matching
			countArgs.TitleContains = titleContains
			countArgs.DescriptionContains = descContains
			countArgs.NotesContains = notesContains

			// Date ranges
			countArgs.CreatedAfter = createdAfter
			countArgs.CreatedBefore = createdBefore
			countArgs.UpdatedAfter = updatedAfter
			countArgs.UpdatedBefore = updatedBefore
			countArgs.ClosedAfter = closedAfter
			countArgs.ClosedBefore = closedBefore

			// Empty/null checks
			countArgs.EmptyDescription = emptyDesc
			countArgs.NoAssignee = noAssignee
			countArgs.NoLabels = noLabels

			// Priority range
			if cmd.Flags().Changed("priority-min") {
				countArgs.PriorityMin = &priorityMin
			}
			if cmd.Flags().Changed("priority-max") {
				countArgs.PriorityMax = &priorityMax
			}

			// Default exclusions to match bd list behavior (gt-w676pl.1)
			countArgs.ExcludeStatus = defaultExcludeStatus
			countArgs.ExcludeTypes = defaultExcludeTypes
			countArgs.IncludeTemplates = includeTemplates

			resp, err := daemonClient.Count(countArgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if groupBy == "" {
				// Simple count
				var result struct {
					Count int `json:"count"`
				}
				if err := json.Unmarshal(resp.Data, &result); err != nil {
					fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
					os.Exit(1)
				}

				if jsonOutput {
					outputJSON(result)
				} else {
					fmt.Println(result.Count)
				}
			} else {
				// Grouped count
				var result struct {
					Total  int `json:"total"`
					Groups []struct {
						Group string `json:"group"`
						Count int    `json:"count"`
					} `json:"groups"`
				}
				if err := json.Unmarshal(resp.Data, &result); err != nil {
					fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
					os.Exit(1)
				}

				if jsonOutput {
					outputJSON(result)
				} else {
					// Sort groups for consistent output
					slices.SortFunc(result.Groups, func(a, b struct {
						Group string `json:"group"`
						Count int    `json:"count"`
					}) int {
						return cmp.Compare(a.Group, b.Group)
					})

					fmt.Printf("Total: %d\n\n", result.Total)
					for _, g := range result.Groups {
						fmt.Printf("%s: %d\n", g.Group, g.Count)
					}
				}
			}
		}
	},
}

func init() {
	// Filter flags (same as list command)
	countCmd.Flags().StringP("status", "s", "", "Filter by status (open, in_progress, blocked, deferred, closed)")
	countCmd.Flags().IntP("priority", "p", 0, "Filter by priority (0-4: 0=critical, 1=high, 2=medium, 3=low, 4=backlog)")
	countCmd.Flags().StringP("assignee", "a", "", "Filter by assignee")
	countCmd.Flags().StringP("type", "t", "", "Filter by type (bug, feature, task, epic, chore, merge-request, molecule, gate)")
	countCmd.Flags().StringSliceP("label", "l", []string{}, "Filter by labels (AND: must have ALL)")
	countCmd.Flags().StringSlice("label-any", []string{}, "Filter by labels (OR: must have AT LEAST ONE)")
	countCmd.Flags().String("title", "", "Filter by title text (case-insensitive substring match)")
	countCmd.Flags().String("id", "", "Filter by specific issue IDs (comma-separated)")

	// Pattern matching
	countCmd.Flags().String("title-contains", "", "Filter by title substring")
	countCmd.Flags().String("desc-contains", "", "Filter by description substring")
	countCmd.Flags().String("notes-contains", "", "Filter by notes substring")

	// Date ranges
	countCmd.Flags().String("created-after", "", "Filter issues created after date (YYYY-MM-DD or RFC3339)")
	countCmd.Flags().String("created-before", "", "Filter issues created before date (YYYY-MM-DD or RFC3339)")
	countCmd.Flags().String("updated-after", "", "Filter issues updated after date (YYYY-MM-DD or RFC3339)")
	countCmd.Flags().String("updated-before", "", "Filter issues updated before date (YYYY-MM-DD or RFC3339)")
	countCmd.Flags().String("closed-after", "", "Filter issues closed after date (YYYY-MM-DD or RFC3339)")
	countCmd.Flags().String("closed-before", "", "Filter issues closed before date (YYYY-MM-DD or RFC3339)")

	// Empty/null checks
	countCmd.Flags().Bool("empty-description", false, "Filter issues with empty description")
	countCmd.Flags().Bool("no-assignee", false, "Filter issues with no assignee")
	countCmd.Flags().Bool("no-labels", false, "Filter issues with no labels")

	// Priority ranges
	countCmd.Flags().Int("priority-min", 0, "Filter by minimum priority (inclusive)")
	countCmd.Flags().Int("priority-max", 0, "Filter by maximum priority (inclusive)")

	// Grouping flags
	countCmd.Flags().Bool("by-status", false, "Group count by status")
	countCmd.Flags().Bool("by-priority", false, "Group count by priority")
	countCmd.Flags().Bool("by-type", false, "Group count by issue type")
	countCmd.Flags().Bool("by-assignee", false, "Group count by assignee")
	countCmd.Flags().Bool("by-label", false, "Group count by label")

	// Scope control (gt-w676pl.1: match bd list defaults)
	countCmd.Flags().Bool("all", false, "Include all statuses (including closed)")
	countCmd.Flags().Bool("include-gates", false, "Include gate issues in count")
	countCmd.Flags().Bool("include-templates", false, "Include template issues in count")

	rootCmd.AddCommand(countCmd)
}
