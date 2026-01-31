package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// adviceListCmd lists advice beads
var adviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List advice beads",
	Long: `List advice beads filtered by labels or subscriptions.

By default, shows all open advice. Use flags to filter:

LABEL-BASED FILTERING:
  -l, --label    Filter by label (can repeat, matches any)
  --subscribe    Simulate agent subscriptions (show matching advice)
  --for          Shorthand: auto-subscribe to agent's context labels

Examples:
  # List all advice
  bd advice list

  # Filter by labels (matches advice with ANY of these labels)
  bd advice list -l testing -l security

  # Simulate what an agent with these subscriptions would see
  bd advice list --subscribe testing --subscribe go --subscribe global

  # Show all advice applicable to an agent (auto-subscribes to context labels)
  bd advice list --for=beads/polecats/quartz

  # Include closed advice
  bd advice list --all`,
	Run: runAdviceList,
}

func init() {
	adviceListCmd.Flags().String("for", "", "Show advice applicable to agent (auto-subscribes to context labels)")
	adviceListCmd.Flags().StringArrayP("label", "l", nil, "Filter by label (can be repeated, matches any)")
	adviceListCmd.Flags().StringArray("subscribe", nil, "Simulate subscriptions - show advice matching these labels")
	adviceListCmd.Flags().BoolP("all", "a", false, "Include closed advice")
	adviceListCmd.Flags().BoolP("verbose", "v", false, "Show detailed output")

	adviceCmd.AddCommand(adviceListCmd)
}

func runAdviceList(cmd *cobra.Command, args []string) {
	// Get flags
	forAgent, _ := cmd.Flags().GetString("for")
	labels, _ := cmd.Flags().GetStringArray("label")
	subscriptions, _ := cmd.Flags().GetStringArray("subscribe")
	showAll, _ := cmd.Flags().GetBool("all")
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Convert --for to subscriptions (auto-subscribe to agent's context labels)
	if forAgent != "" {
		subscriptions = buildAgentSubscriptions(forAgent, subscriptions)
	}

	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

	// Build filter for advice type
	adviceType := types.IssueType("advice")
	filter := types.IssueFilter{
		IssueType: &adviceType,
	}

	// Add status filter unless --all
	if !showAll {
		openStatus := types.StatusOpen
		filter.Status = &openStatus
	}

	// Search for advice issues
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		FatalError("searching advice: %v", err)
	}

	// Get labels for all issues (always needed now for filtering)
	issueIDs := make([]string, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	labelsMap, err := store.GetLabelsForIssues(ctx, issueIDs)
	if err != nil {
		FatalError("getting labels: %v", err)
	}

	// Apply filtering based on labels/subscriptions
	var filtered []*types.Issue

	for _, issue := range issues {
		// Label-based filtering (--label flag)
		if len(labels) > 0 {
			if matchesAnyLabel(labelsMap[issue.ID], labels) {
				filtered = append(filtered, issue)
			}
			continue
		}

		// Subscription-based filtering (--subscribe or --for)
		if len(subscriptions) > 0 {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				filtered = append(filtered, issue)
			}
			continue
		}

		// No filter - include all
		filtered = append(filtered, issue)
	}

	// JSON output
	if jsonOutput {
		outputJSON(filtered)
		return
	}

	// Human-readable output
	if len(filtered) == 0 {
		scopeDesc := "advice"
		if len(subscriptions) > 0 {
			scopeDesc = fmt.Sprintf("advice matching subscriptions [%s]", strings.Join(subscriptions, ", "))
		} else if len(labels) > 0 {
			scopeDesc = fmt.Sprintf("advice with labels [%s]", strings.Join(labels, ", "))
		}
		fmt.Printf("No %s found\n", scopeDesc)
		return
	}

	// Show advice with labels
	fmt.Printf("## Agent Advice\n\n")
	if len(subscriptions) > 0 {
		fmt.Printf("Subscriptions: [%s]\n\n", strings.Join(subscriptions, ", "))
	}
	printAdviceListWithLabels(filtered, labelsMap, verbose)
}

// buildAgentSubscriptions creates the auto-subscription labels for an agent
// Agent IDs are typically: rig/role_plural/agent_name (e.g., beads/polecats/quartz)
func buildAgentSubscriptions(agentID string, existing []string) []string {
	subs := append([]string{}, existing...) // Copy existing
	subs = append(subs, "global")           // All agents subscribe to global
	subs = append(subs, "agent:"+agentID)   // Subscribe to own agent label

	// Parse agent ID to extract rig and role
	parts := strings.Split(agentID, "/")
	if len(parts) >= 1 {
		subs = append(subs, "rig:"+parts[0])
	}
	if len(parts) >= 2 {
		// Role is typically plural (polecats) - subscribe to both forms
		rolePlural := parts[1]
		subs = append(subs, "role:"+rolePlural)
		roleSingular := singularize(rolePlural)
		if roleSingular != rolePlural {
			subs = append(subs, "role:"+roleSingular)
		}
	}

	return subs
}

// NOTE: matchesAgentScope removed - use matchesSubscriptions with buildAgentSubscriptions instead

// singularize converts a plural role name to singular
func singularize(plural string) string {
	if strings.HasSuffix(plural, "s") {
		return strings.TrimSuffix(plural, "s")
	}
	return plural
}

// matchesAnyLabel checks if issue labels contain any of the filter labels
func matchesAnyLabel(issueLabels []string, filterLabels []string) bool {
	labelSet := make(map[string]bool)
	for _, l := range issueLabels {
		labelSet[l] = true
	}
	for _, l := range filterLabels {
		if labelSet[l] {
			return true
		}
	}
	return false
}

// matchesSubscriptions checks if advice should be shown based on subscriptions
// An advice matches if its labels intersect with the subscriptions
func matchesSubscriptions(issue *types.Issue, issueLabels []string, subscriptions []string) bool {
	// Build subscription set
	subSet := make(map[string]bool)
	for _, s := range subscriptions {
		subSet[s] = true
	}

	// Check if any advice labels match subscriptions
	for _, l := range issueLabels {
		if subSet[l] {
			return true
		}
	}

	return false
}

// printAdviceListWithLabels prints advice with their labels (for subscription mode)
func printAdviceListWithLabels(issues []*types.Issue, labelsMap map[string][]string, verbose bool) {
	for _, issue := range issues {
		issueLabels := labelsMap[issue.ID]

		// Status indicator
		status := ""
		if issue.Status == types.StatusClosed {
			status = " (closed)"
		}

		// Labels display
		labelStr := ""
		if len(issueLabels) > 0 {
			labelStr = fmt.Sprintf(" [%s]", strings.Join(issueLabels, ", "))
		}

		if verbose {
			fmt.Printf("  %s %s%s%s\n", ui.RenderID(issue.ID), issue.Title, labelStr, status)
			if issue.Description != "" && issue.Description != issue.Title {
				desc := issue.Description
				if len(desc) > 200 {
					desc = desc[:197] + "..."
				}
				fmt.Printf("    %s\n", desc)
			}
		} else {
			fmt.Printf("  %s%s%s\n", issue.Title, labelStr, status)
		}
	}
	fmt.Println()
}

// NOTE: printAdviceList removed - all display now uses printAdviceListWithLabels
