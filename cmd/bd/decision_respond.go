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

// decisionRespondCmd records a human response to a decision point
var decisionRespondCmd = &cobra.Command{
	Use:   "respond <decision-id>",
	Short: "Record a human response to a decision point",
	Long: `Record a response to a pending decision point gate.

The response can be:
  1. Select an option: --select=<option-id>
  2. Provide text guidance: --text="..."
  3. Both: select an option AND provide additional text
  4. Accept guidance as-is: --accept-guidance (skips iteration, uses text directly)

When an option is selected, the decision gate closes and any blocked issues are unblocked.
When only text is provided (no selection), iterative refinement may be triggered (future feature).

Examples:
  # Select an option
  bd decision respond gt-abc.decision-1 --select=a

  # Provide text guidance (may trigger iteration)
  bd decision respond gt-abc.decision-1 --text="I'd prefer a hybrid approach"

  # Select with additional context
  bd decision respond gt-abc.decision-1 --select=b --text="But make sure to handle edge case X"

  # Accept text guidance directly without iteration
  bd decision respond gt-abc.decision-1 --text="Just do X" --accept-guidance

  # Specify who responded (for audit)
  bd decision respond gt-abc.decision-1 --select=yes --by=user@example.com`,
	Args: cobra.ExactArgs(1),
	Run:  runDecisionRespond,
}

func init() {
	decisionRespondCmd.Flags().StringP("select", "s", "", "Option ID to select")
	decisionRespondCmd.Flags().StringP("text", "t", "", "Custom text response/guidance")
	decisionRespondCmd.Flags().String("by", "", "Respondent identity (email, user ID)")
	decisionRespondCmd.Flags().Bool("accept-guidance", false, "Skip iteration, accept text as directive")

	decisionCmd.AddCommand(decisionRespondCmd)
}

func runDecisionRespond(cmd *cobra.Command, args []string) {
	CheckReadonly("decision respond")

	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	decisionID := args[0]
	selectOpt, _ := cmd.Flags().GetString("select")
	textResponse, _ := cmd.Flags().GetString("text")
	respondedBy, _ := cmd.Flags().GetString("by")
	acceptGuidance, _ := cmd.Flags().GetBool("accept-guidance")

	ctx := rootCtx

	// Must provide either --select or --text
	if selectOpt == "" && textResponse == "" {
		fmt.Fprintf(os.Stderr, "Error: must provide --select and/or --text\n")
		os.Exit(1)
	}

	// --accept-guidance requires --text
	if acceptGuidance && textResponse == "" {
		fmt.Fprintf(os.Stderr, "Error: --accept-guidance requires --text\n")
		os.Exit(1)
	}

	// Resolve partial ID
	resolvedID, err := utils.ResolvePartialID(ctx, store, decisionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get the issue to verify it's a decision gate
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

	// Check if already responded
	if dp.RespondedAt != nil {
		fmt.Fprintf(os.Stderr, "Error: decision %s already responded at %s by %s\n",
			resolvedID, dp.RespondedAt.Format(time.RFC3339), dp.RespondedBy)
		if dp.SelectedOption != "" {
			fmt.Fprintf(os.Stderr, "  Selected: %s\n", dp.SelectedOption)
		}
		if dp.ResponseText != "" {
			fmt.Fprintf(os.Stderr, "  Text: %s\n", dp.ResponseText)
		}
		os.Exit(1)
	}

	// If --select provided, validate the option exists
	var options []types.DecisionOption
	if dp.Options != "" {
		if err := json.Unmarshal([]byte(dp.Options), &options); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing options: %v\n", err)
			os.Exit(1)
		}
	}

	if selectOpt != "" {
		found := false
		for _, opt := range options {
			if opt.ID == selectOpt {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "Error: option '%s' not found\n", selectOpt)
			fmt.Fprintf(os.Stderr, "Available options:\n")
			for _, opt := range options {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", opt.ID, opt.Label)
			}
			os.Exit(1)
		}
	}

	// Update the decision point
	now := time.Now()
	dp.RespondedAt = &now
	dp.RespondedBy = respondedBy
	if selectOpt != "" {
		dp.SelectedOption = selectOpt
	}
	if textResponse != "" {
		dp.ResponseText = textResponse
	}

	if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating decision point: %v\n", err)
		os.Exit(1)
	}

	// Determine next action based on response type
	shouldCloseGate := selectOpt != "" || acceptGuidance
	shouldIterate := textResponse != "" && selectOpt == "" && !acceptGuidance

	if shouldCloseGate {
		// Close the gate issue
		reason := "Decision resolved"
		if selectOpt != "" {
			// Find the label for the selected option
			for _, opt := range options {
				if opt.ID == selectOpt {
					reason = fmt.Sprintf("Selected: %s", opt.Label)
					break
				}
			}
		} else if acceptGuidance {
			reason = "Guidance accepted"
		}

		if err := store.CloseIssue(ctx, resolvedID, reason, actor, ""); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing gate: %v\n", err)
			os.Exit(1)
		}
	}

	markDirtyAndScheduleFlush()

	// Output
	if jsonOutput {
		result := map[string]interface{}{
			"id":              resolvedID,
			"selected_option": selectOpt,
			"response_text":   textResponse,
			"responded_by":    respondedBy,
			"responded_at":    now.Format(time.RFC3339),
			"gate_closed":     shouldCloseGate,
			"iteration":       shouldIterate,
		}
		outputJSON(result)
		return
	}

	// Human-readable output
	fmt.Printf("%s Decision response recorded: %s\n\n", ui.RenderPass("✓"), ui.RenderID(resolvedID))
	fmt.Printf("  Prompt: %s\n", dp.Prompt)

	if selectOpt != "" {
		for _, opt := range options {
			if opt.ID == selectOpt {
				fmt.Printf("  Selected: [%s] %s\n", opt.ID, opt.Label)
				break
			}
		}
	}

	if textResponse != "" {
		fmt.Printf("  Text: %s\n", textResponse)
	}

	if respondedBy != "" {
		fmt.Printf("  By: %s\n", respondedBy)
	}

	fmt.Println()

	if shouldCloseGate {
		fmt.Printf("  %s Gate closed - blocked issues now unblocked\n", ui.RenderPass("✓"))
	} else if shouldIterate {
		// TODO: Implement iteration trigger (hq-946577.23)
		fmt.Printf("  %s Text-only response (iteration not yet implemented)\n", ui.RenderWarn("⚠"))
		fmt.Printf("  Use --accept-guidance to proceed with this guidance directly\n")
	}
}
