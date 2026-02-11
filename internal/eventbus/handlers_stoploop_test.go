package eventbus

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func makeStopEvent(sessionID string, reentry bool) *Event {
	raw := map[string]interface{}{
		"session_id":       sessionID,
		"stop_hook_active": reentry,
	}
	data, _ := json.Marshal(raw)
	return &Event{
		Type:      EventStop,
		SessionID: sessionID,
		Raw:       data,
	}
}

func TestStopLoopDetector_FirstAttemptPassesThrough(t *testing.T) {
	d := &StopLoopDetector{Threshold: 3, WindowDuration: 60 * time.Second}
	event := makeStopEvent("sess-1", false)
	result := &Result{}

	if err := d.Handle(context.Background(), event, result); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if result.Block {
		t.Error("first attempt should not block")
	}
	if len(result.Inject) > 0 {
		t.Error("first attempt should not inject anything")
	}
}

func TestStopLoopDetector_ReentryUnderThresholdPassesThrough(t *testing.T) {
	d := &StopLoopDetector{Threshold: 3, WindowDuration: 60 * time.Second}
	result := &Result{}

	// First attempt (not reentry)
	d.Handle(context.Background(), makeStopEvent("sess-1", false), result)
	// Second attempt (reentry)
	d.Handle(context.Background(), makeStopEvent("sess-1", true), result)

	if result.Block {
		t.Error("under threshold should not block")
	}
	if len(result.Inject) > 0 {
		t.Error("under threshold should not inject")
	}
}

func TestStopLoopDetector_ThresholdTriggersLoopBreak(t *testing.T) {
	d := &StopLoopDetector{Threshold: 3, WindowDuration: 60 * time.Second}

	// First attempt (not reentry) — recorded
	d.Handle(context.Background(), makeStopEvent("sess-1", false), &Result{})

	// Reentries 2 and 3
	d.Handle(context.Background(), makeStopEvent("sess-1", true), &Result{})

	result := &Result{}
	d.Handle(context.Background(), makeStopEvent("sess-1", true), result)

	if len(result.Inject) == 0 {
		t.Fatal("at threshold, should inject loop warning")
	}
	if result.Block {
		t.Error("loop break should not set Block=true")
	}

	// Verify the loop-break flag is set in event.Raw
	event := makeStopEvent("sess-1", true)
	d.Handle(context.Background(), event, &Result{})

	// After threshold + clear, a new reentry starts counting fresh
	// (window was cleared, so this is attempt 1 under threshold)
	if stopLoopBreakSet(event) {
		t.Error("after clear, next attempt should not have loop break flag")
	}
}

func TestStopLoopDetector_SetsLoopBreakFlag(t *testing.T) {
	d := &StopLoopDetector{Threshold: 2, WindowDuration: 60 * time.Second}

	// First attempt
	d.Handle(context.Background(), makeStopEvent("sess-1", false), &Result{})

	// Second attempt (reentry) — hits threshold of 2
	event := makeStopEvent("sess-1", true)
	result := &Result{}
	d.Handle(context.Background(), event, result)

	if !stopLoopBreakSet(event) {
		t.Error("should set stop_loop_break flag in event.Raw at threshold")
	}
	if len(result.Inject) == 0 {
		t.Error("should inject warning at threshold")
	}
}

func TestStopLoopDetector_SeparateSessionsAreIndependent(t *testing.T) {
	d := &StopLoopDetector{Threshold: 3, WindowDuration: 60 * time.Second}

	// Session A: 3 attempts
	d.Handle(context.Background(), makeStopEvent("sess-a", false), &Result{})
	d.Handle(context.Background(), makeStopEvent("sess-a", true), &Result{})
	result := &Result{}
	d.Handle(context.Background(), makeStopEvent("sess-a", true), result)

	if len(result.Inject) == 0 {
		t.Error("session A should trigger at 3 attempts")
	}

	// Session B: only 1 attempt — should NOT trigger
	resultB := &Result{}
	d.Handle(context.Background(), makeStopEvent("sess-b", false), resultB)

	if len(resultB.Inject) > 0 {
		t.Error("session B should not trigger with only 1 attempt")
	}
}

func TestStopLoopDetector_WindowExpiry(t *testing.T) {
	d := &StopLoopDetector{Threshold: 3, WindowDuration: 50 * time.Millisecond}

	// Two attempts
	d.Handle(context.Background(), makeStopEvent("sess-1", false), &Result{})
	d.Handle(context.Background(), makeStopEvent("sess-1", true), &Result{})

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Third attempt — but first two have expired
	result := &Result{}
	d.Handle(context.Background(), makeStopEvent("sess-1", true), result)

	if len(result.Inject) > 0 {
		t.Error("should not trigger — old attempts expired from window")
	}
}

func TestStopLoopDetector_ClearsAfterDetection(t *testing.T) {
	d := &StopLoopDetector{Threshold: 2, WindowDuration: 60 * time.Second}

	// Trigger detection
	d.Handle(context.Background(), makeStopEvent("sess-1", false), &Result{})
	d.Handle(context.Background(), makeStopEvent("sess-1", true), &Result{})

	// Next attempt should start fresh (window was cleared)
	result := &Result{}
	d.Handle(context.Background(), makeStopEvent("sess-1", false), result)

	if len(result.Inject) > 0 {
		t.Error("after detection + clear, next attempt should not trigger")
	}
}

func TestStopDecisionHandler_RespectsLoopBreakFlag(t *testing.T) {
	event := makeStopEvent("sess-1", true)
	// Set the loop break flag manually
	var raw map[string]interface{}
	json.Unmarshal(event.Raw, &raw)
	raw["stop_loop_break"] = true
	event.Raw, _ = json.Marshal(raw)

	if !stopLoopBreakSet(event) {
		t.Fatal("stopLoopBreakSet should return true")
	}
}

func TestStopLoopBreakSet_FalseByDefault(t *testing.T) {
	event := makeStopEvent("sess-1", false)
	if stopLoopBreakSet(event) {
		t.Error("should be false by default")
	}
}

func TestStopLoopBreakSet_EmptyRaw(t *testing.T) {
	event := &Event{Type: EventStop, Raw: nil}
	if stopLoopBreakSet(event) {
		t.Error("should be false with nil Raw")
	}
}

func TestIsReentry_True(t *testing.T) {
	event := makeStopEvent("sess-1", true)
	if !isReentry(event) {
		t.Error("should detect reentry when stop_hook_active=true")
	}
}

func TestIsReentry_False(t *testing.T) {
	event := makeStopEvent("sess-1", false)
	if isReentry(event) {
		t.Error("should not detect reentry when stop_hook_active=false")
	}
}

func TestIsReentry_NoRaw(t *testing.T) {
	event := &Event{Type: EventStop, Raw: nil}
	if isReentry(event) {
		t.Error("should not detect reentry with nil Raw")
	}
}
