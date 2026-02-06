package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// advicePreviewCmd shows what an agent would see during gt prime
var advicePreviewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Preview advice as an agent would see it during gt prime",
	Long: `Show exactly what an agent would see during gt prime - renders advice
in the same format, grouped by scope, with label match explanations.

This is useful for verifying advice targeting before agents pick it up.

Examples:
  # Preview advice for a specific agent
  bd advice preview --for=beads/polecats/quartz

  # Preview in raw PRIME.md format (what gt prime actually injects)
  bd advice preview --for=beads/polecats/quartz --raw`,
	Run: runAdvicePreview,
}

func init() {
	advicePreviewCmd.Flags().String("for", "", "Agent ID to preview for (required)")
	advicePreviewCmd.Flags().Bool("raw", false, "Show raw PRIME.md format")
	advicePreviewCmd.MarkFlagRequired("for")

	adviceCmd.AddCommand(advicePreviewCmd)
}

// adviceScopeGroup holds advice grouped by scope for rendering
type adviceScopeGroup struct {
	Scope    string           // "global", "rig", "role", "agent"
	Target   string           // "" for global, or the rig/role/agent name
	Header   string           // Display header: "Global", "Rig: beads", "Role: polecat", etc.
	Items    []*advicePreviewItem
}

// advicePreviewItem holds a single advice item with its match info
type advicePreviewItem struct {
	Issue         *types.Issue `json:"issue"`
	MatchedLabels []string     `json:"matched_labels"`
}

// advicePreviewResult is the JSON output structure
type advicePreviewResult struct {
	Agent         string             `json:"agent"`
	Subscriptions []string           `json:"subscriptions"`
	Groups        []advicePreviewGroup `json:"groups"`
	TotalCount    int                `json:"total_count"`
}

// advicePreviewGroup is the JSON representation of a scope group
type advicePreviewGroup struct {
	Scope  string               `json:"scope"`
	Target string               `json:"target,omitempty"`
	Header string               `json:"header"`
	Count  int                  `json:"count"`
	Items  []advicePreviewJSON  `json:"items"`
}

// advicePreviewJSON is the JSON representation of a single advice item
type advicePreviewJSON struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	MatchedLabels []string `json:"matched_labels"`
}

