package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/eventbus"
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

Dispatch priority:
  1. If bd daemon is running (RPC): send to daemon
  2. Otherwise: create local bus and dispatch (no handlers = passthrough)

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

	var emitResult *rpc.BusEmitResult

	// Try daemon RPC first.
	if daemonClient != nil {
		emitArgs := &rpc.BusEmitArgs{
			HookType:  resolvedType,
			EventJSON: eventData,
			SessionID: eventMeta.SessionID,
		}

		resp, err := daemonClient.Execute(rpc.OpBusEmit, emitArgs)
		if err != nil {
			// Daemon unreachable — fall through to local dispatch.
			fmt.Fprintf(os.Stderr, "bus: daemon RPC failed, falling back to local: %v\n", err)
		} else if !resp.Success {
			fmt.Fprintf(os.Stderr, "bus: daemon error: %s\n", resp.Error)
		} else {
			var result rpc.BusEmitResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				return fmt.Errorf("parse emit result: %w", err)
			}
			emitResult = &result
		}
	}

	if emitResult == nil {
		// Local dispatch: create a bus with no handlers (passthrough).
		bus := eventbus.New()
		event := &eventbus.Event{
			Type:      eventbus.EventType(resolvedType),
			SessionID: eventMeta.SessionID,
			Raw:       eventData,
		}

		// Parse remaining fields from stdin/payload JSON into the event.
		if len(eventData) > 0 {
			_ = json.Unmarshal(eventData, event)
			// Ensure Type is not overwritten by JSON field.
			event.Type = eventbus.EventType(resolvedType)
		}

		result, err := bus.Dispatch(context.Background(), event)
		if err != nil {
			return fmt.Errorf("local dispatch: %w", err)
		}

		emitResult = &rpc.BusEmitResult{
			Block:    result.Block,
			Reason:   result.Reason,
			Inject:   result.Inject,
			Warnings: result.Warnings,
		}
	}

	// Stop decision check: runs directly in this process (not in the event bus)
	// because it needs to poll for up to 30 minutes, which exceeds daemon timeouts.
	if resolvedType == "Stop" && !emitResult.Block {
		if block, reason := runStopDecisionCheck(eventData); block {
			emitResult.Block = true
			emitResult.Reason = reason
		}
	}

	return outputEmitResult(emitResult)
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

// runStopDecisionCheck runs `bd decision stop-check --json` as a subprocess.
// This runs directly in the bus emit process (not in the event bus handler)
// because it needs to poll for up to 30 minutes, exceeding daemon RPC timeouts.
// Returns (block, reason).
func runStopDecisionCheck(eventData []byte) (bool, string) {
	// Check stop_hook_active to prevent infinite loop.
	if len(eventData) > 0 {
		var raw map[string]interface{}
		if err := json.Unmarshal(eventData, &raw); err == nil {
			if active, ok := raw["stop_hook_active"]; ok {
				if boolVal, isBool := active.(bool); isBool && boolVal {
					return false, ""
				}
			}
		}
	}

	// Find bd binary (use our own executable path).
	bdPath, err := os.Executable()
	if err != nil {
		bdPath = "bd" // fallback to PATH lookup
	}

	cmd := exec.Command(bdPath, "decision", "stop-check", "--json")
	cmd.Stderr = os.Stderr // pass through stderr for progress messages
	stdout, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Exit 1 = block (human said "continue").
			var resp struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason"`
			}
			if jsonErr := json.Unmarshal(stdout, &resp); jsonErr == nil && resp.Decision == "block" {
				return true, resp.Reason
			}
			return true, strings.TrimSpace(string(stdout))
		}
		// Other errors — allow stop (fail-open).
		return false, ""
	}

	// Exit 0 = allow stop.
	return false, ""
}

func init() {
	busEmitCmd.Flags().String("hook", "", "Hook event type (e.g., Stop, PreToolUse, SessionStart)")
	busEmitCmd.Flags().String("event", "", "Non-hook event type (e.g., DecisionCreated, DecisionResponded)")
	busEmitCmd.Flags().String("payload", "", "JSON payload for --event (alternative to stdin)")
	busCmd.AddCommand(busEmitCmd)
}
