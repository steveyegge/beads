package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// StopLoopDetector detects rapid Stop→Block cycles that indicate an agent is
// stuck in a stop hook loop (e.g., due to expired auth tokens preventing
// decision creation). When the threshold is reached, it short-circuits the
// handler chain by setting result.Block = false, allowing the agent to stop
// and breaking the loop.
//
// The detector maintains a per-session sliding window of stop attempts. When
// an agent hits >= Threshold attempts within WindowDuration, the detector
// fires and publishes a StopLoopDetected event to AGENT_EVENTS for
// observability.
//
// Priority 14 (before StopDecisionHandler at 15, so it can prevent blocking).
type StopLoopDetector struct {
	mu       sync.Mutex
	windows  map[string]*stopWindow // session_id -> window
	bus      *Bus                   // for JetStream publish (set after registration)

	// Configurable thresholds (defaults applied in Handle).
	Threshold      int           // stop attempts before triggering (default: 3)
	WindowDuration time.Duration // sliding window size (default: 120s)
}

// stopWindow tracks stop attempt timestamps for a single session.
type stopWindow struct {
	attempts []time.Time
}

func (h *StopLoopDetector) ID() string           { return "stop-loop-detector" }
func (h *StopLoopDetector) Handles() []EventType { return []EventType{EventStop} }
func (h *StopLoopDetector) Priority() int         { return 14 }

// SetBus allows the detector to publish loop-detected events to JetStream.
// Called after registration when the bus reference is available.
func (h *StopLoopDetector) SetBus(bus *Bus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bus = bus
}

func (h *StopLoopDetector) Handle(ctx context.Context, event *Event, result *Result) error {
	// Only act on re-entry (stop_hook_active=true), meaning Claude was already
	// blocked once and is trying again. First stop attempts are always legitimate.
	if !isReentry(event) {
		// First stop attempt — record it but don't interfere.
		h.recordAttempt(event.SessionID)
		return nil
	}

	// Record this re-entry attempt.
	count := h.recordAttempt(event.SessionID)

	threshold := h.Threshold
	if threshold <= 0 {
		threshold = 3
	}

	if count < threshold {
		// Under threshold — let StopDecisionHandler handle normally.
		return nil
	}

	// Loop detected — short-circuit the chain.
	windowDur := h.WindowDuration
	if windowDur <= 0 {
		windowDur = 120 * time.Second
	}

	log.Printf("stop-loop-detector: loop detected for session %s (%d attempts in %v), allowing stop",
		event.SessionID, count, windowDur)

	// Clear the window so next session starts fresh.
	h.clearSession(event.SessionID)

	// Inject warning so the agent (and transcript) records what happened.
	result.Inject = append(result.Inject, fmt.Sprintf(
		"⚠️ Stop hook loop detected (%d stop attempts in %v). "+
			"Allowing stop to break the loop. This usually means the agent could not "+
			"create a decision point (e.g., expired auth token). "+
			"Check agent logs and run /login if needed.",
		count, windowDur))

	// Publish observability event to AGENT_EVENTS stream.
	h.publishLoopDetected(event.SessionID, count, windowDur)

	// CRITICAL: Set Block=false explicitly. The StopDecisionHandler (priority 15)
	// runs AFTER us and would normally set Block=true. By returning nil here,
	// we don't prevent it from running. Instead, we need a mechanism to skip it.
	//
	// We signal "loop detected" via a well-known key in result.Inject that the
	// StopDecisionHandler can check. But since handlers can't read each other's
	// state, we use a simpler approach: set a flag in event.Raw that downstream
	// handlers can inspect.
	h.setLoopBreakFlag(event)

	return nil
}

// recordAttempt adds a timestamp to the session's window and returns the
// current count of attempts within the window.
func (h *StopLoopDetector) recordAttempt(sessionID string) int {
	if sessionID == "" {
		return 0
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.windows == nil {
		h.windows = make(map[string]*stopWindow)
	}

	w, ok := h.windows[sessionID]
	if !ok {
		w = &stopWindow{}
		h.windows[sessionID] = w
	}

	now := time.Now()
	w.attempts = append(w.attempts, now)

	// Prune entries outside window.
	windowDur := h.WindowDuration
	if windowDur <= 0 {
		windowDur = 120 * time.Second
	}
	cutoff := now.Add(-windowDur)
	pruned := w.attempts[:0]
	for _, t := range w.attempts {
		if !t.Before(cutoff) {
			pruned = append(pruned, t)
		}
	}
	w.attempts = pruned

	return len(w.attempts)
}

// clearSession removes the window for a session after loop detection.
func (h *StopLoopDetector) clearSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.windows, sessionID)
}

// isReentry checks the raw event for stop_hook_active=true.
func isReentry(event *Event) bool {
	if len(event.Raw) == 0 {
		return false
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(event.Raw, &raw); err != nil {
		return false
	}
	active, ok := raw["stop_hook_active"]
	if !ok {
		return false
	}
	b, ok := active.(bool)
	return ok && b
}

// setLoopBreakFlag modifies event.Raw to include stop_loop_break=true so
// downstream handlers (StopDecisionHandler) can skip blocking.
func (h *StopLoopDetector) setLoopBreakFlag(event *Event) {
	var raw map[string]interface{}
	if len(event.Raw) > 0 {
		if err := json.Unmarshal(event.Raw, &raw); err != nil {
			raw = make(map[string]interface{})
		}
	} else {
		raw = make(map[string]interface{})
	}
	raw["stop_loop_break"] = true
	if data, err := json.Marshal(raw); err == nil {
		event.Raw = data
	}
}

// publishLoopDetected publishes a StopLoopDetected event to the AGENT_EVENTS
// JetStream stream for observability (dashboards, alerts, Slack notifications).
func (h *StopLoopDetector) publishLoopDetected(sessionID string, count int, window time.Duration) {
	h.mu.Lock()
	bus := h.bus
	h.mu.Unlock()

	if bus == nil {
		return
	}

	payload := StopLoopPayload{
		SessionID:    sessionID,
		AttemptCount: count,
		WindowSecs:   int(window.Seconds()),
		Reason:       "rapid stop re-entry detected",
		DetectedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("stop-loop-detector: failed to marshal payload: %v", err)
		return
	}

	subject := SubjectForEvent(EventStopLoopDetected)
	bus.PublishRaw(subject, data)
}
