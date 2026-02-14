package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// decisionStopCheckCmd checks whether Claude should be allowed to stop.
// It reads config from the claude-hooks config bead, creates a decision point,
// and polls until the human responds. Exit 0 = allow stop, exit 1 = block (continue).
var decisionStopCheckCmd = &cobra.Command{
	Use:   "stop-check",
	Short: "Check if Claude should stop (creates decision and awaits response)",
	Long: `Check whether Claude should be allowed to stop by creating a decision point
and waiting for a human response.

This command is called by the StopDecisionHandler in the event bus. It:
1. Reads the claude-hooks config bead for stop_decision settings
2. If disabled or no config, exits 0 (allow stop)
3. Creates a decision point asking the human to review
4. Polls until the human responds or timeout is reached

Exit codes:
  0 - Allow stop (human said stop, timeout, disabled, or error)
  1 - Block stop (human said continue) — stdout has JSON with reason

Examples:
  bd decision stop-check --json
  bd decision stop-check --timeout 30m --json`,
	Run: runDecisionStopCheck,
}

// stopDecisionConfig holds settings from the config bead.
type stopDecisionConfig struct {
	Enabled              bool                   `json:"enabled"`
	Timeout              string                 `json:"timeout"`
	PollInterval         string                 `json:"poll_interval"`
	Prompt               string                 `json:"prompt"`
	Options              []types.DecisionOption `json:"options"`
	DefaultAction        string                 `json:"default_action"`
	Urgency              string                 `json:"urgency"`
	RequireAgentDecision bool                   `json:"require_agent_decision"` // Agent must create decision before stopping
	RequireCloseOld      bool                   `json:"require_close_old"`      // Agent must close previous stop decisions before new
	RequireContext       bool                   `json:"require_context"`        // Decision must have non-empty Context field
	AgentDecisionPrompt  string                 `json:"agent_decision_prompt"`  // Custom instructions when agent must create decision
	AgentContextPrompt   string                 `json:"agent_context_prompt"`   // Custom instructions when context is missing
	AgentCloseOldPrompt  string                 `json:"agent_close_old_prompt"` // Custom instructions when old decisions need closing
}

func init() {
	decisionStopCheckCmd.Flags().Duration("timeout", 30*time.Minute, "Override timeout from config")
	decisionStopCheckCmd.Flags().Duration("poll-interval", 2*time.Second, "Override poll interval from config")
	decisionStopCheckCmd.Flags().Bool("reentry", false, "Indicates this is a re-entry (Claude tried to stop again after being blocked)")

	decisionCmd.AddCommand(decisionStopCheckCmd)
}

