package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

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
  2. Otherwise: create local bus with default handlers and dispatch

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
		// Local dispatch: create a bus with default handlers.
		bus := eventbus.New()
		for _, h := range eventbus.DefaultHandlers() {
			bus.Register(h)
		}

		cwd, _ := os.Getwd()
		event := &eventbus.Event{
			Type:      eventbus.EventType(resolvedType),
			SessionID: eventMeta.SessionID,
			Raw:       eventData,
			CWD:       cwd,
		}

		// Parse remaining fields from stdin/payload JSON into the event.
		if len(eventData) > 0 {
			_ = json.Unmarshal(eventData, event)
			// Ensure Type and CWD are not overwritten by JSON field.
			event.Type = eventbus.EventType(resolvedType)
			event.CWD = cwd
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

func init() {
	busEmitCmd.Flags().String("hook", "", "Hook event type (e.g., Stop, PreToolUse, SessionStart)")
	busEmitCmd.Flags().String("event", "", "Non-hook event type (e.g., DecisionCreated, DecisionResponded)")
	busEmitCmd.Flags().String("payload", "", "JSON payload for --event (alternative to stdin)")
	busCmd.AddCommand(busEmitCmd)
}
