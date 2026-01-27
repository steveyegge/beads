package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// decisionAwaitCmd waits for a decision point to be responded
var decisionAwaitCmd = &cobra.Command{
	Use:   "await <decision-id>",
	Short: "Wait for a decision point response (blocking)",
	Long: `Wait for a decision point to receive a response.

This command blocks until the decision is responded to, times out, or is canceled.
Useful for scripts and Claude Code integration where you need to wait for human input.

Exit codes:
  0 - Decision was responded to
  1 - Timeout reached without response
  2 - Decision was canceled
  3 - Error occurred

The response is output as JSON for easy parsing:
  {"selected": "y", "text": "optional guidance", "responded_by": "user@example.com"}

Examples:
  # Wait up to 5 minutes for response
  bd decision await gt-abc.decision-1 --timeout 5m

  # Create and wait in one flow
  id=$(bd decision create --prompt="Deploy?" --options='[{"id":"y","label":"Yes"}]' --json | jq -r .id)
  response=$(bd decision await $id --timeout 5m)
  echo "Selected: $(echo $response | jq -r .selected)"

  # With shorter poll interval for faster response
  bd decision await $id --timeout 2m --poll-interval 1s`,
	Args: cobra.ExactArgs(1),
	Run:  runDecisionAwait,
}

var (
	awaitTimeout      time.Duration
	awaitPollInterval time.Duration
)

func init() {
	decisionAwaitCmd.Flags().DurationVar(&awaitTimeout, "timeout", 5*time.Minute, "Maximum time to wait for response")
	decisionAwaitCmd.Flags().DurationVar(&awaitPollInterval, "poll-interval", 2*time.Second, "How often to check for response")

	decisionCmd.AddCommand(decisionAwaitCmd)
}

// AwaitResponse is the JSON output when a decision is responded
type AwaitResponse struct {
	ID          string `json:"id"`
	Selected    string `json:"selected,omitempty"`
	Text        string `json:"text,omitempty"`
	RespondedBy string `json:"responded_by,omitempty"`
	RespondedAt string `json:"responded_at,omitempty"`
	Canceled   bool   `json:"canceled,omitempty"`
	TimedOut    bool   `json:"timed_out,omitempty"`
}

func runDecisionAwait(cmd *cobra.Command, args []string) {
	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(3)
	}

	decisionID := args[0]
	ctx := rootCtx

	// Resolve partial ID
	resolvedID, err := utils.ResolvePartialID(ctx, store, decisionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(3)
	}

	// Verify it's a decision gate
	issue, err := store.GetIssue(ctx, resolvedID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting issue: %v\n", err)
		os.Exit(3)
	}
	if issue == nil {
		fmt.Fprintf(os.Stderr, "Error: issue %s not found\n", resolvedID)
		os.Exit(3)
	}
	if issue.IssueType != types.TypeGate || issue.AwaitType != "decision" {
		fmt.Fprintf(os.Stderr, "Error: %s is not a decision point\n", resolvedID)
		os.Exit(3)
	}

	// Check if already responded
	dp, err := store.GetDecisionPoint(ctx, resolvedID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting decision point: %v\n", err)
		os.Exit(3)
	}
	if dp == nil {
		fmt.Fprintf(os.Stderr, "Error: no decision point data for %s\n", resolvedID)
		os.Exit(3)
	}

	// If already responded, return immediately
	if dp.RespondedAt != nil {
		outputAwaitResponse(resolvedID, dp, false)
		os.Exit(0)
	}

	// Check if already canceled/closed
	if issue.Status == types.StatusClosed {
		resp := AwaitResponse{
			ID:        resolvedID,
			Canceled: true,
		}
		outputJSON(resp)
		os.Exit(2)
	}

	// Start polling
	deadline := time.Now().Add(awaitTimeout)
	ticker := time.NewTicker(awaitPollInterval)
	defer ticker.Stop()

	fmt.Fprintf(os.Stderr, "Waiting for response to %s (timeout: %s)...\n", resolvedID, awaitTimeout)

	for {
		select {
		case <-ticker.C:
			// Refresh the decision point
			dp, err = store.GetDecisionPoint(ctx, resolvedID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error polling decision: %v\n", err)
				continue
			}

			// Check if responded
			if dp.RespondedAt != nil {
				outputAwaitResponse(resolvedID, dp, false)
				os.Exit(0)
			}

			// Check if canceled
			issue, _ = store.GetIssue(ctx, resolvedID)
			if issue != nil && issue.Status == types.StatusClosed {
				resp := AwaitResponse{
					ID:        resolvedID,
					Canceled: true,
				}
				outputJSON(resp)
				os.Exit(2)
			}

			// Check timeout
			if time.Now().After(deadline) {
				resp := AwaitResponse{
					ID:       resolvedID,
					TimedOut: true,
				}
				outputJSON(resp)
				fmt.Fprintf(os.Stderr, "Timeout waiting for response\n")
				os.Exit(1)
			}

		case <-ctx.Done():
			// Context canceled
			resp := AwaitResponse{
				ID:        resolvedID,
				Canceled: true,
			}
			outputJSON(resp)
			os.Exit(2)
		}
	}
}

func outputAwaitResponse(id string, dp *types.DecisionPoint, timedOut bool) {
	resp := AwaitResponse{
		ID:       id,
		Selected: dp.SelectedOption,
		Text:     dp.ResponseText,
		TimedOut: timedOut,
	}
	if dp.RespondedBy != "" {
		resp.RespondedBy = dp.RespondedBy
	}
	if dp.RespondedAt != nil {
		resp.RespondedAt = dp.RespondedAt.Format(time.RFC3339)
	}
	outputJSON(resp)
}