func runDecisionStopCheck(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	// Load config from config bead
	cfg := loadStopDecisionConfig(ctx)
	if cfg == nil || !cfg.Enabled {
		// Disabled or no config — allow stop
		if jsonOutput {
			outputJSON(map[string]string{"decision": "allow", "reason": "stop_decision disabled or not configured"})
		}
		os.Exit(0)
	}

	// -------------------------------------------------------------------------
	// Pure guard: no polling, no blocking. Just check state and return fast.
	//
	// The stop hook's only job is to ensure the agent creates a decision via
	// `bd decision create` (which blocks and waits for the human response).
	// This hook fires when the agent tries to stop:
	//
	//   1. No agent decision found → block with "create a decision"
	//   2. Agent decision found, still pending → allow (the agent is about to
	//      call or has called `bd decision create --wait`, which handles polling)
	//
	// After the agent receives a response (decision is no longer pending),
	// findPendingAgentDecision won't find it, so the next stop attempt blocks
	// again → agent creates a new decision → cycle repeats.
	// -------------------------------------------------------------------------

	if cfg.RequireAgentDecision {
		// Scope by actor name (human-readable) instead of session UUID.
		// This means all sessions from the same user share a decision pool,
		// which is the correct behavior for stop-check gating.
		actorTag := getActorWithGit()
		agentDecision, err := findPendingAgentDecision(ctx, actorTag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: error finding agent decision: %v\n", err)
			// Fail-open on error
			if jsonOutput {
				outputJSON(map[string]string{"decision": "allow", "reason": fmt.Sprintf("error: %v", err)})
			}
			os.Exit(0)
		}

		if agentDecision != nil {
			// Agent created a decision — allow stop. The agent's `bd decision
			// create --wait` call handles the actual waiting and returns the
			// human's response directly to the agent.
			fmt.Fprintf(os.Stderr, "Agent decision found: %s — allowing stop\n", agentDecision.IssueID)
			if jsonOutput {
				outputJSON(map[string]string{
					"decision":  "allow",
					"reason":    fmt.Sprintf("agent decision %s exists", agentDecision.IssueID),
					"source_id": agentDecision.IssueID,
				})
			}
			os.Exit(0)
		}

		// No agent decision — block and instruct agent to create one.
		reason := `Before stopping, create a decision point for the human to review.

Steps:
1. Run 'bd ready' and 'bd list --status=in_progress' to find open/available work
2. Summarize what you accomplished this session and what remains
3. Suggest 2-4 concrete next actions as decision options (based on open work, blockers, or logical next steps)
4. Create the decision with 'bd decision create' including:
   --context="<summary of session work and current state>"
   --options='[{"id":"...","short":"...","label":"<specific actionable option>"},...]'
   Always include a "stop" option: {"id":"stop","short":"stop","label":"Done for now"}

The human will pick which direction to go, or provide custom instructions.`
		if cfg.AgentDecisionPrompt != "" {
			reason = cfg.AgentDecisionPrompt
		}
		// Note: we no longer tell the agent to pass --requested-by with the session ID.
		// The actor name (from BD_ACTOR, git user.name, etc.) is used automatically
		// and produces a human-readable name in Slack notifications.
		fmt.Fprintf(os.Stderr, "No agent decision found. Blocking.\n")
		if jsonOutput {
			outputJSON(map[string]string{"decision": "block", "reason": reason})
		} else {
			fmt.Printf("Block: %s\n", reason)
		}
		os.Exit(1)
	}

	// No require_agent_decision — allow stop
	if jsonOutput {
		outputJSON(map[string]string{"decision": "allow", "reason": "require_agent_decision not set"})
	}
	os.Exit(0)
}

// loadStopDecisionConfig loads stop_decision settings from the claude-hooks config bead.
func loadStopDecisionConfig(ctx context.Context) *stopDecisionConfig {
	if daemonClient == nil {
		return nil
	}
	var mergedData map[string]interface{}

	// Use daemon to get merged config
	listArgs := &rpc.ListArgs{
		IssueType: "config",
		Labels:    []string{"config:claude-hooks"},
		Limit:     200,
	}
	resp, err := daemonClient.List(listArgs)
	if err != nil {
		return nil
	}

	var issues []*types.IssueWithCounts
	if resp.Data != nil {
		if err := json.Unmarshal(resp.Data, &issues); err != nil {
			return nil
		}
	}

	// Find the global config bead and extract metadata
	for _, iwc := range issues {
		if iwc.Issue.Metadata != nil {
			if err := json.Unmarshal(iwc.Issue.Metadata, &mergedData); err == nil {
				break
			}
		}
	}

	if mergedData == nil {
		return nil
	}

	// Extract stop_decision key
	sdRaw, ok := mergedData["stop_decision"]
	if !ok {
		return nil
	}

	// Re-marshal and unmarshal into our config struct
	sdBytes, err := json.Marshal(sdRaw)
	if err != nil {
		return nil
	}

	var cfg stopDecisionConfig
	if err := json.Unmarshal(sdBytes, &cfg); err != nil {
		return nil
	}

	return &cfg
}

// createStopDecision creates a decision point for the stop check.
func createStopDecision(_ context.Context, prompt string, options []types.DecisionOption, _, defaultOption string, _ time.Duration) (string, error) {
	if daemonClient == nil {
		return "", fmt.Errorf("no daemon client")
	}
	createArgs := &rpc.DecisionCreateArgs{
		Prompt:        prompt,
		Options:       options,
		DefaultOption: defaultOption,
		MaxIterations: 1,
		RequestedBy:   "stop-hook",
	}

	result, err := daemonClient.DecisionCreate(createArgs)
	if err != nil {
		return "", fmt.Errorf("daemon decision create: %w", err)
	}

	return result.Decision.IssueID, nil
}

