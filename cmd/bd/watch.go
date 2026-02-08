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

  # General-purpose matching (AND logic)
  bd watch --match type=create,issue_type=gate --timeout=30s

  # Match with contains operator
  bd watch --match title~=deploy --timeout=5m

  # Stream all mutations (raw mode, for debugging)
  bd watch --raw

  # Stream with server-side type filter
  bd watch --raw --type=update

Exit codes:
  0 - Condition met (or raw mode ended normally)
  1 - Timeout reached
  2 - Canceled`,
	RunE: runWatch,
}

var (
	watchIssue       string
	watchDecision    string
	watchUntilStatus string
	watchTimeout     time.Duration
	watchRaw         bool
	watchJSON        bool
	watchType        string
	watchSince       int64
	watchMatch       string
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
	watchCmd.Flags().StringVar(&watchMatch, "match", "", "General-purpose match expression (key=value,key=value; AND logic, ~= for contains)")

	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	// Raw mode: stream all events directly
	if watchRaw {
		return runWatchRawMode()
	}

	// Decision mode: uses shared awaitDecision with initial-state check
	if watchDecision != "" {
		return runWatchDecisionMode()
	}

	// General --match mode or sugar flags compile to matcher
	matcher, err := buildMatcher()
	if err != nil {
		return err
	}

	// If no matcher and no flags, stream everything (same as --raw)
	if matcher.IsEmpty() && watchIssue == "" && watchType == "" {
		return runWatchRawMode()
	}

	if daemonClient == nil {
		return fmt.Errorf("no daemon connection (bd watch requires a running daemon)")
	}

	ctx, cancel := context.WithTimeout(rootCtx, watchTimeout)
	defer cancel()

	result, err := awaitEvent(ctx, daemonClient, AwaitOpts{
		Matcher: matcher,
		Timeout: watchTimeout,
		JSON:    watchJSON,
	})
	if err != nil {
		return err
	}

	outputResult(result)
	if result.TimedOut {
		os.Exit(1)
	}
	if result.Canceled {
		os.Exit(2)
	}
	return nil
}

// buildMatcher compiles sugar flags and --match into a single EventMatcher.
func buildMatcher() (*EventMatcher, error) {
	// Start with explicit --match expression
	matcher, err := ParseMatcher(watchMatch)
	if err != nil {
		return nil, fmt.Errorf("invalid --match expression: %w", err)
	}

	// Sugar: --issue compiles to issue_id= condition
	if watchIssue != "" {
		matcher.Conditions = append(matcher.Conditions,
			MatchCondition{Field: "issue_id", Value: watchIssue, Op: OpEqual})
	}

	// Sugar: --until-status compiles to new_status= condition
	if watchUntilStatus != "" {
		matcher.Conditions = append(matcher.Conditions,
			MatchCondition{Field: "new_status", Value: watchUntilStatus, Op: OpEqual})
	}

	// Sugar: --type compiles to type= condition
	if watchType != "" {
		matcher.Conditions = append(matcher.Conditions,
			MatchCondition{Field: "type", Value: watchType, Op: OpEqual})
	}

	return matcher, nil
}

// runWatchDecisionMode handles --decision with initial-state check and enrichment.
func runWatchDecisionMode() error {
	if daemonClient == nil {
		return fmt.Errorf("no daemon connection (bd watch --decision requires a running daemon)")
	}

	ctx := rootCtx
	result, exitCode := awaitDecision(ctx, daemonClient, watchDecision, watchTimeout)
	if result == nil {
		fmt.Fprintf(os.Stderr, "Error waiting for decision %s\n", watchDecision)
		os.Exit(3)
	}

	outputResult(result)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

// runWatchRawMode streams all events to stdout (no condition matching).
func runWatchRawMode() error {
	baseURL, token, err := resolveSSEEndpoint()
	if err != nil {
		return fmt.Errorf("cannot connect to SSE endpoint: %w", err)
	}

	ctx, cancel := context.WithTimeout(rootCtx, watchTimeout)
	defer cancel()

	since := watchSince
	if since == 0 {
		since = time.Now().UnixMilli()
	}

	// Build optional server-side filter
	var filter string
	if watchType != "" {
		filter = "type:" + watchType
	}

	events, errs := rpc.ConnectSSE(ctx, rpc.SSEClientOptions{
		BaseURL: baseURL,
		Token:   token,
		Since:   since,
		Filter:  filter,
	})

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
			return nil // timeout or cancel is normal for raw mode
		}
	}
}

// outputResult writes a WatchResult as JSON to stdout.
func outputResult(result *WatchResult) {
	data, _ := json.Marshal(result)
	fmt.Println(string(data))
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
