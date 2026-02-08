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
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
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

	// Parse timeout and poll interval (command flags override config)
	timeout := parseDurationOrDefault(cfg.Timeout, 30*time.Minute)
	pollInterval := parseDurationOrDefault(cfg.PollInterval, 2*time.Second)

	// Command-line flags override config values if explicitly set
	if cmd.Flags().Changed("timeout") {
		timeout, _ = cmd.Flags().GetDuration("timeout")
	}
	if cmd.Flags().Changed("poll-interval") {
		pollInterval, _ = cmd.Flags().GetDuration("poll-interval")
	}

	// Step 1: If require_close_old, check for unclosed old stop decisions
	if cfg.RequireCloseOld {
		unclosed, err := findUnclosedStopDecisions(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: error checking unclosed stop decisions: %v\n", err)
		} else if len(unclosed) > 0 {
			ids := make([]string, len(unclosed))
			for i, dp := range unclosed {
				ids[i] = dp.IssueID
			}
			reason := fmt.Sprintf("Close previous stop decisions before stopping: %s", joinIDs(ids))
			if cfg.AgentCloseOldPrompt != "" {
				reason = fmt.Sprintf("%s\n\nDecisions to close: %s", cfg.AgentCloseOldPrompt, joinIDs(ids))
			}
			if jsonOutput {
				outputJSON(map[string]string{"decision": "block", "reason": reason})
			} else {
				fmt.Printf("Block: %s\n", reason)
			}
			os.Exit(1)
		}
	}

	// Step 2: If require_agent_decision, look for an agent decision.
	// IMPORTANT: This must NOT poll/block when no decision exists, because
	// the agent (Claude) is blocked waiting for this hook to return. If we
	// poll here, it's a deadlock — the agent can't create a decision while
	// blocked. Instead, return immediately with block + instructions so the
	// agent can create the decision, then on re-entry we'll find and await it.
	//
	// On re-entry (stop_hook_active=true → --reentry), if the agent STILL
	// didn't create a decision, fall through to auto-create a generic one.
	// This prevents infinite block loops where the agent ignores instructions.
	isReentry, _ := cmd.Flags().GetBool("reentry")
	if cfg.RequireAgentDecision {
		sessionTag := getStopSessionTag()
		agentDecision, err := findPendingAgentDecision(ctx, sessionTag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: error finding agent decision: %v\n", err)
			// Fall through to generic decision creation
		} else if agentDecision == nil {
			if !isReentry {
				// First attempt: block with instructions for the agent.
				reason := "Create a decision with 'bd decision create' before stopping"
				if cfg.AgentDecisionPrompt != "" {
					reason = cfg.AgentDecisionPrompt
				}
				if sessionTag != "" {
					reason += fmt.Sprintf("\n\nIMPORTANT: Pass --requested-by %q to scope this decision to your session.", sessionTag)
				}
				fmt.Fprintf(os.Stderr, "No agent decision found. Blocking so agent can create one.\n")
				if jsonOutput {
					outputJSON(map[string]string{"decision": "block", "reason": reason})
				} else {
					fmt.Printf("Block: %s\n", reason)
				}
				os.Exit(1)
			}
			// Re-entry: agent didn't comply. Fall through to auto-create
			// a generic decision so the human still gets notified.
			fmt.Fprintf(os.Stderr, "Re-entry: agent did not create decision. Auto-creating generic decision.\n")
		} else {
			// Agent decision found — validate context if required
			if cfg.RequireContext && agentDecision.Context == "" {
				reason := fmt.Sprintf("Decision %s is missing context. Close it and create a new one with --context", agentDecision.IssueID)
				if cfg.AgentContextPrompt != "" {
					reason = fmt.Sprintf("%s\n\nDecision to close: %s", cfg.AgentContextPrompt, agentDecision.IssueID)
				}
				if jsonOutput {
					outputJSON(map[string]string{"decision": "block", "reason": reason})
				} else {
					fmt.Printf("Block: %s\n", reason)
				}
				os.Exit(1)
			}

			// Await the human's response to the agent's decision.
			// This is the only place we poll — and it's safe because the agent
			// already created the decision before trying to stop.
			fmt.Fprintf(os.Stderr, "Found agent decision: %s (timeout: %s)\n", agentDecision.IssueID, timeout)
			fmt.Fprintf(os.Stderr, "Waiting for human response...\n")

			selected, responseText, err := pollStopDecision(ctx, agentDecision.IssueID, timeout, pollInterval)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error polling agent decision: %v\n", err)
				if jsonOutput {
					outputJSON(map[string]string{"decision": "allow", "reason": fmt.Sprintf("poll error: %v", err)})
				}
				os.Exit(0)
			}

			// Check the human's selection
			if selected == "stop" {
				// Human said stop — allow it
				if jsonOutput {
					outputJSON(map[string]string{"decision": "allow", "reason": "human selected stop"})
				}
				os.Exit(0)
			}
			if selected != "" {
				// Any other selection (e.g. "continue") blocks the stop.
				// Always lead with the selected option so the agent knows what
				// the human chose, then append any extra text as context.
				reason := fmt.Sprintf("Human selected '%s' on decision %s", selected, agentDecision.IssueID)
				if responseText != "" {
					reason += "\n\n" + responseText
				}
				if jsonOutput {
					outputJSON(map[string]string{
						"decision":  "block",
						"reason":    reason,
						"selected":  selected,
						"source_id": agentDecision.IssueID,
					})
				} else {
					fmt.Printf("Block: %s\n", reason)
				}
				os.Exit(1)
			}

			// Timeout or canceled — allow stop
			if jsonOutput {
				outputJSON(map[string]string{"decision": "allow", "reason": "timeout or canceled"})
			}
			os.Exit(0)
		}
	}

	// Default behavior: create a generic stop decision (same as before)
	prompt := cfg.Prompt
	if prompt == "" {
		prompt = "Claude is ready to stop. Review and decide:"
	}

	options := cfg.Options
	if len(options) == 0 {
		options = []types.DecisionOption{
			{ID: "continue", Short: "continue", Label: "Continue working - provide instructions"},
			{ID: "stop", Short: "stop", Label: "Allow Claude to stop"},
		}
	}

	urgency := cfg.Urgency
	if urgency == "" {
		urgency = "high"
	}

	defaultAction := cfg.DefaultAction
	if defaultAction == "" {
		defaultAction = "stop"
	}

	// Create the decision point
	decisionID, err := createStopDecision(ctx, prompt, options, urgency, defaultAction, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating stop decision: %v\n", err)
		// On error, allow stop (fail-open)
		if jsonOutput {
			outputJSON(map[string]string{"decision": "allow", "reason": fmt.Sprintf("error creating decision: %v", err)})
		}
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "Stop decision created: %s (timeout: %s)\n", decisionID, timeout)
	fmt.Fprintf(os.Stderr, "Waiting for human response via 'bd decision watch' or 'bd decision respond'...\n")

	// Poll for response
	selected, responseText, err := pollStopDecision(ctx, decisionID, timeout, pollInterval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error polling stop decision: %v\n", err)
		// On error, allow stop (fail-open)
		if jsonOutput {
			outputJSON(map[string]string{"decision": "allow", "reason": fmt.Sprintf("poll error: %v", err)})
		}
		os.Exit(0)
	}

	// Interpret the response
	if selected == "continue" {
		// Block stop — human wants Claude to continue
		reason := "Human selected 'continue'"
		if responseText != "" {
			reason += "\n\n" + responseText
		}
		if jsonOutput {
			outputJSON(map[string]string{"decision": "block", "reason": reason})
		} else {
			fmt.Printf("Block: %s\n", reason)
		}
		os.Exit(1)
	}

	// Allow stop (human said "stop", timeout, canceled, or unknown selection)
	if jsonOutput {
		reason := "human selected stop"
		if selected == "" {
			reason = "timeout or canceled"
		}
		outputJSON(map[string]string{"decision": "allow", "reason": reason})
	}
	os.Exit(0)
}

