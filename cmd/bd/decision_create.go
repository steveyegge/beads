package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/notification"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// decisionCreateCmd creates a new decision point
var decisionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new decision point",
	Long: `Create a decision point gate that blocks until a human responds.

The decision point is a gate issue (type=gate, await_type=decision) with associated
decision data stored in the decision_points table.

Options are specified as a JSON array of objects with id, short, label, and optional description:
  [{"id":"a","short":"Redis","label":"Use Redis for caching","description":"Full markdown..."}]

Examples:
  # Simple yes/no decision
  bd decision create --prompt="Proceed with migration?" \
    --options='[{"id":"yes","short":"Yes","label":"Yes, proceed"},{"id":"no","short":"No","label":"No, abort"}]'

  # Decision with default and timeout
  bd decision create --prompt="Which approach?" \
    --options='[{"id":"a","label":"Option A"},{"id":"b","label":"Option B"}]' \
    --default=a --timeout=24h

  # Decision that blocks another issue
  bd decision create --prompt="Approve design?" \
    --options='[{"id":"approve","label":"Approve"},{"id":"reject","label":"Reject"}]' \
    --blocks=gt-abc123.4

  # Decision with parent molecule
  bd decision create --prompt="Select strategy" --parent=gt-abc123 \
    --options='[{"id":"a","label":"Strategy A"}]'`,
	Run: runDecisionCreate,
}

func init() {
	decisionCreateCmd.Flags().StringP("prompt", "p", "", "The question to ask (required)")
	decisionCreateCmd.Flags().StringP("options", "o", "", "JSON array of options")
	decisionCreateCmd.Flags().StringP("default", "d", "", "Default option ID if timeout")
	decisionCreateCmd.Flags().Duration("timeout", 24*time.Hour, "Timeout duration (default 24h)")
	decisionCreateCmd.Flags().String("parent", "", "Parent issue (molecule)")
	decisionCreateCmd.Flags().String("blocks", "", "Issue ID this decision blocks")
	decisionCreateCmd.Flags().Int("max-iterations", 3, "Maximum refinement iterations")
	decisionCreateCmd.Flags().Bool("no-notify", false, "Don't send notifications (for testing)")
	decisionCreateCmd.Flags().String("requested-by", "", "Agent/session that requested this decision (for wake notifications)")
	decisionCreateCmd.Flags().StringP("urgency", "u", "medium", "Urgency level: high, medium, low")
	decisionCreateCmd.Flags().String("predecessor", "", "Previous decision in chain (for decision chaining)")
	decisionCreateCmd.Flags().StringP("context", "c", "", "Background/analysis context for the decision (JSON or text)")
	decisionCreateCmd.Flags().Bool("wait", true, "Block until human responds (default: true). Use --wait=false for fire-and-forget.")
	decisionCreateCmd.Flags().Duration("wait-timeout", 60*time.Minute, "Timeout when waiting for response")
	decisionCreateCmd.Flags().Duration("wait-poll-interval", 2*time.Second, "Poll interval when waiting for response")

	_ = decisionCreateCmd.MarkFlagRequired("prompt")

	decisionCmd.AddCommand(decisionCreateCmd)
}

