package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/eventbus"
)

// handleBusEmit dispatches a hook event through the event bus. (bd-66fp)
func (s *Server) handleBusEmit(req *Request) Response {
	var args BusEmitArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid arguments: %v", err),
		}
	}

	if args.HookType == "" {
		return Response{
			Success: false,
			Error:   "hook_type is required",
		}
	}

	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	if bus == nil {
		// No bus configured — passthrough (no handlers = no-op).
		data, _ := json.Marshal(BusEmitResult{})
		return Response{Success: true, Data: data}
	}

	// Build event from the raw JSON payload.
	event := &eventbus.Event{
		Type:      eventbus.EventType(args.HookType),
		SessionID: args.SessionID,
		Raw:       args.EventJSON,
	}

	// Parse additional fields from stdin JSON if present.
	if len(args.EventJSON) > 0 {
		_ = json.Unmarshal(args.EventJSON, event)
		// Ensure Type is set from args (JSON may have hook_event_name which would overwrite).
		event.Type = eventbus.EventType(args.HookType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()

	result, err := bus.Dispatch(ctx, event)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("dispatch error: %v", err),
		}
	}

	data, _ := json.Marshal(BusEmitResult{
		Block:    result.Block,
		Reason:   result.Reason,
		Inject:   result.Inject,
		Warnings: result.Warnings,
	})

	return Response{Success: true, Data: data}
}

// handleBusStatus returns event bus health and handler count. (bd-66fp)
func (s *Server) handleBusStatus(_ *Request) Response {
	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	result := BusStatusResult{
		NATSEnabled: false, // NATS status is set separately if enabled
	}

	if bus != nil {
		result.HandlerCount = len(bus.Handlers())
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleBusHandlers lists all registered event bus handlers. (bd-66fp)
func (s *Server) handleBusHandlers(_ *Request) Response {
	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	var handlers []BusHandlerInfo
	if bus != nil {
		for _, h := range bus.Handlers() {
			events := make([]string, len(h.Handles()))
			for i, e := range h.Handles() {
				events[i] = string(e)
			}
			handlers = append(handlers, BusHandlerInfo{
				ID:       h.ID(),
				Priority: h.Priority(),
				Handles:  events,
			})
		}
	}

	data, _ := json.Marshal(BusHandlersResult{Handlers: handlers})
	return Response{Success: true, Data: data}
}
