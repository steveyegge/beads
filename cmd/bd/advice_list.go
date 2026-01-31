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
	Long: `List advice beads filtered by scope.

By default, shows all open advice. Use flags to filter by target scope:
  --rig      Filter to advice targeting a specific rig
  --role     Filter to advice targeting a specific role (requires --rig)
  --agent    Filter to advice targeting a specific agent
  --all      Include closed advice

Examples:
  # List all advice
  bd advice list

  # List advice for a specific rig
  bd advice list --rig=beads

  # List advice for a role in a rig
  bd advice list --rig=beads --role=polecat

  # List advice for a specific agent
  bd advice list --agent=beads/polecats/quartz

  # List advice applicable to an agent (including inherited advice)
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
	adviceType := types.TypeAdvice
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

	// Apply additional filtering in-memory for advice targets
	var filtered []*types.Issue

	for _, issue := range issues {
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
		if forAgent != "" {
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

	// Group by scope for better display
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
