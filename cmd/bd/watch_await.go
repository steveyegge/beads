package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// WatchResult is the unified JSON output for bd watch.
type WatchResult struct {
	Matched  bool             `json:"matched"`
	TimedOut bool             `json:"timed_out,omitempty"`
	Canceled bool             `json:"canceled,omitempty"`
	Event    *MutationEventJSON `json:"event,omitempty"`
	// Decision-specific enrichment
	Decision *DecisionDetail `json:"decision,omitempty"`
}

// MutationEventJSON is the JSON-serializable form of a MutationEvent.
type MutationEventJSON struct {
	Type      string   `json:"type"`
	IssueID   string   `json:"issue_id"`
	Title     string   `json:"title,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Actor     string   `json:"actor,omitempty"`
	OldStatus string   `json:"old_status,omitempty"`
	NewStatus string   `json:"new_status,omitempty"`
	ParentID  string   `json:"parent_id,omitempty"`
	IssueType string   `json:"issue_type,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	AwaitType string   `json:"await_type,omitempty"`
	Timestamp string   `json:"timestamp"`
}

// DecisionDetail contains decision-specific enrichment when watching decisions.
type DecisionDetail struct {
	Selected    string `json:"selected,omitempty"`
	RespondedBy string `json:"responded_by,omitempty"`
	Reason      string `json:"reason,omitempty"`
	RespondedAt string `json:"responded_at,omitempty"`
}

// AwaitOpts configures the await loop.
type AwaitOpts struct {
	Matcher      *EventMatcher
	Timeout      time.Duration
	PollInterval time.Duration
	DecisionID   string // If set, enables decision enrichment + initial-state check
	JSON         bool   // Output format
}

// toJSON converts a MutationEvent to its JSON-serializable form.
func toJSON(evt rpc.MutationEvent) *MutationEventJSON {
	return &MutationEventJSON{
		Type:      evt.Type,
		IssueID:   evt.IssueID,
		Title:     evt.Title,
		Assignee:  evt.Assignee,
		Actor:     evt.Actor,
		OldStatus: evt.OldStatus,
		NewStatus: evt.NewStatus,
		ParentID:  evt.ParentID,
		IssueType: evt.IssueType,
		Labels:    evt.Labels,
		AwaitType: evt.AwaitType,
		Timestamp: evt.Timestamp.Format(time.RFC3339),
	}
}

// awaitEvent watches for a mutation event matching the given conditions.
// Uses SSE if an HTTP endpoint is available, otherwise falls back to polling.
// Returns the matching result, or a timeout/canceled result.
func awaitEvent(ctx context.Context, client *rpc.Client, opts AwaitOpts) (*WatchResult, error) {
	// Try SSE-based watching first
	baseURL, token, sseErr := resolveSSEEndpoint()
	if sseErr == nil {
		return awaitEventSSE(ctx, client, baseURL, token, opts)
	}

	// SSE not available — fall back to polling
	return awaitEventPolling(ctx, client, opts)
}

// awaitEventSSE watches via SSE stream.
func awaitEventSSE(ctx context.Context, client *rpc.Client, baseURL, token string, opts AwaitOpts) (*WatchResult, error) {
	// Build SSE filter for server-side pre-filtering
	var filter string
	if opts.DecisionID != "" {
		filter = "issue:" + opts.DecisionID
	} else {
		// Use first issue_id condition as server-side filter if available
		for _, c := range opts.Matcher.Conditions {
			if c.Field == "issue_id" && c.Op == OpEqual {
				filter = "issue:" + c.Value
				break
			}
		}
		// Use first type condition as server-side filter
		if filter == "" {
			for _, c := range opts.Matcher.Conditions {
				if c.Field == "type" && c.Op == OpEqual {
					filter = "type:" + c.Value
					break
				}
			}
		}
	}

	events, errs := rpc.ConnectSSE(ctx, rpc.SSEClientOptions{
		BaseURL: baseURL,
		Token:   token,
		Since:   time.Now().UnixMilli(),
		Filter:  filter,
	})

	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("SSE connection closed")
			}

			if !opts.Matcher.IsEmpty() && !opts.Matcher.Matches(evt.Data) {
				continue
			}

			result := &WatchResult{
				Matched: true,
				Event:   toJSON(evt.Data),
			}

			// Decision enrichment
			if opts.DecisionID != "" && client != nil {
				enrichDecision(client, opts.DecisionID, result)
			}

			return result, nil

		case err := <-errs:
			if err != nil {
				return nil, err
			}

		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return &WatchResult{TimedOut: true}, nil
			}
			return &WatchResult{Canceled: true}, nil
		}
	}
}