// loadStopDecisionConfig loads stop_decision settings from the claude-hooks config bead.
func loadStopDecisionConfig(ctx context.Context) *stopDecisionConfig {
	var mergedData map[string]interface{}

	if daemonClient != nil {
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
	} else if store != nil {
		// Direct store access
		issueType := types.IssueType("config")
		filter := types.IssueFilter{
			IssueType: &issueType,
			Labels:    []string{"config:claude-hooks"},
			Limit:     200,
		}
		issues, err := store.SearchIssues(ctx, "", filter)
		if err != nil || len(issues) == 0 {
			return nil
		}

		for _, issue := range issues {
			if issue.Metadata != nil {
				if err := json.Unmarshal(issue.Metadata, &mergedData); err == nil {
					break
				}
			}
		}
	} else {
		return nil
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
func createStopDecision(ctx context.Context, prompt string, options []types.DecisionOption, urgency, defaultOption string, timeout time.Duration) (string, error) {
	now := time.Now()

	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return "", fmt.Errorf("marshaling options: %w", err)
	}

	if daemonClient != nil {
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

	if store == nil {
		return "", fmt.Errorf("no database connection available")
	}

	// Generate decision ID
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		prefix = "hq"
	}
	var decisionID string
	for nonce := 0; nonce < 100; nonce++ {
		candidateID := idgen.GenerateHashID(prefix, prompt, "", actor, now, 6, nonce)
		issue, err := store.GetIssue(ctx, candidateID)
		if err != nil {
			return "", fmt.Errorf("checking issue existence: %w", err)
		}
		if issue == nil {
			decisionID = candidateID
			break
		}
	}
	if decisionID == "" {
		return "", fmt.Errorf("failed to generate unique decision ID")
	}

	// Create gate issue + decision point in a transaction
	gateIssue := &types.Issue{
		ID:        decisionID,
		Title:     truncateTitle(prompt, 100),
		IssueType: types.IssueType("gate"),
		Status:    types.StatusOpen,
		Priority:  1,
		AwaitType: "decision",
		Timeout:   timeout,
		Labels:    []string{"gt:decision", "decision:pending", "urgency:" + urgency, "stop-decision"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	decisionPoint := &types.DecisionPoint{
		Prompt:        prompt,
		Options:       string(optionsJSON),
		DefaultOption: defaultOption,
		Iteration:     1,
		MaxIterations: 1,
		CreatedAt:     now,
		RequestedBy:   "stop-hook",
		Urgency:       urgency,
	}

	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		if err := tx.CreateIssue(ctx, gateIssue, actor); err != nil {
			return fmt.Errorf("creating gate issue: %w", err)
		}

		decisionID = gateIssue.ID
		decisionPoint.IssueID = decisionID

		for _, label := range gateIssue.Labels {
			if err := tx.AddLabel(ctx, decisionID, label, actor); err != nil {
				return fmt.Errorf("adding label %s: %w", label, err)
			}
		}

		if err := tx.CreateDecisionPoint(ctx, decisionPoint); err != nil {
			return fmt.Errorf("creating decision point: %w", err)
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	markDirtyAndScheduleFlush()

	// Trigger decision create hook
	if hookRunner != nil {
		_ = hookRunner.RunDecisionSync(hooks.EventDecisionCreate, decisionPoint, nil, "stop-hook")
	}

	return decisionID, nil
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
	if daemonClient != nil {
		// Check decision point response first (via store fallback).
		if store != nil {
			dp, err := store.GetDecisionPoint(ctx, decisionID)
			if err != nil {
				return nil, "", "", false, err
			}
			if dp != nil && dp.RespondedAt != nil {
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

	if store == nil {
		return nil, "", "", false, fmt.Errorf("no database connection")
	}

	// Check decision point response first.
	dp, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		return nil, "", "", false, err
	}
	if dp != nil && dp.RespondedAt != nil {
		return dp, dp.SelectedOption, decisionResponseText(dp), true, nil
	}

	// Then check if issue was closed without a response (canceled).
	issue, err := store.GetIssue(ctx, decisionID)
	if err != nil {
		return nil, "", "", false, err
	}
	if issue != nil && issue.Status == types.StatusClosed {
		return nil, "", "", true, nil
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
func pollForAgentDecision(ctx context.Context, sessionTag string, timeout, pollInterval time.Duration) (*types.DecisionPoint, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dp, err := findPendingAgentDecision(ctx, sessionTag)
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

// findPendingAgentDecision finds the most recent pending decision created by an agent
// (i.e., not by stop-hook). Returns nil if none found.
func findPendingAgentDecision(ctx context.Context, sessionTag string) (*types.DecisionPoint, error) {
	var decisions []*types.DecisionPoint

	if daemonClient != nil {
		listArgs := &rpc.DecisionListArgs{All: false} // pending only
		resp, err := daemonClient.DecisionList(listArgs)
		if err != nil {
			return nil, fmt.Errorf("daemon decision list: %w", err)
		}
		for _, dr := range resp.Decisions {
			decisions = append(decisions, dr.Decision)
		}
	} else if store != nil {
		var err error
		decisions, err = store.ListPendingDecisions(ctx)
		if err != nil {
			return nil, fmt.Errorf("list pending decisions: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no database connection")
	}

	// Filter: not created by stop-hook, has a RequestedBy value,
	// and matches the current session tag (if available).
	// This prevents one Claude session from picking up another's decisions.
	var best *types.DecisionPoint
	for _, dp := range decisions {
		if dp.RequestedBy == "stop-hook" || dp.RequestedBy == "" {
			continue
		}
		// Session scoping: if we have a session tag, only match decisions from this session
		if sessionTag != "" && dp.RequestedBy != sessionTag {
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
		// List all decisions (including resolved) to find stop-hook ones
		listArgs := &rpc.DecisionListArgs{All: true}
		resp, err := daemonClient.DecisionList(listArgs)
		if err != nil {
			return nil, fmt.Errorf("daemon decision list: %w", err)
		}
		for _, dr := range resp.Decisions {
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
		// Get all pending decisions first, then check for resolved stop-hook ones
		pending, err := store.ListPendingDecisions(ctx)
		if err != nil {
			return nil, fmt.Errorf("list pending decisions: %w", err)
		}
		for _, dp := range pending {
			if dp.RequestedBy != "stop-hook" {
				continue
			}
			// Check if the gate issue is still open
			issue, err := store.GetIssue(ctx, dp.IssueID)
			if err != nil {
				continue
			}
			if issue != nil && issue.Status != types.StatusClosed {
				decisions = append(decisions, dp)
			}
		}
	} else {
		return nil, fmt.Errorf("no database connection")
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