// pollStopDecision waits for a decision response using NATS event bus if available,
// falling back to polling. Returns (selectedOption, responseText, error).
func pollStopDecision(ctx context.Context, decisionID string, timeout, pollInterval time.Duration) (string, string, error) {
	// Try event bus wake first — sub-second latency vs 2s polling.
	selected, text, err := waitForDecisionViaEventBus(ctx, decisionID, timeout)
	if err == nil {
		return selected, text, nil
	}
	// NATS unavailable — fall back to polling.
	fmt.Fprintf(os.Stderr, "NATS unavailable (%v), falling back to polling\n", err)
	return pollStopDecisionLoop(ctx, decisionID, timeout, pollInterval)
}

// pollStopDecisionLoop is the polling fallback when NATS is unavailable.
func pollStopDecisionLoop(ctx context.Context, decisionID string, timeout, pollInterval time.Duration) (string, string, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dp, selected, text, done, err := checkDecisionResponse(ctx, decisionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error polling decision: %v\n", err)
				continue
			}
			if done {
				return selected, text, nil
			}
			_ = dp // suppress unused

			if time.Now().After(deadline) {
				return "", "", nil // timeout — allow stop
			}

		case <-ctx.Done():
			return "", "", nil // context canceled — allow stop
		}
	}
}

// waitForDecisionViaEventBus subscribes to NATS JetStream for DecisionResponded
// events and waits for the specific decision ID. Returns an error if NATS is
// unavailable (caller should fall back to polling).
func waitForDecisionViaEventBus(ctx context.Context, decisionID string, timeout time.Duration) (string, string, error) {
	// Discover NATS port from daemon via BusStatus RPC.
	if daemonClient == nil {
		return "", "", fmt.Errorf("no daemon client")
	}

	resp, err := daemonClient.Execute(rpc.OpBusStatus, nil)
	if err != nil {
		return "", "", fmt.Errorf("bus status RPC: %w", err)
	}
	if !resp.Success {
		return "", "", fmt.Errorf("bus status error: %s", resp.Error)
	}

	var status rpc.BusStatusResult
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		return "", "", fmt.Errorf("parse bus status: %w", err)
	}
	if !status.NATSEnabled || status.NATSPort == 0 {
		return "", "", fmt.Errorf("NATS not enabled")
	}

	// Connect to NATS.
	connectURL := fmt.Sprintf("nats://127.0.0.1:%d", status.NATSPort)
	connectOpts := []nats.Option{
		nats.Name("bd-stop-check"),
		nats.Timeout(5 * time.Second),
	}
	if token := os.Getenv("BD_DAEMON_TOKEN"); token != "" {
		connectOpts = append(connectOpts, nats.Token(token))
	}

	nc, err := nats.Connect(connectURL, connectOpts...)
	if err != nil {
		return "", "", fmt.Errorf("NATS connect: %w", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		return "", "", fmt.Errorf("JetStream context: %w", err)
	}

	return awaitDecisionOnJetStream(ctx, js, decisionID, timeout)
}