// awaitEventPolling watches via periodic RPC polling.
func awaitEventPolling(ctx context.Context, client *rpc.Client, opts AwaitOpts) (*WatchResult, error) {
	interval := opts.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	since := time.Now().UnixMilli()

	for {
		select {
		case <-ticker.C:
			resp, err := client.Execute(rpc.OpGetMutations, &rpc.GetMutationsArgs{Since: since})
			if err != nil {
				fmt.Fprintf(os.Stderr, "poll error: %v\n", err)
				continue
			}

			var mutations []rpc.MutationEvent
			if err := json.Unmarshal(resp.Data, &mutations); err != nil {
				continue
			}

			for _, evt := range mutations {
				if !opts.Matcher.IsEmpty() && !opts.Matcher.Matches(evt) {
					continue
				}

				result := &WatchResult{
					Matched: true,
					Event:   toJSON(evt),
				}

				if opts.DecisionID != "" && client != nil {
					enrichDecision(client, opts.DecisionID, result)
				}

				return result, nil
			}

			// Update since marker for next poll
			if len(mutations) > 0 {
				since = mutations[len(mutations)-1].Timestamp.UnixMilli()
			}

		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return &WatchResult{TimedOut: true}, nil
			}
			return &WatchResult{Canceled: true}, nil
		}
	}
}

// awaitDecision checks initial state then delegates to awaitEvent.
// Returns the WatchResult and a suggested exit code (0=matched, 1=timeout, 2=canceled).
func awaitDecision(ctx context.Context, client *rpc.Client, decisionID string, timeout time.Duration) (*WatchResult, int) {
	// Initial state check — if already responded, return immediately
	dr, err := client.DecisionGet(&rpc.DecisionGetArgs{IssueID: decisionID})
	if err != nil {
		return nil, 3 // error
	}
	if dr.Decision != nil && dr.Decision.RespondedAt != nil {
		result := &WatchResult{
			Matched: true,
			Event: &MutationEventJSON{
				Type:    rpc.MutationUpdate,
				IssueID: decisionID,
			},
		}
		enrichDecisionFromDP(dr.Decision, result)
		return result, 0
	}

	// Check if already canceled/closed
	if dr.Issue != nil && dr.Issue.Status == types.StatusClosed {
		return &WatchResult{Canceled: true, Event: &MutationEventJSON{IssueID: decisionID}}, 2
	}

	// Build matcher for any mutation on this decision's issue
	matcher := &EventMatcher{
		Conditions: []MatchCondition{
			{Field: "issue_id", Value: decisionID, Op: OpEqual},
		},
	}

	opts := AwaitOpts{
		Matcher:    matcher,
		Timeout:    timeout,
		DecisionID: decisionID,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, awaitErr := awaitEvent(timeoutCtx, client, opts)
	if awaitErr != nil {
		// SSE/polling error — fall back to simple poll
		return awaitDecisionPoll(ctx, client, decisionID, timeout)
	}

	if result.TimedOut {
		return result, 1
	}
	if result.Canceled {
		return result, 2
	}
	return result, 0
}

// awaitDecisionPoll is a simple polling fallback for decision await.
func awaitDecisionPoll(ctx context.Context, client *rpc.Client, decisionID string, timeout time.Duration) (*WatchResult, int) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dr, err := client.DecisionGet(&rpc.DecisionGetArgs{IssueID: decisionID})
			if err != nil {
				continue
			}
			if dr.Decision != nil && dr.Decision.RespondedAt != nil {
				result := &WatchResult{
					Matched: true,
					Event:   &MutationEventJSON{Type: rpc.MutationUpdate, IssueID: decisionID},
				}
				enrichDecisionFromDP(dr.Decision, result)
				return result, 0
			}
			if dr.Issue != nil && dr.Issue.Status == types.StatusClosed {
				return &WatchResult{Canceled: true, Event: &MutationEventJSON{IssueID: decisionID}}, 2
			}
			if time.Now().After(deadline) {
				return &WatchResult{TimedOut: true, Event: &MutationEventJSON{IssueID: decisionID}}, 1
			}

		case <-ctx.Done():
			return &WatchResult{Canceled: true, Event: &MutationEventJSON{IssueID: decisionID}}, 2
		}
	}
}

// enrichDecision fetches decision state via RPC and populates the result.
func enrichDecision(client *rpc.Client, decisionID string, result *WatchResult) {
	dr, err := client.DecisionGet(&rpc.DecisionGetArgs{IssueID: decisionID})
	if err != nil || dr.Decision == nil {
		return
	}
	if dr.Decision.RespondedAt == nil {
		return // Not yet responded
	}
	enrichDecisionFromDP(dr.Decision, result)
}

// enrichDecisionFromDP populates decision details from a DecisionPoint.
func enrichDecisionFromDP(dp *types.DecisionPoint, result *WatchResult) {
	result.Decision = &DecisionDetail{
		Selected:    dp.SelectedOption,
		RespondedBy: dp.RespondedBy,
		Reason:      dp.ResponseText,
	}
	if dp.RespondedAt != nil {
		result.Decision.RespondedAt = dp.RespondedAt.Format(time.RFC3339)
	}
}
