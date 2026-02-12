package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// decisionListCmd lists decision points
var decisionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List decision points",
	Long: `List decision points in the beads database.

By default, shows only pending (unresponded) decision points.
Use --all to include responded decisions.

Examples:
  # List pending decisions
  bd decision list

  # List all decisions (including responded)
  bd decision list --all

  # Show detailed output
  bd decision list --verbose`,
	Run: runDecisionList,
}

func init() {
	decisionListCmd.Flags().BoolP("all", "a", false, "Show all decisions (including responded)")
	decisionListCmd.Flags().BoolP("verbose", "v", false, "Show detailed output")

	decisionCmd.AddCommand(decisionListCmd)
}

func runDecisionList(cmd *cobra.Command, args []string) {
	showAll, _ := cmd.Flags().GetBool("all")
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Enrich with issue data
	type enrichedDecision struct {
		*types.DecisionPoint
		Issue   *types.Issue           `json:"issue,omitempty"`
		Options []types.DecisionOption `json:"options_parsed,omitempty"`
	}

	var decisions []enrichedDecision

	requireDaemon("decision list")

	listArgs := &rpc.DecisionListArgs{
		All: showAll,
	}
	result, err := daemonClient.DecisionList(listArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing decisions via daemon: %v\n", err)
		os.Exit(1)
	}

	// Convert from RPC response to enrichedDecision
	for _, dr := range result.Decisions {
		ed := enrichedDecision{
			DecisionPoint: dr.Decision,
			Issue:         dr.Issue,
		}

		// Parse options
		if dr.Decision != nil && dr.Decision.Options != "" {
			var opts []types.DecisionOption
			if err := json.Unmarshal([]byte(dr.Decision.Options), &opts); err == nil {
				ed.Options = opts
			}
		}

		decisions = append(decisions, ed)
	}

	// JSON output
	if jsonOutput {
		outputJSON(decisions)
		return
	}

	// Human-readable output
	if len(decisions) == 0 {
		fmt.Println("No pending decisions")
		return
	}

	fmt.Printf("📋 Pending decisions (%d):\n\n", len(decisions))

	for i, d := range decisions {
		// Status indicator
		status := "⏳"
		if d.RespondedAt != nil {
			status = "✓"
		}

		// Issue title
		title := d.Prompt
		if d.Issue != nil && d.Issue.Title != title {
			title = d.Issue.Title
		}

		// Age
		age := formatAge(d.CreatedAt)

		fmt.Printf("%d. %s %s %s\n", i+1, status, ui.RenderID(d.IssueID), age)
		fmt.Printf("   %s\n", d.Prompt)

		if verbose {
			// Show options
			if len(d.Options) > 0 {
				fmt.Println("   Options:")
				for _, opt := range d.Options {
					defaultMark := ""
					if opt.ID == d.DefaultOption {
						defaultMark = " (default)"
					}
					fmt.Printf("     [%s] %s%s\n", opt.ID, opt.Label, defaultMark)
				}
			}

			// Show iteration info
			if d.Iteration > 1 {
				fmt.Printf("   Iteration: %d/%d\n", d.Iteration, d.MaxIterations)
			}
			if d.PriorID != "" {
				fmt.Printf("   Prior: %s\n", d.PriorID)
			}
		}

		fmt.Println()
	}

	// Summary
	if !verbose {
		fmt.Printf("Use 'bd decision show <id>' for details, 'bd decision respond <id> --select=<opt>' to respond\n")
	}
}

// formatAge returns a human-readable age string
func formatAge(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
