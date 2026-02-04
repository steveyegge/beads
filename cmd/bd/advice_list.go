package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
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

  # COMPOUND LABEL MATCHING:
  # When advice uses compound labels (AND groups), --for matches correctly:
  #   - Advice with 'g0:role:polecat,g0:rig:beads' requires BOTH to match
  #   - Agent must be subscribed to role:polecat AND rig:beads
  #   - The --for flag auto-generates these subscriptions from the agent path

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
	// Note: buildAgentSubscriptions may need store access for native fields,
	// but we defer that to after we have issues (for filtering)
	if forAgent != "" && daemonClient == nil {
		// Only build subscriptions now if we have direct store access
		// (buildAgentSubscriptions may query the store for agent bead)
		subscriptions = buildAgentSubscriptions(forAgent, subscriptions)
	}

	ctx := rootCtx

	var issues []*types.Issue
	var labelsMap map[string][]string

	// If daemon is running, use RPC (fixes gt-w1vin0)
	if daemonClient != nil {
		// Build list args for advice type
		listArgs := &rpc.ListArgs{
			IssueType: "advice",
		}

		// Add status filter unless --all
		if !showAll {
			listArgs.Status = "open"
		}

		// Add label filters if specified
		if len(labels) > 0 {
			listArgs.LabelsAny = labels // OR semantics for label filtering
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

		// Parse response as IssueWithCounts (standard daemon response format)
		var issuesWithCounts []*types.IssueWithCounts
		if err := json.Unmarshal(resp.Data, &issuesWithCounts); err != nil {
			FatalError("parsing response: %v", err)
		}

		// Extract issues and build labels map from embedded labels
		issues = make([]*types.Issue, len(issuesWithCounts))
		labelsMap = make(map[string][]string)
		for i, iwc := range issuesWithCounts {
			issues[i] = iwc.Issue
			// Labels are embedded in the issue from daemon response
			if iwc.Issue != nil && len(iwc.Issue.Labels) > 0 {
				labelsMap[iwc.Issue.ID] = iwc.Issue.Labels
			}
		}

		// Build subscriptions now if --for was specified
		// We need to do this after getting issues because buildAgentSubscriptions
		// may need store access for native fields. With daemon, we skip that lookup.
		if forAgent != "" {
			subscriptions = buildAgentSubscriptionsWithoutStore(forAgent, subscriptions)
		}
	} else {
		// Direct mode: ensure store is initialized
		if err := ensureStoreActive(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

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
		var err error
		issues, err = store.SearchIssues(ctx, "", filter)
		if err != nil {
			FatalError("searching advice: %v", err)
		}

		// Get labels for all issues (always needed now for filtering)
		issueIDs := make([]string, len(issues))
		for i, issue := range issues {
			issueIDs[i] = issue.ID
		}
		labelsMap, err = store.GetLabelsForIssues(ctx, issueIDs)
		if err != nil {
			FatalError("getting labels: %v", err)
		}
	}

	// Apply filtering based on labels/subscriptions
	var filtered []*types.Issue

	for _, issue := range issues {
		// Label-based filtering (--label flag)
		// Skip if daemon already filtered by labels
		if len(labels) > 0 && daemonClient != nil {
			// Daemon already applied LabelsAny filter, include all
			filtered = append(filtered, issue)
			continue
		}
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
		// Populate labels for JSON output
		for _, issue := range filtered {
			issue.Labels = labelsMap[issue.ID]
		}
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

	// Look up agent bead to read native subscription fields (gt-w2mh8a.5)
	if store != nil {
		agent, err := store.GetIssue(rootCtx, agentID)
		if err == nil && agent != nil {
			// Add custom subscriptions
			subs = append(subs, agent.AdviceSubscriptions...)

			// Remove excluded subscriptions
			if len(agent.AdviceSubscriptionsExclude) > 0 {
				excludeSet := make(map[string]bool)
				for _, exc := range agent.AdviceSubscriptionsExclude {
					excludeSet[exc] = true
				}
				filtered := subs[:0]
				for _, sub := range subs {
					if !excludeSet[sub] {
						filtered = append(filtered, sub)
					}
				}
				subs = filtered
			}
		}
	}

	return subs
}

// NOTE: matchesAgentScope removed - use matchesSubscriptions with buildAgentSubscriptions instead

// buildAgentSubscriptionsWithoutStore creates the auto-subscription labels for an agent
// without requiring store access. This is used in daemon mode where we can't query
// the store directly. It provides the same basic subscriptions but skips the native
// AdviceSubscriptions and AdviceSubscriptionsExclude fields from agent beads.
func buildAgentSubscriptionsWithoutStore(agentID string, existing []string) []string {
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

// parseGroups extracts group numbers from label prefixes.
// Labels with gN: prefix are grouped together (AND within group).
// Labels without prefix are treated as separate groups (backward compat - OR behavior).
func parseGroups(labels []string) map[int][]string {
	groups := make(map[int][]string)
	nextUnprefixedGroup := 1000 // High number for backward compat

	for _, label := range labels {
		if strings.HasPrefix(label, "g") {
			// Parse gN: prefix
			idx := strings.Index(label, ":")
			if idx > 1 {
				var groupNum int
				if _, err := fmt.Sscanf(label[:idx], "g%d", &groupNum); err == nil {
					groups[groupNum] = append(groups[groupNum], label[idx+1:])
					continue
				}
			}
		}
		// No valid prefix - treat as separate group (OR behavior)
		groups[nextUnprefixedGroup] = append(groups[nextUnprefixedGroup], label)
		nextUnprefixedGroup++
	}
	return groups
}

// matchesSubscriptions checks if advice should be shown based on subscriptions.
//
// Matching rules:
//   - If advice has rig:X label, agent MUST be subscribed to rig:X (required match)
//   - If advice has agent:X label, agent MUST be subscribed to agent:X (required match)
//   - For other labels: AND within groups (gN: prefix), OR across groups
//
// This prevents rig-scoped advice from leaking to other rigs via role matches.
func matchesSubscriptions(issue *types.Issue, issueLabels []string, subscriptions []string) bool {
	// Build subscription set
	subSet := make(map[string]bool)
	for _, s := range subscriptions {
		subSet[s] = true
	}

	// Check for required labels (rig:X, agent:X) - these MUST match
	for _, l := range issueLabels {
		if strings.HasPrefix(l, "rig:") {
			// Advice has rig label - agent must be subscribed to this specific rig
			if !subSet[l] {
				return false
			}
		}
		if strings.HasPrefix(l, "agent:") {
			// Advice has agent label - agent must be subscribed to this specific agent
			if !subSet[l] {
				return false
			}
		}
	}

	// Parse label groups for AND/OR matching
	groups := parseGroups(issueLabels)

	// OR across groups: if any group fully matches, advice applies
	for _, groupLabels := range groups {
		allMatch := true
		for _, label := range groupLabels {
			if !subSet[label] {
				allMatch = false
				break
			}
		}
		if allMatch && len(groupLabels) > 0 {
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