// awaitDecisionOnJetStream subscribes to DecisionResponded events on the given
// JetStream context and waits for the specific decision ID. This is the core
// NATS wake logic, extracted for testability.
func awaitDecisionOnJetStream(ctx context.Context, js nats.JetStreamContext, decisionID string, timeout time.Duration) (string, string, error) {
	// Subscribe to DecisionResponded events.
	subject := eventbus.SubjectForEvent(eventbus.EventDecisionResponded)
	sub, err := js.SubscribeSync(subject, nats.DeliverNew())
	if err != nil {
		return "", "", fmt.Errorf("subscribe %s: %w", subject, err)
	}
	defer sub.Unsubscribe()

	fmt.Fprintf(os.Stderr, "Listening on NATS %s for decision %s\n", subject, decisionID)

	// Check if already responded (race: response arrived before we subscribed).
	_, selected, text, done, err := checkDecisionResponse(ctx, decisionID)
	if err == nil && done {
		return selected, text, nil
	}

	// Wait for matching event.
	deadline := time.Now().Add(timeout)
	for {
		timeLeft := time.Until(deadline)
		if timeLeft <= 0 {
			return "", "", nil // timeout
		}

		msg, err := sub.NextMsg(timeLeft)
		if err != nil {
			if err == nats.ErrTimeout {
				return "", "", nil // timeout
			}
			// Transient error — check DB as fallback.
			_, selected, text, done, dbErr := checkDecisionResponse(ctx, decisionID)
			if dbErr == nil && done {
				return selected, text, nil
			}
			return "", "", fmt.Errorf("NATS NextMsg: %w", err)
		}

		// Parse the event payload.
		var payload eventbus.DecisionEventPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			_ = msg.Ack()
			continue
		}

		_ = msg.Ack()

		// Check if this is the decision we're waiting for.
		if payload.DecisionID == decisionID {
			// Read the full response from DB to get all fields.
			_, selected, text, done, err := checkDecisionResponse(ctx, decisionID)
			if err == nil && done {
				return selected, text, nil
			}
			// Event arrived but DB not yet updated — brief retry.
			time.Sleep(100 * time.Millisecond)
			_, selected, text, done, err = checkDecisionResponse(ctx, decisionID)
			if err == nil && done {
				return selected, text, nil
			}
			return payload.ChosenLabel, payload.Rationale, nil
		}
		// Not our decision — continue waiting.
	}
}

// checkDecisionResponse checks if a decision point has been responded to.
// Returns (decisionPoint, selectedOption, responseText, done, error).
// Important: checks decision point response BEFORE issue status, because
// bd decision respond both records the response AND closes the gate issue.
func checkDecisionResponse(ctx context.Context, decisionID string) (*types.DecisionPoint, string, string, bool, error) {
	if daemonClient == nil {
		// Fall back to direct store access (used in tests)
		if store == nil {
			return nil, "", "", false, nil
		}
		dp, err := store.GetDecisionPoint(ctx, decisionID)
		if err == nil && dp != nil && dp.RespondedAt != nil {
			return dp, dp.SelectedOption, decisionResponseText(dp), true, nil
		}
		issue, err := store.GetIssue(ctx, decisionID)
		if err != nil {
			return nil, "", "", false, err
		}
		if issue != nil && issue.Status == types.StatusClosed {
			return nil, "", "", true, nil
		}
		return nil, "", "", false, nil
	}
	// Check decision point response via RPC
	getArgs := &rpc.DecisionGetArgs{IssueID: decisionID}
	result, err := daemonClient.DecisionGet(getArgs)
	if err == nil && result != nil && result.Decision != nil {
		dp := result.Decision
		if dp.RespondedAt != nil {
			return dp, dp.SelectedOption, decisionResponseText(dp), true, nil
		}
	}

	// Then check if issue is closed/canceled (without a response = canceled).
	showArgs := &rpc.ShowArgs{ID: decisionID}
	resp, err := daemonClient.Show(showArgs)
	if err != nil {
		return nil, "", "", false, err
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		return nil, "", "", false, err
	}

	if issue.Status == types.StatusClosed {
		return nil, "", "", true, nil // closed without response = canceled
	}

	return nil, "", "", false, nil
}

// parseDurationOrDefault parses a duration string, returning defaultVal on error.
func parseDurationOrDefault(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// pollForAgentDecision polls until an agent-created decision appears or timeout.
// Returns the decision if found, nil on timeout.
func pollForAgentDecision(ctx context.Context, actorTag string, timeout, pollInterval time.Duration) (*types.DecisionPoint, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dp, err := findPendingAgentDecision(ctx, actorTag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: error polling for agent decision: %v\n", err)
				continue
			}
			if dp != nil {
				return dp, nil
			}
			if time.Now().After(deadline) {
				return nil, nil // timeout
			}
		case <-ctx.Done():
			return nil, nil // context canceled
		}
	}
}

