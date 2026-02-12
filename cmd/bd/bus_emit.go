package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
)

var busEmitCmd = &cobra.Command{
	Use:   "emit",
	Short: "Dispatch an event through the event bus",
	Long: `Dispatch an event through the event bus.

For Claude Code hook events, reads the hook event JSON from stdin:
  bd bus emit --hook=Stop

For non-hook events (e.g. decision events), use --event and --payload:
  bd bus emit --event=DecisionCreated --payload='{"decision_id":"x",...}'

Events are dispatched via the bd daemon RPC.

Exit codes:
  0 - Event processed, no blocking
  2 - Event blocked by a handler (gate check failed)

Examples:
  # Claude Code hook (reads stdin):
  bd bus emit --hook=Stop
  bd bus emit --hook=PreToolUse

  # Decision event (inline payload):
  bd bus emit --event=DecisionCreated --payload='{"decision_id":"od-xyz","question":"Which approach?"}'`,
	RunE: runBusEmit,
}

func runBusEmit(cmd *cobra.Command, args []string) error {
	hookType, _ := cmd.Flags().GetString("hook")
	eventType, _ := cmd.Flags().GetString("event")
	payloadFlag, _ := cmd.Flags().GetString("payload")

	// Determine the event type from either --hook or --event.
	var resolvedType string
	var eventData []byte

	switch {
	case hookType != "" && eventType != "":
		return fmt.Errorf("--hook and --event are mutually exclusive")
	case hookType != "":
		resolvedType = hookType
		// Read stdin (Claude Code sends hook event JSON on stdin).
		var err error
		eventData, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	case eventType != "":
		resolvedType = eventType
		if payloadFlag != "" {
			eventData = []byte(payloadFlag)
		}
	default:
		return fmt.Errorf("either --hook or --event is required")
	}

	// Extract session_id from the event JSON if present.
	var eventMeta struct {
		SessionID string `json:"session_id"`
	}
	if len(eventData) > 0 {
		_ = json.Unmarshal(eventData, &eventMeta)
	}

	// Inject caller's session tag into the event JSON so the daemon-side
	// stop-check subprocess can scope decisions to this terminal session.
	if sessionTag := os.Getenv("TERM_SESSION_ID"); sessionTag != "" {
		var raw map[string]interface{}
		if len(eventData) > 0 {
			_ = json.Unmarshal(eventData, &raw)
		}
		if raw == nil {
			raw = map[string]interface{}{}
		}
		if _, exists := raw["caller_session_tag"]; !exists {
			raw["caller_session_tag"] = sessionTag
			eventData, _ = json.Marshal(raw)
		}
	}

	// Dispatch via daemon RPC
	requireDaemon("bus emit")

	// For Stop hooks, extend the request timeout so the stop-decision
	// handler can poll for up to 1 hour without hitting daemon timeouts.
	if resolvedType == "Stop" {
		daemonClient.SetRequestTimeout(3600 * 1000) // 1 hour
	}

	emitArgs := &rpc.BusEmitArgs{
		HookType:  resolvedType,
		EventJSON: eventData,
		SessionID: eventMeta.SessionID,
	}

	resp, err := daemonClient.Execute(rpc.OpBusEmit, emitArgs)

	// Reset request timeout after the call.
	if resolvedType == "Stop" {
		daemonClient.SetRequestTimeout(0)
	}

	if err != nil {
		return fmt.Errorf("bus emit: daemon RPC failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("bus emit: daemon error: %s", resp.Error)
	}

	var emitResult rpc.BusEmitResult
	if err := json.Unmarshal(resp.Data, &emitResult); err != nil {
		return fmt.Errorf("parse emit result: %w", err)
	}

	return outputEmitResult(&emitResult)
}

// outputEmitResult writes the emit result according to the Claude Code hook
// protocol:
//   - result.Block → exit 2, stderr JSON with decision/reason
//   - result.Inject → stdout (content for Claude Code)
//   - result.Warnings → stdout as system-reminder tags
//   - otherwise → exit 0
func outputEmitResult(result *rpc.BusEmitResult) error {
	// Output injected content.
	for _, msg := range result.Inject {
		fmt.Println(msg)
	}

	// Output warnings as system-reminder tags.
	for _, w := range result.Warnings {
		fmt.Printf("<system-reminder>%s</system-reminder>\n", w)
	}

	if result.Block {
		blockJSON, _ := json.Marshal(map[string]string{
			"decision": "block",
			"reason":   result.Reason,
		})
		fmt.Fprintln(os.Stderr, string(blockJSON))
		os.Exit(2)
	}

	return nil
}

func init() {
	busEmitCmd.Flags().String("hook", "", "Hook event type (e.g., Stop, PreToolUse, SessionStart)")
	busEmitCmd.Flags().String("event", "", "Non-hook event type (e.g., DecisionCreated, DecisionResponded)")
	busEmitCmd.Flags().String("payload", "", "JSON payload for --event (alternative to stdin)")
	busCmd.AddCommand(busEmitCmd)
}
