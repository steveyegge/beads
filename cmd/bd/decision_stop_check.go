package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
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
	Enabled       bool                   `json:"enabled"`
	Timeout       string                 `json:"timeout"`
	PollInterval  string                 `json:"poll_interval"`
	Prompt        string                 `json:"prompt"`
	Options       []types.DecisionOption `json:"options"`
	DefaultAction string                 `json:"default_action"`
	Urgency       string                 `json:"urgency"`
}

func init() {
	decisionStopCheckCmd.Flags().Duration("timeout", 30*time.Minute, "Override timeout from config")
	decisionStopCheckCmd.Flags().Duration("poll-interval", 2*time.Second, "Override poll interval from config")

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
		reason := responseText
		if reason == "" {
			reason = "Human selected 'continue'"
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
		optionStrings := make([]string, len(options))
		for i, opt := range options {
			optionStrings[i] = opt.Label
		}

		createArgs := &rpc.DecisionCreateArgs{
			Prompt:        prompt,
			Options:       optionStrings,
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

// pollStopDecision polls the decision point until it gets a response, timeout, or cancellation.
// Returns (selectedOption, responseText, error).
func pollStopDecision(ctx context.Context, decisionID string, timeout, pollInterval time.Duration) (string, string, error) {
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
				return dp, dp.SelectedOption, dp.ResponseText, true, nil
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
		return dp, dp.SelectedOption, dp.ResponseText, true, nil
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