func runDecisionCreate(cmd *cobra.Command, args []string) {
	CheckReadonly("decision create")

	prompt, _ := cmd.Flags().GetString("prompt")
	optionsJSON, _ := cmd.Flags().GetString("options")
	defaultOption, _ := cmd.Flags().GetString("default")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	parent, _ := cmd.Flags().GetString("parent")
	blocks, _ := cmd.Flags().GetString("blocks")
	maxIterations, _ := cmd.Flags().GetInt("max-iterations")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	requestedBy, _ := cmd.Flags().GetString("requested-by")
	if requestedBy == "" && actor != "" {
		requestedBy = actor // Default to resolved actor identity for readable Slack display
	}
	waitForResponse, _ := cmd.Flags().GetBool("wait")
	waitTimeout, _ := cmd.Flags().GetDuration("wait-timeout")
	waitPollInterval, _ := cmd.Flags().GetDuration("wait-poll-interval")
	urgency, _ := cmd.Flags().GetString("urgency")
	predecessor, _ := cmd.Flags().GetString("predecessor")
	decisionContext, _ := cmd.Flags().GetString("context")

	ctx := rootCtx

	// Validate urgency
	urgency = strings.ToLower(urgency)
	switch urgency {
	case "high", "medium", "low":
		// Valid
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid urgency '%s': must be high, medium, or low\n", urgency)
		os.Exit(1)
	}

	// Validate context requirement from stop_decision config.
	// This catches missing context at creation time so bad decisions
	// never reach Slack (avoids spam from create-close-recreate cycles).
	if decisionContext == "" {
		cfg := loadStopDecisionConfig(ctx)
		if cfg != nil && cfg.Enabled && cfg.RequireContext {
			fmt.Fprintf(os.Stderr, "Error: --context is required by stop_decision config (require_context=true)\n")
			if cfg.AgentContextPrompt != "" {
				fmt.Fprintf(os.Stderr, "%s\n", cfg.AgentContextPrompt)
			}
			os.Exit(1)
		}
	}

	// Validate options JSON - at least one option is required
	if optionsJSON == "" {
		fmt.Fprintf(os.Stderr, "Error: --options is required (at least one option must be provided)\n")
		fmt.Fprintf(os.Stderr, "Example: --options='[{\"id\":\"yes\",\"label\":\"Yes\"},{\"id\":\"no\",\"label\":\"No\"}]'\n")
		os.Exit(1)
	}

	var options []types.DecisionOption
	if optionsJSON != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid options JSON: %v\n", err)
			os.Exit(1)
		}

		// Require at least one option
		if len(options) == 0 {
			fmt.Fprintf(os.Stderr, "Error: at least one option is required\n")
			fmt.Fprintf(os.Stderr, "Example: --options='[{\"id\":\"yes\",\"label\":\"Yes\"},{\"id\":\"no\",\"label\":\"No\"}]'\n")
			os.Exit(1)
		}

		// Validate each option has required fields
		for i, opt := range options {
			if opt.ID == "" {
				fmt.Fprintf(os.Stderr, "Error: option %d missing 'id' field\n", i)
				os.Exit(1)
			}
			if opt.Label == "" {
				fmt.Fprintf(os.Stderr, "Error: option %d missing 'label' field\n", i)
				os.Exit(1)
			}
			// Auto-fill short from ID if not provided
			if opt.Short == "" {
				options[i].Short = opt.ID
			}
		}

		// Validate default option exists
		if defaultOption != "" {
			found := false
			for _, opt := range options {
				if opt.ID == defaultOption {
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "Error: default option '%s' not found in options\n", defaultOption)
				os.Exit(1)
			}
		}

		// Re-marshal with any fixes
		optionsBytes, _ := json.Marshal(options)
		optionsJSON = string(optionsBytes)
	}

	var decisionID string
	var decisionPoint *types.DecisionPoint
	var gateIssue *types.Issue
	now := time.Now()

	requireDaemon("decision create")

	createArgs := &rpc.DecisionCreateArgs{
		Prompt:        prompt,
		Options:       options,
		DefaultOption: defaultOption,
		MaxIterations: maxIterations,
		RequestedBy:   requestedBy,
		Context:       decisionContext,
		Parent:        parent,
		Blocks:        blocks,
		Predecessor:   predecessor,
		Urgency:       urgency,
	}

	result, err := daemonClient.DecisionCreate(createArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating decision via daemon: %v\n", err)
		os.Exit(1)
	}

	decisionPoint = result.Decision
	gateIssue = result.Issue
	decisionID = decisionPoint.IssueID

	// Trigger decision create hook (hq-e0adf6.4)
	// Use RunDecisionSync to ensure hook completes before program exits
	if hookRunner != nil {
		_ = hookRunner.RunDecisionSync(hooks.EventDecisionCreate, decisionPoint, nil, requestedBy)
	}

	// Emit decision event to bus (od-k3o.15.1).
	emitDecisionEvent(eventbus.EventDecisionCreated, eventbus.DecisionEventPayload{
		DecisionID:  decisionID,
		Question:    prompt,
		Urgency:     urgency,
		RequestedBy: requestedBy,
		Options:     len(options),
	})

	// Output — if waiting, defer JSON output until after response arrives.
	if jsonOutput && !waitForResponse {
		result := map[string]interface{}{
			"id":             decisionID,
			"prompt":         prompt,
			"context":        decisionContext,
			"options":        options,
			"default_option": defaultOption,
			"timeout":        timeout.String(),
			"parent":         parent,
			"blocks":         blocks,
			"predecessor":    predecessor,
		}
		outputJSON(result)
		return
	}
	if jsonOutput {
		// When waiting, print creation info to stderr so stdout is reserved
		// for the final response JSON.
		fmt.Fprintf(os.Stderr, "Created decision: %s\n", decisionID)
	}

	// Human-readable output
	fmt.Printf("%s Created decision point: %s\n\n", ui.RenderPass("✓"), ui.RenderID(decisionID))
	fmt.Printf("  %s\n\n", prompt)

	if decisionContext != "" {
		fmt.Printf("  Context: %s\n\n", decisionContext)
	}

	if len(options) > 0 {
		for _, opt := range options {
			defaultMarker := ""
			if opt.ID == defaultOption {
				defaultMarker = " (default)"
			}
			fmt.Printf("  [%s] %s - %s%s\n", opt.ID, opt.Short, opt.Label, defaultMarker)
		}
		fmt.Println()
	}

	fmt.Println("  Or provide custom text response.")
	fmt.Println()

	fmt.Printf("  Timeout: %s\n", formatTimeout(timeout, now))
	if blocks != "" {
		fmt.Printf("  Blocks: %s\n", blocks)
	}
	if parent != "" {
		fmt.Printf("  Parent: %s\n", parent)
	}
	if predecessor != "" {
		fmt.Printf("  Predecessor: %s\n", predecessor)
	}

	if noNotify {
		fmt.Println("\n  (Notifications skipped)")
	} else {
		// Dispatch notifications (hq-5d43fc)
		beadsDir := filepath.Dir(dbPath)
		results, err := notification.DispatchDecisionNotification(beadsDir, decisionPoint, gateIssue, "default")
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  Warning: notification dispatch failed: %v\n", err)
		} else if len(results) > 0 {
			fmt.Printf("\n  Notifications sent: %d\n", len(results))
			for _, r := range results {
				if r.Success {
					fmt.Printf("    ✓ %s\n", r.Channel)
				} else {
					fmt.Printf("    ✗ %s: %s\n", r.Channel, r.Error)
				}
			}
		} else {
			fmt.Println("\n  (No notification routes configured)")
		}
	}

	// --wait: block until human responds, then output the response.
	// This is the primary mechanism for human-in-the-loop decisions.
	// The stop hook is just a guard that forces the agent to call this.
	if waitForResponse {
		fmt.Fprintf(os.Stderr, "\nWaiting for response on %s (timeout: %s)...\n", decisionID, waitTimeout)
		selected, responseText, err := pollStopDecision(ctx, decisionID, waitTimeout, waitPollInterval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error waiting for response: %v\n", err)
			os.Exit(1)
		}
		if selected == "" && responseText == "" {
			fmt.Fprintf(os.Stderr, "Timeout waiting for response on %s\n", decisionID)
			if jsonOutput {
				outputJSON(map[string]string{
					"id":       decisionID,
					"status":   "timeout",
					"selected": "",
				})
			} else {
				fmt.Printf("\n  Timed out waiting for response.\n")
			}
			return
		}
		// Map selected option ID to its label
		selectedLabel := selected
		for _, opt := range options {
			if opt.ID == selected {
				selectedLabel = opt.Label
				break
			}
		}

		// Output the response
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"id":             decisionID,
				"status":         "responded",
				"selected":       selected,
				"selected_label": selectedLabel,
				"response_text":  responseText,
			})
		} else {
			fmt.Printf("\n  Response received: %s\n", selectedLabel)
			if responseText != "" {
				fmt.Printf("  %s\n", responseText)
			}
		}
	}
}


// formatTimeout formats the timeout duration relative to creation time
func formatTimeout(timeout time.Duration, created time.Time) string {
	expires := created.Add(timeout)
	return fmt.Sprintf("%s (%s)", expires.Format("2006-01-02 15:04 MST"), timeout)
}
