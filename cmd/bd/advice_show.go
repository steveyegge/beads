package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// adviceShowCmd shows details of an advice bead
var adviceShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show advice bead details",
	Long: `Show detailed information about an advice bead.

Displays the title, description, labels, hook configuration, creation date,
author, status, priority, and delivery scope.

Examples:
  bd advice show bd-abc123
  bd advice show bd-abc123 --json`,
	Args: cobra.ExactArgs(1),
	Run:  runAdviceShow,
}

func init() {
	adviceShowCmd.ValidArgsFunction = issueIDCompletion
	adviceCmd.AddCommand(adviceShowCmd)
}

func runAdviceShow(cmd *cobra.Command, args []string) {
	id := args[0]

	var issue *types.Issue
	var issueLabels []string
	var resolvedID string

	// Resolve partial ID via daemon RPC
	resolveArgs := &rpc.ResolveIDArgs{ID: id}
	resp, err := daemonClient.ResolveID(resolveArgs)
	if err != nil {
		FatalError("resolving ID %s: %v", id, err)
	}
	if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
		FatalError("unmarshaling resolved ID: %v", err)
	}

	// Fetch the issue via daemon
	showArgs := &rpc.ShowArgs{ID: resolvedID}
	showResp, err := daemonClient.Show(showArgs)
	if err != nil {
		FatalError("getting issue %s: %v", resolvedID, err)
	}
	if err := json.Unmarshal(showResp.Data, &issue); err != nil {
		FatalError("parsing issue: %v", err)
	}

	if issue == nil {
		FatalError("issue %s not found", resolvedID)
	}

	// Verify it's an advice issue
	if issue.IssueType != types.IssueType("advice") {
		FatalError("issue %s is not an advice bead (type: %s)", resolvedID, issue.IssueType)
	}

	// Labels are embedded in daemon response
	issueLabels = issue.Labels

	SetLastTouchedID(resolvedID)

	// JSON output
	if jsonOutput {
		issue.Labels = issueLabels
		outputJSON(issue)
		return
	}

	// Human-readable output
	renderAdviceShow(issue, resolvedID, issueLabels)
}

// renderAdviceShow prints detailed human-readable output for an advice bead.
func renderAdviceShow(issue *types.Issue, resolvedID string, issueLabels []string) {
	// Header line: ○ bd-abc123 · Advice Title   [● P2 · OPEN]
	statusIcon := ui.RenderStatusIcon(string(issue.Status))
	priorityStr := ui.RenderPriority(issue.Priority)
	statusStr := ui.RenderStatus(strings.ToUpper(string(issue.Status)))

	fmt.Printf("%s %s · %s   [%s · %s]\n",
		statusIcon,
		ui.RenderID(resolvedID),
		issue.Title,
		priorityStr,
		statusStr,
	)

	// Type / Created / Author line
	createdDate := issue.CreatedAt.Format("2006-01-02")
	author := issue.CreatedBy
	if author == "" {
		author = "unknown"
	}
	fmt.Printf("Type: %s · Created: %s · Author: %s\n", issue.IssueType, createdDate, author)

	// Description
	if issue.Description != "" {
		fmt.Printf("\nDESCRIPTION\n")
		for _, line := range strings.Split(issue.Description, "\n") {
			fmt.Printf("  %s\n", line)
		}
	}

	// Labels
	if len(issueLabels) > 0 {
		fmt.Printf("\nLABELS\n")
		fmt.Printf("  %s\n", strings.Join(issueLabels, ", "))
	}

	// Hook configuration
	if issue.AdviceHookCommand != "" {
		fmt.Printf("\nHOOK\n")
		fmt.Printf("  Command: %s\n", issue.AdviceHookCommand)
		if issue.AdviceHookTrigger != "" {
			fmt.Printf("  Trigger: %s\n", issue.AdviceHookTrigger)
		}
		onFailure := issue.AdviceHookOnFailure
		if onFailure == "" {
			onFailure = "warn"
		}
		fmt.Printf("  On Failure: %s\n", onFailure)
		timeout := issue.AdviceHookTimeout
		if timeout == 0 {
			timeout = types.AdviceHookTimeoutDefault
		}
		fmt.Printf("  Timeout: %ds\n", timeout)
	}

	// Delivery scope based on labels
	if len(issueLabels) > 0 {
		fmt.Printf("\nDELIVERY SCOPE\n")
		fmt.Printf("  %s\n", describeDeliveryScope(issueLabels))
	}
}

// describeDeliveryScope interprets labels to produce a human-readable delivery
// scope description. Labels use the convention: global, rig:X, role:Y, agent:Z.
func describeDeliveryScope(labels []string) string {
	var parts []string
	hasGlobal := false
	var rigs []string
	var roles []string
	var agents []string
	var other []string

	for _, label := range labels {
		// Strip group prefixes (e.g., "g0:role:polecat" -> "role:polecat")
		clean := label
		if strings.HasPrefix(label, "g") {
			if idx := strings.Index(label, ":"); idx > 0 {
				var groupNum int
				if _, err := fmt.Sscanf(label[:idx], "g%d", &groupNum); err == nil {
					clean = label[idx+1:]
				}
			}
		}

		switch {
		case clean == "global":
			hasGlobal = true
		case strings.HasPrefix(clean, "rig:"):
			rigs = append(rigs, strings.TrimPrefix(clean, "rig:"))
		case strings.HasPrefix(clean, "role:"):
			roles = append(roles, strings.TrimPrefix(clean, "role:"))
		case strings.HasPrefix(clean, "agent:"):
			agents = append(agents, strings.TrimPrefix(clean, "agent:"))
		default:
			other = append(other, clean)
		}
	}

	if hasGlobal {
		parts = append(parts, "Agents matching: [global] - all agents")
	}
	if len(rigs) > 0 {
		parts = append(parts, fmt.Sprintf("Rigs: %s", strings.Join(rigs, ", ")))
	}
	if len(roles) > 0 {
		parts = append(parts, fmt.Sprintf("Roles: %s", strings.Join(roles, ", ")))
	}
	if len(agents) > 0 {
		parts = append(parts, fmt.Sprintf("Agents: %s", strings.Join(agents, ", ")))
	}
	if len(other) > 0 {
		parts = append(parts, fmt.Sprintf("Labels: %s", strings.Join(other, ", ")))
	}

	if len(parts) == 0 {
		return "No delivery scope (no recognized targeting labels)"
	}
	return strings.Join(parts, "\n  ")
}
