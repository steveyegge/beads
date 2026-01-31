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
	Long: `List advice beads filtered by scope or labels.

By default, shows all open advice. Use flags to filter:

TARGET FILTERING (legacy):
  --rig      Filter to advice targeting a specific rig
  --role     Filter to advice targeting a specific role (requires --rig)
  --agent    Filter to advice targeting a specific agent
  --for      Show all advice applicable to an agent (includes inherited)

LABEL-BASED FILTERING (recommended):
  -l, --label    Filter by label (can repeat, matches any)
  --subscribe    Simulate agent subscriptions (show matching advice)

Examples:
  # List all advice
  bd advice list

  # Filter by labels (matches advice with ANY of these labels)
  bd advice list -l testing -l security

  # Simulate what an agent with these subscriptions would see
  bd advice list --subscribe testing --subscribe go --subscribe global

  # Legacy: List advice for a specific rig
  bd advice list --rig=beads

  # Legacy: List advice applicable to an agent
  bd advice list --for=beads/polecats/quartz

  # Include closed advice
  bd advice list --all`,
	Run: runAdviceList,
}

func init() {
	adviceListCmd.Flags().String("rig", "", "Filter by target rig")
	adviceListCmd.Flags().String("role", "", "Filter by target role (requires --rig)")
	adviceListCmd.Flags().String("agent", "", "Filter by target agent")
	adviceListCmd.Flags().String("for", "", "Show all advice applicable to an agent (includes inherited)")
	adviceListCmd.Flags().StringArrayP("label", "l", nil, "Filter by label (can be repeated, matches any)")
	adviceListCmd.Flags().StringArray("subscribe", nil, "Simulate subscriptions - show advice matching these labels")
	adviceListCmd.Flags().BoolP("all", "a", false, "Include closed advice")
	adviceListCmd.Flags().BoolP("verbose", "v", false, "Show detailed output")

	adviceCmd.AddCommand(adviceListCmd)
}

