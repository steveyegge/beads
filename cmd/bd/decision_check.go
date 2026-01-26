package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

// decisionCheckCmd checks for decision responses (for hooks)
var decisionCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for decision responses (for Claude Code hooks)",
	Long: `Check for recently responded decisions.

This command is designed for Claude Code hooks to notify about decision responses.

Exit codes (normal mode):
  0 - One or more decisions have been responded to
  1 - No responded decisions found

Exit codes (--inject mode):
  0 - Always (hooks should never block)
  Output: <system-reminder> if responses exist, silent otherwise

The --inject mode outputs responses wrapped in <system-reminder> tags
that Claude Code recognizes and injects into the conversation context.

Examples:
  # Check for responses (for scripting)
  bd decision check && echo "Responses available"

  # For Claude Code hooks (SessionStart)
  bd decision check --inject

  # Check for specific decision
  bd decision check --id gt-abc.decision-1`,
	Run: runDecisionCheck,
}

var (
	checkInject bool
	checkID     string
)

func init() {
	decisionCheckCmd.Flags().BoolVar(&checkInject, "inject", false, "Output format for Claude Code hooks")
	decisionCheckCmd.Flags().StringVar(&checkID, "id", "", "Check specific decision ID")

	decisionCmd.AddCommand(decisionCheckCmd)
}

// DecisionCheckResult for JSON output
type DecisionCheckResult struct {
	HasResponses bool                  `json:"has_responses"`
	Responses    []DecisionResponseSum `json:"responses,omitempty"`
}

type DecisionResponseSum struct {
	ID          string `json:"id"`
	Prompt      string `json:"prompt"`
	Selected    string `json:"selected,omitempty"`
	SelectedLbl string `json:"selected_label,omitempty"`
	Text        string `json:"text,omitempty"`
	RespondedBy string `json:"responded_by,omitempty"`
}

func runDecisionCheck(cmd *cobra.Command, args []string) {
	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		if checkInject {
			// Silent failure for hooks
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

	var responses []DecisionResponseSum

	if checkID != "" {
		// Check specific decision
		dp, err := store.GetDecisionPoint(ctx, checkID)
		if err != nil || dp == nil {
			if checkInject {
				os.Exit(0)
			}
			os.Exit(1)
		}

		if dp.RespondedAt != nil {
			// Parse options to get label
			var options []types.DecisionOption
			if dp.Options != "" {
				_ = json.Unmarshal([]byte(dp.Options), &options)
			}

			selectedLabel := dp.SelectedOption
			for _, opt := range options {
				if opt.ID == dp.SelectedOption {
					selectedLabel = opt.Label
					break
				}
			}

			responses = append(responses, DecisionResponseSum{
				ID:          checkID,
				Prompt:      dp.Prompt,
				Selected:    dp.SelectedOption,
				SelectedLbl: selectedLabel,
				Text:        dp.ResponseText,
				RespondedBy: dp.RespondedBy,
			})
		}
	} else {
		// Get all pending decisions - these are the ones we might have responded to
		// Note: ListPendingDecisions returns decisions without responses,
		// so we need a different approach to find responded ones.
		// For now, we'll check if there are any pending decisions and report that.
		// In practice, the --id flag should be used for specific decision tracking.

		pendingDecisions, err := store.ListPendingDecisions(ctx)
		if err != nil {
			if checkInject {
				os.Exit(0)
			}
			fmt.Fprintf(os.Stderr, "Error listing decisions: %v\n", err)
			os.Exit(1)
		}

		// Check each pending decision to see if it has been responded
		// (ListPendingDecisions may be slightly stale)
		for _, dp := range pendingDecisions {
			// Re-fetch to get latest state
			freshDP, err := store.GetDecisionPoint(ctx, dp.IssueID)
			if err != nil || freshDP == nil {
				continue
			}

			// Only include if responded
			if freshDP.RespondedAt == nil {
				continue
			}

			// Parse options for label
			var options []types.DecisionOption
			if freshDP.Options != "" {
				_ = json.Unmarshal([]byte(freshDP.Options), &options)
			}

			selectedLabel := freshDP.SelectedOption
			for _, opt := range options {
				if opt.ID == freshDP.SelectedOption {
					selectedLabel = opt.Label
					break
				}
			}

			responses = append(responses, DecisionResponseSum{
				ID:          freshDP.IssueID,
				Prompt:      freshDP.Prompt,
				Selected:    freshDP.SelectedOption,
				SelectedLbl: selectedLabel,
				Text:        freshDP.ResponseText,
				RespondedBy: freshDP.RespondedBy,
			})
		}
	}

	// JSON output
	if jsonOutput {
		result := DecisionCheckResult{
			HasResponses: len(responses) > 0,
			Responses:    responses,
		}
		outputJSON(result)
		if len(responses) > 0 {
			os.Exit(0)
		}
		os.Exit(1)
	}

	// Inject mode for hooks
	if checkInject {
		if len(responses) == 0 {
			// Silent - no output
			os.Exit(0)
		}

		fmt.Println("<system-reminder>")
		fmt.Printf("Decision response(s) received:\n\n")
		for _, r := range responses {
			fmt.Printf("Decision: %s\n", r.ID)
			fmt.Printf("  Prompt: %s\n", r.Prompt)
			if r.Selected != "" {
				fmt.Printf("  Selected: %s (%s)\n", r.Selected, r.SelectedLbl)
			}
			if r.Text != "" {
				fmt.Printf("  Text: %s\n", r.Text)
			}
			if r.RespondedBy != "" {
				fmt.Printf("  By: %s\n", r.RespondedBy)
			}
			fmt.Println()
		}
		fmt.Println("Use this response to continue your work.")
		fmt.Println("</system-reminder>")
		os.Exit(0)
	}

	// Normal output
	if len(responses) == 0 {
		fmt.Println("No decision responses found")
		os.Exit(1)
	}

	fmt.Printf("Found %d decision response(s):\n\n", len(responses))
	for _, r := range responses {
		fmt.Printf("  %s\n", r.ID)
		fmt.Printf("    Prompt: %s\n", r.Prompt)
		if r.Selected != "" {
			fmt.Printf("    Selected: %s (%s)\n", r.Selected, r.SelectedLbl)
		}
		if r.Text != "" {
			fmt.Printf("    Text: %s\n", r.Text)
		}
		fmt.Println()
	}
	os.Exit(0)
}
