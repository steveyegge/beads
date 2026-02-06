package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/rpc"
)

// emitDecisionEvent dispatches a decision event through the event bus.
// It tries the daemon RPC first; if the daemon is unavailable it falls
// back to a local (handler-less) bus dispatch. Errors are logged to
// stderr but never propagated — event emission is supplementary.
func emitDecisionEvent(eventType eventbus.EventType, payload eventbus.DecisionEventPayload) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bus: marshal decision event: %v\n", err)
		return
	}

	// Prefer daemon RPC.
	if daemonClient != nil {
		emitArgs := &rpc.BusEmitArgs{
			HookType:  string(eventType),
			EventJSON: payloadJSON,
		}
		resp, err := daemonClient.Execute(rpc.OpBusEmit, emitArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bus: daemon RPC for %s failed: %v\n", eventType, err)
		} else if !resp.Success {
			fmt.Fprintf(os.Stderr, "bus: daemon error for %s: %s\n", eventType, resp.Error)
		}
		return
	}

	// Fallback: local dispatch (no handlers = fire-and-forget to JetStream if configured).
	bus := eventbus.New()
	event := &eventbus.Event{
		Type: eventType,
		Raw:  payloadJSON,
	}
	if _, err := bus.Dispatch(context.Background(), event); err != nil {
		fmt.Fprintf(os.Stderr, "bus: local dispatch for %s failed: %v\n", eventType, err)
	}
}
