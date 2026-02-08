package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch for real-time mutation events via SSE",
	Long: `Connect to the daemon's SSE endpoint and stream mutation events in real-time.

Blocks until a condition is met, then exits with the matching event as JSON.

Examples:
  # Watch for any mutation on a specific issue
  bd watch --issue=gt-abc --timeout=30m

  # Watch for an issue to reach a specific status
  bd watch --issue=gt-abc --until-status=closed --timeout=30m

  # Watch for a decision to be responded to
  bd watch --decision=gt-abc --timeout=30m

  # Stream all mutations (raw mode, for debugging)
  bd watch --raw

  # Stream with server-side type filter
  bd watch --raw --type=update

Exit codes:
  0 - Condition met (or raw mode ended normally)
  1 - Timeout reached
  2 - Error occurred`,
	RunE: runWatch,
}

var (
	watchIssue      string
	watchDecision   string
	watchUntilStatus string
	watchTimeout    time.Duration
	watchRaw        bool
	watchJSON       bool
	watchType       string
	watchSince      int64
)

func init() {
	watchCmd.Flags().StringVar(&watchIssue, "issue", "", "Watch for mutations on a specific issue ID")
	watchCmd.Flags().StringVar(&watchDecision, "decision", "", "Watch for a decision to be responded to")
	watchCmd.Flags().StringVar(&watchUntilStatus, "until-status", "", "Wait until issue reaches this status")
	watchCmd.Flags().DurationVar(&watchTimeout, "timeout", 30*time.Minute, "Maximum time to wait")
	watchCmd.Flags().BoolVar(&watchRaw, "raw", false, "Stream all events (no condition matching)")
	watchCmd.Flags().BoolVar(&watchJSON, "json", false, "Output as JSON")
	watchCmd.Flags().StringVar(&watchType, "type", "", "Filter by mutation type (create, update, delete, etc.)")
	watchCmd.Flags().Int64Var(&watchSince, "since", 0, "Replay events since this unix timestamp (ms)")

	rootCmd.AddCommand(watchCmd)
}

// WatchResult is the JSON output when a watch condition is met.
type WatchResult struct {
	Matched  bool   `json:"matched"`
	IssueID  string `json:"issue_id,omitempty"`
	Type     string `json:"type,omitempty"`
	Status   string `json:"status,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
	// Decision-specific fields (populated via follow-up RPC)
	Selected    string `json:"selected,omitempty"`
	RespondedBy string `json:"responded_by,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func runWatch(cmd *cobra.Command, args []string) error {
	// Determine SSE endpoint from daemon client
	baseURL, token, err := resolveSSEEndpoint()
	if err != nil {
		return fmt.Errorf("cannot connect to SSE endpoint: %w", err)
	}

	// Build filter
	var filter string
	if watchIssue != "" {
		filter = "issue:" + watchIssue
	} else if watchDecision != "" {
		filter = "issue:" + watchDecision
	} else if watchType != "" {
		filter = "type:" + watchType
	}

	ctx, cancel := context.WithTimeout(rootCtx, watchTimeout)
	defer cancel()

	since := watchSince
	if since == 0 {
		since = time.Now().UnixMilli()
	}

	events, errs := rpc.ConnectSSE(ctx, rpc.SSEClientOptions{
		BaseURL: baseURL,
		Token:   token,
		Since:   since,
		Filter:  filter,
	})

	if watchRaw {
		return runWatchRaw(ctx, events, errs)
	}

	if watchDecision != "" {
		return runWatchDecision(ctx, events, errs)
	}

	if watchIssue != "" {
		return runWatchIssue(ctx, events, errs)
	}

	// No specific target — stream everything (same as --raw)
	return runWatchRaw(ctx, events, errs)
}

func runWatchRaw(ctx context.Context, events <-chan rpc.SSEEvent, errs <-chan error) error {
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return nil
			}
			if watchJSON {
				data, _ := json.Marshal(evt.Data)
				fmt.Println(string(data))
			} else {
				fmt.Fprintf(os.Stdout, "[%s] %s %s %s\n",
					evt.Data.Timestamp.Format(time.RFC3339),
					evt.Data.Type,
					evt.Data.IssueID,
					evt.Data.Title)
			}
		case err := <-errs:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return nil // timeout is normal for raw mode
			}
			return nil
		}
	}
}

func runWatchIssue(ctx context.Context, events <-chan rpc.SSEEvent, errs <-chan error) error {
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return fmt.Errorf("SSE connection closed")
			}
			// Check until-status condition
			if watchUntilStatus != "" {
				if evt.Data.NewStatus == watchUntilStatus {
					result := WatchResult{
						Matched: true,
						IssueID: evt.Data.IssueID,
						Type:    evt.Data.Type,
						Status:  evt.Data.NewStatus,
					}
					outputWatchResult(result)
					return nil
				}
			} else {
				// Any mutation on the issue matches
				result := WatchResult{
					Matched: true,
					IssueID: evt.Data.IssueID,
					Type:    evt.Data.Type,
					Status:  evt.Data.NewStatus,
				}
				outputWatchResult(result)
				return nil
			}
		case err := <-errs:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				result := WatchResult{TimedOut: true, IssueID: watchIssue}
				outputWatchResult(result)
				os.Exit(1)
			}
			return nil
		}
	}
}

func runWatchDecision(ctx context.Context, events <-chan rpc.SSEEvent, errs <-chan error) error {
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return fmt.Errorf("SSE connection closed")
			}
			// A mutation on the decision issue — check if it's been responded to
			// by fetching the full decision state via RPC
			if daemonClient != nil {
				dr, err := daemonClient.DecisionGet(&rpc.DecisionGetArgs{IssueID: watchDecision})
				if err == nil && dr.Decision != nil && dr.Decision.RespondedAt != nil {
					result := WatchResult{
						Matched:     true,
						IssueID:     evt.Data.IssueID,
						Type:        evt.Data.Type,
						Selected:    dr.Decision.SelectedOption,
						RespondedBy: dr.Decision.RespondedBy,
						Reason:      dr.Decision.ResponseText,
					}
					outputWatchResult(result)
					return nil
				}
			}
		case err := <-errs:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				result := WatchResult{TimedOut: true, IssueID: watchDecision}
				outputWatchResult(result)
				os.Exit(1)
			}
			return nil
		}
	}
}

func outputWatchResult(result WatchResult) {
	if watchJSON || true { // always JSON for scriptability
		data, _ := json.Marshal(result)
		fmt.Println(string(data))
	}
}

// resolveSSEEndpoint determines the SSE base URL and token from the daemon client.
func resolveSSEEndpoint() (string, string, error) {
	if daemonClient == nil {
		return "", "", fmt.Errorf("no daemon connection (bd watch requires a running daemon with HTTP)")
	}

	// If the client wraps an HTTP client, use its base URL
	if hc := daemonClient.HTTPClient(); hc != nil {
		return hc.BaseURL(), hc.Token(), nil
	}

	// For local Unix socket daemon, construct localhost URL
	// Check if the daemon has an HTTP server running
	resp, err := daemonClient.Execute(rpc.OpStatus, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to get daemon status: %w", err)
	}

	var status rpc.StatusResponse
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		return "", "", fmt.Errorf("failed to parse status: %w", err)
	}

	// Look for HTTP address in status
	if status.HTTPAddr != "" {
		return "http://" + status.HTTPAddr, "", nil
	}

	return "", "", fmt.Errorf("daemon does not expose an HTTP endpoint (SSE requires HTTP)")
}