func runAdvicePreview(cmd *cobra.Command, args []string) {
	forAgent, _ := cmd.Flags().GetString("for")
	rawMode, _ := cmd.Flags().GetBool("raw")

	// Build subscriptions
	var subscriptions []string
	if daemonClient != nil {
		subscriptions = buildAgentSubscriptionsWithoutStore(forAgent, nil)
	} else {
		subscriptions = buildAgentSubscriptions(forAgent, nil)
	}

	ctx := rootCtx

	var issues []*types.Issue
	var labelsMap map[string][]string

	// Fetch all open advice (same pattern as advice list)
	if daemonClient != nil {
		listArgs := &rpc.ListArgs{
			IssueType: "advice",
			Status:    "open",
		}

		resp, err := daemonClient.List(listArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !resp.Success {
			fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
			os.Exit(1)
		}

		var issuesWithCounts []*types.IssueWithCounts
		if err := json.Unmarshal(resp.Data, &issuesWithCounts); err != nil {
			FatalError("parsing response: %v", err)
		}

		issues = make([]*types.Issue, len(issuesWithCounts))
		labelsMap = make(map[string][]string)
		for i, iwc := range issuesWithCounts {
			issues[i] = iwc.Issue
			if iwc.Issue != nil && len(iwc.Issue.Labels) > 0 {
				labelsMap[iwc.Issue.ID] = iwc.Issue.Labels
			}
		}
	} else {
		// Direct mode
		if err := ensureStoreActive(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		adviceType := types.IssueType("advice")
		openStatus := types.StatusOpen
		filter := types.IssueFilter{
			IssueType: &adviceType,
			Status:    &openStatus,
		}

		var err error
		issues, err = store.SearchIssues(ctx, "", filter)
		if err != nil {
			FatalError("searching advice: %v", err)
		}

		issueIDs := make([]string, len(issues))
		for i, issue := range issues {
			issueIDs[i] = issue.ID
		}
		labelsMap, err = store.GetLabelsForIssues(ctx, issueIDs)
		if err != nil {
			FatalError("getting labels: %v", err)
		}
	}

	// Filter with matchesSubscriptions
	var matched []*advicePreviewItem
	for _, issue := range issues {
		issueLabels := labelsMap[issue.ID]
		if matchesSubscriptions(issue, issueLabels, subscriptions) {
			// Determine which labels matched
			matchedLabels := findMatchedLabels(issueLabels, subscriptions)
			matched = append(matched, &advicePreviewItem{
				Issue:         issue,
				MatchedLabels: matchedLabels,
			})
		}
	}

	// Group by scope
	groups := groupByScope(matched)

	// JSON output
	if jsonOutput {
		result := advicePreviewResult{
			Agent:         forAgent,
			Subscriptions: subscriptions,
			TotalCount:    len(matched),
		}
		for _, g := range groups {
			jsonGroup := advicePreviewGroup{
				Scope:  g.Scope,
				Target: g.Target,
				Header: g.Header,
				Count:  len(g.Items),
			}
			for _, item := range g.Items {
				jsonItem := advicePreviewJSON{
					ID:            item.Issue.ID,
					Title:         item.Issue.Title,
					Description:   item.Issue.Description,
					MatchedLabels: item.MatchedLabels,
				}
				jsonGroup.Items = append(jsonGroup.Items, jsonItem)
			}
			result.Groups = append(result.Groups, jsonGroup)
		}
		outputJSON(result)
		return
	}

	// Human-readable output
	if rawMode {
		renderRawPreview(groups)
	} else {
		renderHumanPreview(forAgent, subscriptions, groups, len(matched))
	}
}

// stripGroupPrefixes removes gN: prefixes from labels, returning clean labels.
// Uses stripGroupPrefix from advice_list.go for each individual label.
func stripGroupPrefixes(labels []string) []string {
	result := make([]string, 0, len(labels))
	for _, l := range labels {
		result = append(result, stripGroupPrefix(l))
	}
	return result
}

// findMatchedLabels determines which subscription labels matched this advice's labels.
func findMatchedLabels(issueLabels []string, subscriptions []string) []string {
	subSet := make(map[string]bool)
	for _, s := range subscriptions {
		subSet[s] = true
	}

	seen := make(map[string]bool)
	var matched []string

	// Check stripped labels against subscriptions
	for _, l := range issueLabels {
		clean := stripGroupPrefix(l)
		if subSet[clean] && !seen[clean] {
			matched = append(matched, clean)
			seen[clean] = true
		}
	}
	return matched
}

// groupByScope organizes advice items into scope groups.
// Returns groups in order: Global, Rig (sorted), Role (sorted), Agent.
func groupByScope(items []*advicePreviewItem) []*adviceScopeGroup {
	groupMap := make(map[string]*adviceScopeGroup)

	for _, item := range items {
		issueLabels := item.Issue.Labels
		if len(issueLabels) == 0 {
			issueLabels = item.MatchedLabels
		}
		scope, target := categorizeAdviceScope(issueLabels)

		key := scope + ":" + target
		group, exists := groupMap[key]
		if !exists {
			header := buildScopeHeader(scope, target)
			group = &adviceScopeGroup{
				Scope:  scope,
				Target: target,
				Header: header,
			}
			groupMap[key] = group
		}
		group.Items = append(group.Items, item)
	}

	// Collect and sort groups: global first, then rig, role, agent
	var groups []*adviceScopeGroup
	for _, g := range groupMap {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		return scopeSortKey(groups[i]) < scopeSortKey(groups[j])
	})

	return groups
}

// buildScopeHeader creates the display header for a scope group.
func buildScopeHeader(scope, target string) string {
	switch scope {
	case "global":
		return "Global"
	case "rig":
		return "Rig: " + target
	case "role":
		return "Role: " + target
	case "agent":
		return "Agent: " + target
	default:
		return scope
	}
}

// scopeSortKey returns a string for ordering scope groups.
func scopeSortKey(g *adviceScopeGroup) string {
	switch g.Scope {
	case "global":
		return "0:" + g.Target
	case "rig":
		return "1:" + g.Target
	case "role":
		return "2:" + g.Target
	case "agent":
		return "3:" + g.Target
	default:
		return "9:" + g.Target
	}
}

// renderHumanPreview renders the human-readable preview format.
func renderHumanPreview(agent string, subscriptions []string, groups []*adviceScopeGroup, totalCount int) {
	fmt.Printf("## Advice Preview for: %s\n\n", agent)
	fmt.Printf("Subscriptions: [%s]\n\n", strings.Join(subscriptions, ", "))

	if totalCount == 0 {
		fmt.Println("No matching advice found.")
		return
	}

	for _, group := range groups {
		fmt.Printf("### %s (%d)\n", group.Header, len(group.Items))
		for _, item := range group.Items {
			fmt.Printf("  %s\n", ui.RenderBold(item.Issue.Title))
			if item.Issue.Description != "" && item.Issue.Description != item.Issue.Title {
				desc := item.Issue.Description
				if len(desc) > 200 {
					desc = desc[:197] + "..."
				}
				fmt.Printf("  %s\n", desc)
			}
			displayLabels := stripGroupPrefixes(item.MatchedLabels)
			fmt.Printf("  %s Matched: [%s]\n", ui.RenderMuted("\u21b3"), strings.Join(displayLabels, ", "))
			fmt.Println()
		}
	}
}

// renderRawPreview renders in the format gt prime actually injects.
func renderRawPreview(groups []*adviceScopeGroup) {
	first := true
	for _, group := range groups {
		for _, item := range group.Items {
			if !first {
				fmt.Println()
			}
			first = false

			fmt.Printf("**[%s]** %s\n", group.Header, item.Issue.Title)
			if item.Issue.Description != "" && item.Issue.Description != item.Issue.Title {
				// Indent description lines
				lines := strings.Split(item.Issue.Description, "\n")
				for _, line := range lines {
					fmt.Printf("  %s\n", line)
				}
			}
		}
	}
}