func runAdviceList(cmd *cobra.Command, args []string) {
	// Get flags
	rig, _ := cmd.Flags().GetString("rig")
	role, _ := cmd.Flags().GetString("role")
	agent, _ := cmd.Flags().GetString("agent")
	forAgent, _ := cmd.Flags().GetString("for")
	labels, _ := cmd.Flags().GetStringArray("label")
	subscriptions, _ := cmd.Flags().GetStringArray("subscribe")
	showAll, _ := cmd.Flags().GetBool("all")
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Validate role requires rig
	if role != "" && rig == "" {
		FatalError("--role requires --rig")
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

	// Get labels for all issues if we need them for filtering
	var labelsMap map[string][]string
	if len(labels) > 0 || len(subscriptions) > 0 {
		issueIDs := make([]string, len(issues))
		for i, issue := range issues {
			issueIDs[i] = issue.ID
		}
		labelsMap, err = store.GetLabelsForIssues(ctx, issueIDs)
		if err != nil {
			FatalError("getting labels: %v", err)
		}
	}

	// Apply additional filtering in-memory for advice targets
	var filtered []*types.Issue

	for _, issue := range issues {
		// Label-based filtering (--label flag)
		if len(labels) > 0 {
			if matchesAnyLabel(labelsMap[issue.ID], labels) {
				filtered = append(filtered, issue)
			}
			continue
		}

		// Subscription simulation (--subscribe flag)
		if len(subscriptions) > 0 {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				filtered = append(filtered, issue)
			}
			continue
		}
		// If --for flag is used, show all advice applicable to that agent
		if forAgent != "" {
			if matchesAgentScope(issue, forAgent) {
				filtered = append(filtered, issue)
			}
			continue
		}

		// Exact match filtering
		if agent != "" {
			if issue.AdviceTargetAgent == agent {
				filtered = append(filtered, issue)
			}
			continue
		}

		if role != "" {
			if issue.AdviceTargetRig == rig && issue.AdviceTargetRole == role {
				filtered = append(filtered, issue)
			}
			continue
		}

		if rig != "" {
			if issue.AdviceTargetRig == rig && issue.AdviceTargetRole == "" && issue.AdviceTargetAgent == "" {
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
		} else if forAgent != "" {
			scopeDesc = fmt.Sprintf("advice applicable to %s", forAgent)
		} else if agent != "" {
			scopeDesc = fmt.Sprintf("advice for agent %s", agent)
		} else if role != "" {
			scopeDesc = fmt.Sprintf("advice for role %s/%s", rig, role)
		} else if rig != "" {
			scopeDesc = fmt.Sprintf("advice for rig %s", rig)
		}
		fmt.Printf("No %s found\n", scopeDesc)
		return
	}

	// Subscription/label mode: show labels with each advice
	if len(subscriptions) > 0 || len(labels) > 0 {
		fmt.Printf("## Agent Advice\n\n")
		if len(subscriptions) > 0 {
			fmt.Printf("Subscriptions: [%s]\n\n", strings.Join(subscriptions, ", "))
		}
		printAdviceListWithLabels(filtered, labelsMap, verbose)
		return
	}

	// Group by scope for better display (legacy mode)
	var global, rigLevel, roleLevel, agentLevel []*types.Issue
	for _, issue := range filtered {
		if issue.AdviceTargetAgent != "" {
			agentLevel = append(agentLevel, issue)
		} else if issue.AdviceTargetRole != "" {
			roleLevel = append(roleLevel, issue)
		} else if issue.AdviceTargetRig != "" {
			rigLevel = append(rigLevel, issue)
		} else {
			global = append(global, issue)
		}
	}

	fmt.Printf("## Agent Advice\n\n")

	// Print each scope
	if len(global) > 0 {
		fmt.Printf("**[Global]**\n")
		printAdviceList(global, verbose)
	}

	if len(rigLevel) > 0 {
		fmt.Printf("**[Rig]**\n")
		printAdviceList(rigLevel, verbose)
	}

	if len(roleLevel) > 0 {
		fmt.Printf("**[Role]**\n")
		printAdviceList(roleLevel, verbose)
	}

	if len(agentLevel) > 0 {
		fmt.Printf("**[Agent]**\n")
		printAdviceList(agentLevel, verbose)
	}
}

// matchesAgentScope checks if an advice issue applies to the given agent
func matchesAgentScope(issue *types.Issue, agentID string) bool {
	// Global advice applies to everyone
	if issue.AdviceTargetRig == "" && issue.AdviceTargetRole == "" && issue.AdviceTargetAgent == "" {
		return true
	}

	// Agent-specific advice
	if issue.AdviceTargetAgent != "" {
		return issue.AdviceTargetAgent == agentID
	}

	// Parse agent ID to extract rig and role
	// Agent IDs are typically in format: rig/role_plural/agent_name
	// e.g., "beads/polecats/quartz" -> rig="beads", role="polecat"
	parts := strings.Split(agentID, "/")
	if len(parts) < 2 {
		return false
	}

	agentRig := parts[0]

	// Role is typically plural (polecats) but advice uses singular (polecat)
	agentRolePlural := ""
	if len(parts) >= 2 {
		agentRolePlural = parts[1]
	}
	agentRole := singularize(agentRolePlural)

	// Rig-level advice
	if issue.AdviceTargetRole == "" && issue.AdviceTargetRig != "" {
		return issue.AdviceTargetRig == agentRig
	}

	// Role-level advice
	if issue.AdviceTargetRole != "" && issue.AdviceTargetRig != "" {
		return issue.AdviceTargetRig == agentRig && issue.AdviceTargetRole == agentRole
	}

	return false
}

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
// An advice matches if:
// 1. Its labels intersect with the subscriptions, OR
// 2. It has auto-labels matching the subscription (rig:X, role:Y, agent:Z, global)
func matchesSubscriptions(issue *types.Issue, issueLabels []string, subscriptions []string) bool {
	// Build subscription set
	subSet := make(map[string]bool)
	for _, s := range subscriptions {
		subSet[s] = true
	}

	// Check explicit labels
	for _, l := range issueLabels {
		if subSet[l] {
			return true
		}
	}

	// Check auto-labels from targeting (backward compatibility)
	// Global advice has implicit "global" label
	if issue.AdviceTargetRig == "" && issue.AdviceTargetRole == "" && issue.AdviceTargetAgent == "" {
		if subSet["global"] {
			return true
		}
	}

	// Rig-targeted advice has implicit "rig:X" label
	if issue.AdviceTargetRig != "" && issue.AdviceTargetRole == "" && issue.AdviceTargetAgent == "" {
		if subSet["rig:"+issue.AdviceTargetRig] {
			return true
		}
	}

	// Role-targeted advice has implicit "role:X" label
	if issue.AdviceTargetRole != "" {
		if subSet["role:"+issue.AdviceTargetRole] {
			return true
		}
	}

	// Agent-targeted advice has implicit "agent:X" label
	if issue.AdviceTargetAgent != "" {
		if subSet["agent:"+issue.AdviceTargetAgent] {
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

// printAdviceList prints a list of advice issues
func printAdviceList(issues []*types.Issue, verbose bool) {
	for _, issue := range issues {
		// Build scope string
		scope := ""
		if issue.AdviceTargetAgent != "" {
			scope = fmt.Sprintf("[%s] ", issue.AdviceTargetAgent)
		} else if issue.AdviceTargetRole != "" {
			scope = fmt.Sprintf("[%s/%s] ", issue.AdviceTargetRig, issue.AdviceTargetRole)
		} else if issue.AdviceTargetRig != "" {
			scope = fmt.Sprintf("[%s] ", issue.AdviceTargetRig)
		}

		// Status indicator
		status := ""
		if issue.Status == types.StatusClosed {
			status = " (closed)"
		}

		if verbose {
			fmt.Printf("  %s %s%s%s\n", ui.RenderID(issue.ID), scope, issue.Title, status)
			if issue.Description != "" && issue.Description != issue.Title {
				// Wrap description
				desc := issue.Description
				if len(desc) > 200 {
					desc = desc[:197] + "..."
				}
				fmt.Printf("    %s\n", desc)
			}
		} else {
			fmt.Printf("  %s%s%s\n", scope, issue.Title, status)
		}
	}
	fmt.Println()
}
