package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
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
	decisionID := args[0]

	requireDaemon("decision await")
	runDecisionAwaitDaemon(daemonClient, decisionID)
}

// runDecisionAwaitDaemon uses the shared awaitDecision loop (SSE+polling)
// for decision resolution via daemon.
func runDecisionAwaitDaemon(client *rpc.Client, decisionID string) {
	// Resolve partial ID via daemon
	resp, err := client.ResolveID(&rpc.ResolveIDArgs{ID: decisionID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving ID: %v\n", err)
		os.Exit(3)
	}
	var resolvedID string
	if resp.Data != nil {
		_ = json.Unmarshal(resp.Data, &resolvedID)
	}
	if resolvedID == "" {
		resolvedID = decisionID
	}

	// Validate it's a decision gate before waiting
	dr, err := client.DecisionGet(&rpc.DecisionGetArgs{IssueID: resolvedID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting decision: %v\n", err)
		os.Exit(3)
	}
	if dr.Decision == nil {
		fmt.Fprintf(os.Stderr, "Error: no decision point data for %s\n", resolvedID)
		os.Exit(3)
	}
	if dr.Issue != nil && (dr.Issue.IssueType != types.IssueType("gate") || dr.Issue.AwaitType != "decision") {
		fmt.Fprintf(os.Stderr, "Error: %s is not a decision point\n", resolvedID)
		os.Exit(3)
	}

	fmt.Fprintf(os.Stderr, "Waiting for response to %s (timeout: %s, via daemon)...\n", resolvedID, awaitTimeout)

	// Delegate to shared awaitDecision (SSE-first with polling fallback,
	// includes initial-state check for already-responded decisions)
	result, exitCode := awaitDecision(rootCtx, client, resolvedID, awaitTimeout)
	if result == nil {
		fmt.Fprintf(os.Stderr, "Error waiting for decision %s\n", resolvedID)
		os.Exit(3)
	}

	// Convert WatchResult to AwaitResponse for backward compatibility
	awaitResp := AwaitResponse{
		ID:       resolvedID,
		TimedOut: result.TimedOut,
		Canceled: result.Canceled,
	}
	if result.Decision != nil {
		awaitResp.Selected = result.Decision.Selected
		awaitResp.Text = result.Decision.Reason
		awaitResp.RespondedBy = result.Decision.RespondedBy
		awaitResp.RespondedAt = result.Decision.RespondedAt
	}
	outputJSON(awaitResp)

	if result.TimedOut {
		fmt.Fprintf(os.Stderr, "Timeout waiting for response\n")
	}

	os.Exit(exitCode)
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
