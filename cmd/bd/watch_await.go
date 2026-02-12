package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/steveyegge/beads/internal/daemon"
	"github.com/steveyegge/beads/internal/eventbus"
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
	Selected      string `json:"selected,omitempty"`
	SelectedLabel string `json:"selected_label,omitempty"`
	RespondedBy   string `json:"responded_by,omitempty"`
	Reason        string `json:"reason,omitempty"`
	RespondedAt   string `json:"responded_at,omitempty"`
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
// Transport preference depends on daemon location:
//   - Remote daemon (HTTP): SSE first (JetStream-backed on server), then polling
//   - Local daemon: NATS JetStream first, then SSE, then polling
//
// Returns the matching result, or a timeout/canceled result.
func awaitEvent(ctx context.Context, client *rpc.Client, opts AwaitOpts) (*WatchResult, error) {
	if isRemoteDaemon() {
		// Remote daemon: SSE is the primary path (server relays from JetStream).
		// Direct NATS requires port-forwarding and isn't practical for CLI users.
		baseURL, token, sseErr := resolveSSEEndpoint()
		if sseErr == nil {
			return awaitEventSSE(ctx, client, baseURL, token, opts)
		}
		// Fall back to polling for remote.
		return awaitEventPolling(ctx, client, opts)
	}

	// Local/in-cluster: try NATS direct first (lowest latency).
	nc, js, natsErr := connectWatchNATS()
	if natsErr == nil {
		defer nc.Close()
		return awaitEventNATS(ctx, client, js, opts)
	}

	// Try SSE.
	baseURL, token, sseErr := resolveSSEEndpoint()
	if sseErr == nil {
		return awaitEventSSE(ctx, client, baseURL, token, opts)
	}

	// Fall back to polling.
	return awaitEventPolling(ctx, client, opts)
}

// connectWatchNATS resolves the NATS URL and connects.
// Resolution order: BD_NATS_URL env > daemon query > BD_NATS_PORT env > localhost:4222.
func connectWatchNATS() (*nats.Conn, nats.JetStreamContext, error) {
	var natsURL string

	if envURL := os.Getenv("BD_NATS_URL"); envURL != "" {
		natsURL = envURL
	} else {
		resp, err := daemonClient.Execute(rpc.OpBusStatus, nil)
		if err == nil && resp.Success {
			var result rpc.BusStatusResult
			if err := json.Unmarshal(resp.Data, &result); err == nil && result.NATSEnabled {
				natsURL = fmt.Sprintf("nats://127.0.0.1:%d", result.NATSPort)
			}
		}
	}

	// If daemon reports NATS disabled and no explicit URL, fail fast.
	if natsURL == "" {
		if os.Getenv("BD_NATS_URL") == "" && os.Getenv("BD_NATS_PORT") == "" {
			return nil, nil, fmt.Errorf("NATS not enabled on daemon")
		}
		port := os.Getenv("BD_NATS_PORT")
		if port == "" {
			port = fmt.Sprintf("%d", daemon.DefaultNATSPort)
		}
		natsURL = fmt.Sprintf("nats://127.0.0.1:%s", port)
	}

	natsToken := os.Getenv("BD_DAEMON_TOKEN")

	connectOpts := []nats.Option{
		nats.Name("bd-watch"),
		nats.Timeout(2 * time.Second),  // Connect timeout — fail fast to SSE/polling fallback
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(3),
	}
	if natsToken != "" {
		connectOpts = append(connectOpts, nats.Token(natsToken))
	}

	// WebSocket URL path extraction (same pattern as bus_subscribe.go).
	if strings.HasPrefix(natsURL, "ws://") || strings.HasPrefix(natsURL, "wss://") {
		if u, err := url.Parse(natsURL); err == nil && u.Path != "" && u.Path != "/" {
			connectOpts = append(connectOpts, nats.ProxyPath(u.Path))
			u.Path = ""
			natsURL = u.String()
		}
	}

	nc, err := nats.Connect(natsURL, connectOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to NATS: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("JetStream context: %w", err)
	}

	return nc, js, nil
}

