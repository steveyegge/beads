package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/ui"
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

	requireDaemon("decision cancel")
	cancelViaDaemon(decisionID, reason, canceledBy)
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
