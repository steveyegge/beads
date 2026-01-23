package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// decisionShowCmd shows details of a decision point
var decisionShowCmd = &cobra.Command{
	Use:   "show <decision-id>",
	Short: "Show decision point details",
	Long: `Show detailed information about a decision point.

Displays the prompt, available options, response status, iteration history,
and any associated gate/blocking information.

Examples:
  bd decision show gt-abc.decision-1
  bd decision show gt-abc.decision-1 --json`,
	Args: cobra.ExactArgs(1),
	Run:  runDecisionShow,
}

func init() {
	decisionCmd.AddCommand(decisionShowCmd)
}

func runDecisionShow(cmd *cobra.Command, args []string) {
	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	decisionID := args[0]
	ctx := rootCtx

	// Resolve partial ID
	resolvedID, err := utils.ResolvePartialID(ctx, store, decisionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get the issue
	issue, err := store.GetIssue(ctx, resolvedID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting issue: %v\n", err)
		os.Exit(1)
	}
	if issue == nil {
		fmt.Fprintf(os.Stderr, "Error: issue %s not found\n", resolvedID)
		os.Exit(1)
	}

	// Verify it's a decision gate
	if issue.IssueType != types.TypeGate || issue.AwaitType != "decision" {
		fmt.Fprintf(os.Stderr, "Error: %s is not a decision point (type=%s, await_type=%s)\n",
			resolvedID, issue.IssueType, issue.AwaitType)
		os.Exit(1)
	}

	// Get the decision point data
	dp, err := store.GetDecisionPoint(ctx, resolvedID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting decision point: %v\n", err)
		os.Exit(1)
	}
	if dp == nil {
		fmt.Fprintf(os.Stderr, "Error: no decision point data for %s\n", resolvedID)
		os.Exit(1)
	}

	// Parse options
	var options []types.DecisionOption
	if dp.Options != "" {
		if err := json.Unmarshal([]byte(dp.Options), &options); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not parse options: %v\n", err)
		}
	}

	// Get issues that depend on this decision (will be unblocked when resolved)
	blockedIssues, _ := store.GetDependents(ctx, resolvedID)

	// JSON output
	if jsonOutput {
		result := map[string]interface{}{
			"id":             resolvedID,
			"issue":          issue,
			"decision_point": dp,
			"options":        options,
			"blocked_issues": blockedIssues,
		}
		outputJSON(result)
		return
	}

	// Human-readable output
	statusIcon := "⏳"
	statusText := "PENDING"
	if dp.RespondedAt != nil {
		statusIcon = "✓"
		statusText = "RESPONDED"
	}
	if issue.Status == types.StatusClosed {
		statusIcon = "✓"
		statusText = "CLOSED"
	}

	fmt.Printf("%s %s [%s]\n\n", statusIcon, ui.RenderID(resolvedID), statusText)

	// Prompt
	fmt.Printf("PROMPT\n")
	fmt.Printf("  %s\n\n", dp.Prompt)

	// Options
	if len(options) > 0 {
		fmt.Printf("OPTIONS\n")
		for _, opt := range options {
			selected := ""
			if opt.ID == dp.SelectedOption {
				selected = " ← SELECTED"
			}
			defaultMark := ""
			if opt.ID == dp.DefaultOption {
				defaultMark = " (default)"
			}
			fmt.Printf("  [%s] %s%s%s\n", opt.ID, opt.Label, defaultMark, selected)
			if opt.Description != "" {
				// Indent description
				fmt.Printf("       %s\n", opt.Description)
			}
		}
		fmt.Println()
	}

	// Response
	if dp.RespondedAt != nil {
		fmt.Printf("RESPONSE\n")
		if dp.SelectedOption != "" {
			fmt.Printf("  Selected: %s\n", dp.SelectedOption)
		}
		if dp.ResponseText != "" {
			fmt.Printf("  Text: %s\n", dp.ResponseText)
		}
		fmt.Printf("  At: %s\n", dp.RespondedAt.Format(time.RFC3339))
		if dp.RespondedBy != "" {
			fmt.Printf("  By: %s\n", dp.RespondedBy)
		}
		fmt.Println()
	}

	// Iteration info
	if dp.Iteration > 1 || dp.MaxIterations != 3 {
		fmt.Printf("ITERATION\n")
		fmt.Printf("  Current: %d of %d max\n", dp.Iteration, dp.MaxIterations)
		if dp.PriorID != "" {
			fmt.Printf("  Prior decision: %s\n", dp.PriorID)
		}
		if dp.Guidance != "" {
			fmt.Printf("  Guidance: %s\n", dp.Guidance)
		}
		fmt.Println()
	}

	// Blocking info
	if len(blockedIssues) > 0 {
		fmt.Printf("BLOCKS\n")
		for _, bi := range blockedIssues {
			fmt.Printf("  → %s: %s\n", bi.ID, bi.Title)
		}
		fmt.Println()
	}

	// Metadata
	fmt.Printf("METADATA\n")
	fmt.Printf("  Created: %s\n", dp.CreatedAt.Format(time.RFC3339))
	if issue.Timeout > 0 {
		expires := dp.CreatedAt.Add(issue.Timeout)
		fmt.Printf("  Timeout: %s (%s)\n", expires.Format(time.RFC3339), issue.Timeout)
	}
}