// payloadToMutationEvent converts a NATS MutationEventPayload to an rpc.MutationEvent.
func payloadToMutationEvent(p eventbus.MutationEventPayload) rpc.MutationEvent {
	ts, _ := time.Parse(time.RFC3339Nano, p.Timestamp)
	return rpc.MutationEvent{
		Type:      p.Type,
		IssueID:   p.IssueID,
		Title:     p.Title,
		Assignee:  p.Assignee,
		Actor:     p.Actor,
		Timestamp: ts,
		OldStatus: p.OldStatus,
		NewStatus: p.NewStatus,
		ParentID:  p.ParentID,
		IssueType: p.IssueType,
		Labels:    p.Labels,
		AwaitType: p.AwaitType,
	}
}

// watchMutationSubject maps a mutation type string (e.g. "create") to the
// NATS subject suffix (e.g. "MutationCreate"). Returns "" if unknown.
func watchMutationSubject(mutType string) string {
	switch mutType {
	case rpc.MutationCreate:
		return string(eventbus.EventMutationCreate)
	case rpc.MutationUpdate:
		return string(eventbus.EventMutationUpdate)
	case rpc.MutationDelete:
		return string(eventbus.EventMutationDelete)
	case rpc.MutationComment:
		return string(eventbus.EventMutationComment)
	case rpc.MutationStatus:
		return string(eventbus.EventMutationStatus)
	default:
		return ""
	}
}

// awaitEventNATS watches via NATS JetStream subscription.
func awaitEventNATS(ctx context.Context, client *rpc.Client, js nats.JetStreamContext, opts AwaitOpts) (*WatchResult, error) {
	// Build subject filter from matcher conditions.
	// If there's an exact type match, subscribe to that specific subject.
	// Otherwise subscribe to all mutations.
	subject := eventbus.SubjectMutationPrefix + ">"
	for _, c := range opts.Matcher.Conditions {
		if c.Field == "type" && c.Op == OpEqual {
			if suffix := watchMutationSubject(c.Value); suffix != "" {
				subject = eventbus.SubjectMutationPrefix + suffix
			}
			break
		}
	}

	// Channel to receive matching events.
	matchCh := make(chan rpc.MutationEvent, 1)

	sub, err := js.Subscribe(subject, func(msg *nats.Msg) {
		var payload eventbus.MutationEventPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			_ = msg.Ack()
			return
		}

		evt := payloadToMutationEvent(payload)

		if !opts.Matcher.IsEmpty() && !opts.Matcher.Matches(evt) {
			_ = msg.Ack()
			return
		}

		// Non-blocking send — first match wins.
		select {
		case matchCh <- evt:
		default:
		}
		_ = msg.Ack()
	}, nats.DeliverNew(), nats.AckExplicit())
	if err != nil {
		return nil, fmt.Errorf("subscribe to %s: %w", subject, err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	select {
	case evt := <-matchCh:
		result := &WatchResult{
			Matched: true,
			Event:   toJSON(evt),
		}
		if opts.DecisionID != "" && client != nil {
			enrichDecision(client, opts.DecisionID, result)
		}
		return result, nil

	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return &WatchResult{TimedOut: true}, nil
		}
		return &WatchResult{Canceled: true}, nil
	}
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
	// Map selected option ID to its label
	selectedLabel := dp.SelectedOption
	if dp.Options != "" {
		var options []types.DecisionOption
		if err := json.Unmarshal([]byte(dp.Options), &options); err == nil {
			for _, opt := range options {
				if opt.ID == dp.SelectedOption {
					selectedLabel = opt.Label
					break
				}
			}
		}
	}

	result.Decision = &DecisionDetail{
		Selected:      dp.SelectedOption,
		SelectedLabel: selectedLabel,
		RespondedBy:   dp.RespondedBy,
		Reason:        dp.ResponseText,
	}
	if dp.RespondedAt != nil {
		result.Decision.RespondedAt = dp.RespondedAt.Format(time.RFC3339)
	}
}
