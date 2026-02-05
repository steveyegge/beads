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
	Short: "Dispatch a hook event through the event bus",
	Long: `Dispatch a Claude Code hook event through the event bus.

Reads the hook event JSON from stdin (as provided by Claude Code hooks)
and dispatches it through registered handlers.

Dispatch priority:
  1. If bd daemon is running (RPC): send to daemon
  2. Otherwise: create local bus and dispatch (no handlers = passthrough)

Exit codes:
  0 - Event processed, no blocking
  2 - Event blocked by a handler (gate check failed)

Examples:
  # In Claude Code settings.json hook commands:
  bd bus emit --hook=Stop
  bd bus emit --hook=PreToolUse
  bd bus emit --hook=SessionStart`,
	RunE: runBusEmit,
}

func runBusEmit(cmd *cobra.Command, args []string) error {
	hookType, _ := cmd.Flags().GetString("hook")
	if hookType == "" {
		return fmt.Errorf("--hook flag is required")
	}

	// Read stdin (Claude Code sends hook event JSON on stdin).
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Extract session_id from the event JSON if present.
	var eventMeta struct {
		SessionID string `json:"session_id"`
	}
	if len(stdinData) > 0 {
		_ = json.Unmarshal(stdinData, &eventMeta)
	}

	// Try daemon RPC first.
	if daemonClient != nil {
		emitArgs := &rpc.BusEmitArgs{
			HookType:  hookType,
			EventJSON: stdinData,
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
			return outputEmitResult(&result)
		}
	}

	// Local dispatch: create a bus with no handlers (passthrough).
	bus := eventbus.New()
	event := &eventbus.Event{
		Type:      eventbus.EventType(hookType),
		SessionID: eventMeta.SessionID,
		Raw:       stdinData,
	}

	// Parse remaining fields from stdin JSON.
	if len(stdinData) > 0 {
		_ = json.Unmarshal(stdinData, event)
	}

	result, err := bus.Dispatch(context.Background(), event)
	if err != nil {
		return fmt.Errorf("local dispatch: %w", err)
	}

	return outputEmitResult(&rpc.BusEmitResult{
		Block:    result.Block,
		Reason:   result.Reason,
		Inject:   result.Inject,
		Warnings: result.Warnings,
	})
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
	_ = busEmitCmd.MarkFlagRequired("hook")
	busCmd.AddCommand(busEmitCmd)
}