// findPendingAgentDecision finds the most recent pending decision created by
// an agent (i.e., not by stop-hook). Returns nil if none found.
//
// Only returns decisions with responded_at IS NULL (truly pending). Previously
// this also included recently-responded decisions (within 5 minutes), but that
// caused an infinite loop: after a decision was responded to, the stop hook
// kept finding it and allowing stop, which fired the hook again immediately.
// The race condition the 5-minute window was meant to handle (human responds
// before stop hook fires) is not an actual problem — the agent's
// `bd decision create --wait` call blocks until response, so by the time the
// agent tries to stop, the decision flow is complete.
func findPendingAgentDecision(ctx context.Context, actorTag string) (*types.DecisionPoint, error) {
	var decisions []*types.DecisionPoint

	if daemonClient != nil {
		// Use daemon RPC when available
		listArgs := &rpc.DecisionListArgs{All: false}
		listResp, err := daemonClient.DecisionList(listArgs)
		if err != nil {
			return nil, fmt.Errorf("daemon decision list: %w", err)
		}
		for _, dr := range listResp.Decisions {
			decisions = append(decisions, dr.Decision)
		}
	} else if store != nil {
		// Fall back to direct store access (used in tests)
		pending, err := store.ListPendingDecisions(ctx)
		if err != nil {
			return nil, fmt.Errorf("store list pending decisions: %w", err)
		}
		decisions = pending
	} else {
		return nil, nil
	}

	// Filter: not created by stop-hook, has a RequestedBy value,
	// and matches the current actor (if available).
	// This prevents one user's agents from picking up another's decisions.
	var best *types.DecisionPoint
	for _, dp := range decisions {
		if dp.RequestedBy == "stop-hook" || dp.RequestedBy == "" {
			continue
		}
		// Actor scoping: if we have an actor tag, only match decisions from this actor
		if actorTag != "" && dp.RequestedBy != actorTag {
			continue
		}
		if best == nil || dp.CreatedAt.After(best.CreatedAt) {
			best = dp
		}
	}

	return best, nil
}

// getStopSessionTag returns a session identifier for scoping stop decisions.
// Priority: CLAUDE_SESSION_ID > TERM_SESSION_ID.
// Returns "" if no session identifier is available (falls back to unscoped).
func getStopSessionTag() string {
	if id := os.Getenv("CLAUDE_SESSION_ID"); id != "" {
		return id
	}
	if id := os.Getenv("TERM_SESSION_ID"); id != "" {
		return id
	}
	return ""
}

// findUnclosedStopDecisions finds old stop decisions (created by stop-hook) whose
// gate issues are still open (not closed by the agent).
func findUnclosedStopDecisions(ctx context.Context) ([]*types.DecisionPoint, error) {
	var decisions []*types.DecisionPoint

	if daemonClient != nil {
		// List all decisions (including resolved) to find stop-hook ones via daemon RPC
		allListArgs := &rpc.DecisionListArgs{All: true}
		allResp, err := daemonClient.DecisionList(allListArgs)
		if err != nil {
			return nil, fmt.Errorf("daemon decision list: %w", err)
		}
		for _, dr := range allResp.Decisions {
			dp := dr.Decision
			if dp.RequestedBy != "stop-hook" {
				continue
			}
			// Only include if the gate issue is still open
			if dr.Issue != nil && dr.Issue.Status != types.StatusClosed {
				decisions = append(decisions, dp)
			}
		}
	} else if store != nil {
		// Fall back to direct store access (used in tests).
		// List all pending decisions and filter for stop-hook.
		pending, err := store.ListPendingDecisions(ctx)
		if err != nil {
			return nil, fmt.Errorf("store list pending decisions: %w", err)
		}
		for _, dp := range pending {
			if dp.RequestedBy != "stop-hook" {
				continue
			}
			// Check if gate issue is still open
			issue, err := store.GetIssue(ctx, dp.IssueID)
			if err != nil {
				continue
			}
			if issue != nil && issue.Status != types.StatusClosed {
				decisions = append(decisions, dp)
			}
		}
	}

	return decisions, nil
}

// joinIDs joins a slice of IDs into a comma-separated string.
func joinIDs(ids []string) string {
	return strings.Join(ids, ", ")
}

// decisionResponseText returns the best available text from a decision response,
// checking ResponseText first, then Rationale, then Guidance.
func decisionResponseText(dp *types.DecisionPoint) string {
	if dp.ResponseText != "" {
		return dp.ResponseText
	}
	if dp.Rationale != "" {
		return dp.Rationale
	}
	return dp.Guidance
}
