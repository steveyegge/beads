package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

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

	fmt.Fprintf(os.Stderr, "bus_emit: type=%s payload=%d bytes\n", args.HookType, len(args.EventJSON))

	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	if bus == nil {
		fmt.Fprintf(os.Stderr, "bus_emit: no bus configured, returning no-op\n")
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

	timeout := s.requestTimeout
	if req.TimeoutMs > 0 {
		requested := time.Duration(req.TimeoutMs) * time.Millisecond
		if requested > time.Hour {
			requested = time.Hour
		}
		if requested > timeout {
			timeout = requested
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := bus.Dispatch(ctx, event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bus_emit: dispatch error for %s: %v\n", args.HookType, err)
		return Response{
			Success: false,
			Error:   fmt.Sprintf("dispatch error: %v", err),
		}
	}

	fmt.Fprintf(os.Stderr, "bus_emit: dispatched %s (jetstream=%v block=%v)\n",
		args.HookType, bus.JetStreamEnabled(), result.Block)

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
	natsHealthFn := s.natsHealthFn
	s.mu.RUnlock()

	result := BusStatusResult{}

	if bus != nil {
		result.HandlerCount = len(bus.Handlers())
		result.NATSEnabled = bus.JetStreamEnabled()
	}

	if natsHealthFn != nil {
		health := natsHealthFn()
		result.NATSEnabled = health.Enabled
		result.NATSStatus = health.Status
		result.NATSPort = health.Port
		result.Connections = health.Connections
		result.JetStream = health.JetStream
		result.Streams = health.Streams
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
			info := BusHandlerInfo{
				ID:       h.ID(),
				Priority: h.Priority(),
				Handles:  events,
			}
			if _, ok := h.(*eventbus.ExternalHandler); ok {
				info.External = true
			}
			handlers = append(handlers, info)
		}
	}

	data, _ := json.Marshal(BusHandlersResult{Handlers: handlers})
	return Response{Success: true, Data: data}
}

// handleBusRegister registers an external handler on the event bus. (bd-4q86.1)
func (s *Server) handleBusRegister(req *Request) Response {
	var args BusRegisterArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	if args.ID == "" {
		return Response{Success: false, Error: "id is required"}
	}
	if args.Command == "" {
		return Response{Success: false, Error: "command is required"}
	}
	if len(args.Events) == 0 {
		return Response{Success: false, Error: "events is required (at least one event type)"}
	}

	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	if bus == nil {
		return Response{Success: false, Error: "event bus not configured"}
	}

	cfg := eventbus.ExternalHandlerConfig{
		ID:       args.ID,
		Command:  args.Command,
		Events:   args.Events,
		Priority: args.Priority,
		Shell:    args.Shell,
	}

	// Remove existing handler with same ID (re-registration).
	bus.Unregister(args.ID)

	handler := eventbus.NewExternalHandler(cfg)
	bus.Register(handler)

	fmt.Fprintf(os.Stderr, "bus_register: registered handler %q (events=%v priority=%d persist=%v)\n",
		args.ID, args.Events, handler.Priority(), args.Persist)

	persisted := false
	if args.Persist {
		s.mu.RLock()
		store := s.storage
		s.mu.RUnlock()

		if store != nil {
			cfgJSON, err := json.Marshal(cfg)
			if err == nil {
				key := eventbus.HandlerConfigPrefix + args.ID
				if err := store.SetConfig(context.Background(), key, string(cfgJSON)); err != nil {
					fmt.Fprintf(os.Stderr, "bus_register: persist failed for %q: %v\n", args.ID, err)
				} else {
					persisted = true
				}
			}
		}
	}

	data, _ := json.Marshal(BusRegisterResult{ID: args.ID, Persisted: persisted})
	return Response{Success: true, Data: data}
}

// handleBusUnregister removes an external handler from the event bus. (bd-4q86.1)
func (s *Server) handleBusUnregister(req *Request) Response {
	var args BusUnregisterArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	if args.ID == "" {
		return Response{Success: false, Error: "id is required"}
	}

	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	if bus == nil {
		return Response{Success: false, Error: "event bus not configured"}
	}

	removed := bus.Unregister(args.ID)

	// Also remove from config table if persisted.
	persistRemoved := false
	s.mu.RLock()
	store := s.storage
	s.mu.RUnlock()

	if store != nil {
		key := eventbus.HandlerConfigPrefix + args.ID
		if val, err := store.GetConfig(context.Background(), key); err == nil && val != "" {
			if err := store.DeleteConfig(context.Background(), key); err == nil {
				persistRemoved = true
			}
		}
	}

	fmt.Fprintf(os.Stderr, "bus_unregister: removed=%v persisted=%v handler %q\n", removed, persistRemoved, args.ID)

	data, _ := json.Marshal(BusUnregisterResult{Removed: removed, Persisted: persistRemoved})
	return Response{Success: true, Data: data}
}

// AdviceEventPayload is the payload for advice CRUD bus events. (bd-z4cu.2)
type AdviceEventPayload struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Labels              []string `json:"labels,omitempty"`
	AdviceHookCommand   string   `json:"advice_hook_command,omitempty"`
	AdviceHookTrigger   string   `json:"advice_hook_trigger,omitempty"`
	AdviceHookTimeout   int      `json:"advice_hook_timeout,omitempty"`
	AdviceHookOnFailure string   `json:"advice_hook_on_failure,omitempty"`
}

// emitDecisionEvent dispatches a decision event to the event bus (and NATS
// JetStream) so that the Slack bot and other consumers are notified of
// decision lifecycle events.  No-op if the bus is nil.
func (s *Server) emitDecisionEvent(eventType eventbus.EventType, payload eventbus.DecisionEventPayload) {
	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	if bus == nil {
		return
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emitDecisionEvent: marshal failed: %v\n", err)
		return
	}

	event := &eventbus.Event{
		Type: eventType,
		Raw:  raw,
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()

	if _, err := bus.Dispatch(ctx, event); err != nil {
		fmt.Fprintf(os.Stderr, "emitDecisionEvent: dispatch %s failed: %v\n", eventType, err)
	}
}

// emitOjEvent dispatches an OddJobs lifecycle event to the event bus (and NATS
// JetStream) so that handlers and external consumers are notified of OJ
// lifecycle transitions.  No-op if the bus is nil.  (bd-2iae)
func (s *Server) emitOjEvent(eventType eventbus.EventType, payload interface{}) {
	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	if bus == nil {
		return
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emitOjEvent: marshal failed: %v\n", err)
		return
	}

	event := &eventbus.Event{
		Type: eventType,
		Raw:  raw,
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()

	if _, err := bus.Dispatch(ctx, event); err != nil {
		fmt.Fprintf(os.Stderr, "emitOjEvent: dispatch %s failed: %v\n", eventType, err)
	}
}

// emitAdviceEvent dispatches an advice bus event if the bus is configured. (bd-z4cu.2)
// No-op if bus is nil — CRUD operations still succeed without a bus.
func (s *Server) emitAdviceEvent(eventType eventbus.EventType, payload AdviceEventPayload) {
	s.mu.RLock()
	bus := s.bus
	s.mu.RUnlock()

	if bus == nil {
		return
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}

	event := &eventbus.Event{
		Type: eventType,
		Raw:  raw,
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()

	bus.Dispatch(ctx, event)
}
