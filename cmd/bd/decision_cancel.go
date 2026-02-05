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
	"github.com/steveyegge/beads/internal/utils"
)

// decisionCancelCmd cancels a pending decision point without a response
var decisionCancelCmd = &cobra.Command{
	Use:   "cancel <decision-id>",
	Short: "Cancel a pending decision point",
	Long: `Cancel a decision point without providing a response.

This closes the decision gate with a 'canceled' status. Any issues blocked
by this decision will be unblocked and can see that the decision was canceled
rather than answered.

Use this when a decision is no longer needed (e.g., the approach changed,
or the work was deprioritized).

Examples:
  # Cancel a decision
  bd decision cancel gt-abc123.decision-1

  # Cancel with a reason
  bd decision cancel gt-abc123.decision-1 --reason="Approach changed, using different architecture"

  # Cancel by a specific user
  bd decision cancel gt-abc123.decision-1 --reason="No longer needed" --by=admin@example.com`,
	Args: cobra.ExactArgs(1),
	Run:  runDecisionCancel,
}

func init() {
	decisionCancelCmd.Flags().StringP("reason", "r", "", "Reason for cancellation")
	decisionCancelCmd.Flags().String("by", "", "Who canceled the decision (email, user ID)")

	decisionCmd.AddCommand(decisionCancelCmd)
}

func runDecisionCancel(cmd *cobra.Command, args []string) {
	CheckReadonly("decision cancel")

	decisionID := args[0]
	reason, _ := cmd.Flags().GetString("reason")
	canceledBy, _ := cmd.Flags().GetString("by")

	// Use daemon if available
	if daemonClient != nil {
		cancelViaDaemon(decisionID, reason, canceledBy)
		return
	}

	// Direct mode
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

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
	if issue.IssueType != types.IssueType("gate") || issue.AwaitType != "decision" {
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
			resolvedID, dp.RespondedAt.Format("2006-01-02 15:04"), dp.RespondedBy)
		if dp.SelectedOption != "" {
			fmt.Fprintf(os.Stderr, "  Selected: %s\n", dp.SelectedOption)
		}
		os.Exit(1)
	}

	// Update decision point to mark as canceled
	now := time.Now()
	dp.RespondedAt = &now
	dp.RespondedBy = canceledBy
	dp.SelectedOption = "_canceled"
	if reason != "" {
		dp.ResponseText = reason
	}

	if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating decision point: %v\n", err)
		os.Exit(1)
	}

	// Close the gate issue
	closeReason := "Decision canceled"
	if reason != "" {
		closeReason = fmt.Sprintf("Decision canceled: %s", reason)
	}

	if err := store.CloseIssue(ctx, resolvedID, closeReason, actor, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing gate: %v\n", err)
		os.Exit(1)
	}

	markDirtyAndScheduleFlush()

	printCancelResult(resolvedID, reason, canceledBy, now.Format(time.RFC3339), dp.Prompt)
}

// cancelViaDaemon cancels a decision via the RPC daemon
func cancelViaDaemon(decisionID, reason, canceledBy string) {
	// Resolve ID via daemon
	resolveResp, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: decisionID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var resolvedID string
	if resolveResp != nil && resolveResp.Data != nil {
		_ = json.Unmarshal(resolveResp.Data, &resolvedID)
	}
	if resolvedID == "" {
		resolvedID = decisionID
	}

	result, err := daemonClient.DecisionCancel(&rpc.DecisionCancelArgs{
		IssueID:    resolvedID,
		Reason:     reason,
		CanceledBy: canceledBy,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	printCancelResult(result.IssueID, result.Reason, result.CanceledBy, result.CanceledAt, result.Prompt)
}

func printCancelResult(id, reason, canceledBy, canceledAt, prompt string) {
	if jsonOutput {
		result := map[string]interface{}{
			"id":          id,
			"status":      "canceled",
			"reason":      reason,
			"canceled_by": canceledBy,
			"canceled_at": canceledAt,
			"prompt":      prompt,
		}
		outputJSON(result)
		return
	}

	fmt.Printf("%s Decision canceled: %s\n\n", ui.RenderPass("✓"), ui.RenderID(id))
	fmt.Printf("  Prompt: %s\n", prompt)

	if reason != "" {
		fmt.Printf("  Reason: %s\n", reason)
	}

	if canceledBy != "" {
		fmt.Printf("  Canceled by: %s\n", canceledBy)
	}

	fmt.Printf("\n  %s Gate closed - blocked issues now unblocked (decision: canceled)\n", ui.RenderPass("✓"))
}
