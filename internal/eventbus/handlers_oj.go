package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
)

// OjJobCompleteHandler closes the associated bead when an OJ job completes.
// Priority 40 (runs after standard hook handlers).
type OjJobCompleteHandler struct{}

func (h *OjJobCompleteHandler) ID() string          { return "oj-job-complete" }
func (h *OjJobCompleteHandler) Handles() []EventType { return []EventType{EventOjJobCompleted} }
func (h *OjJobCompleteHandler) Priority() int        { return 40 }

func (h *OjJobCompleteHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	var payload OjJobEventPayload
	if err := unmarshalEventPayload(event, &payload); err != nil {
		return fmt.Errorf("oj-job-complete: %w", err)
	}
	if payload.BeadID == "" {
		return nil // No bead associated, nothing to sync.
	}

	reason := fmt.Sprintf("OJ job %s completed", payload.JobID)
	_, _, err := runBDCommand(ctx, event.CWD, "close", payload.BeadID, "--reason="+reason)
	if err != nil {
		return fmt.Errorf("oj-job-complete: bd close %s: %w", payload.BeadID, err)
	}
	return nil
}

// OjJobFailHandler marks the associated bead as blocked when an OJ job fails.
// Priority 40.
type OjJobFailHandler struct{}

func (h *OjJobFailHandler) ID() string          { return "oj-job-fail" }
func (h *OjJobFailHandler) Handles() []EventType { return []EventType{EventOjJobFailed} }
func (h *OjJobFailHandler) Priority() int        { return 40 }

func (h *OjJobFailHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	var payload OjJobEventPayload
	if err := unmarshalEventPayload(event, &payload); err != nil {
		return fmt.Errorf("oj-job-fail: %w", err)
	}
	if payload.BeadID == "" {
		return nil
	}

	errMsg := payload.Error
	if errMsg == "" {
		errMsg = fmt.Sprintf("OJ job %s failed (exit %d)", payload.JobID, payload.ExitCode)
	}

	// Add a comment with the failure reason, then label as blocked.
	_, _, _ = runBDCommand(ctx, event.CWD, "comment", payload.BeadID, "--body="+errMsg)
	_, _, err := runBDCommand(ctx, event.CWD, "label", payload.BeadID, "--add=oj:failed")
	if err != nil {
		return fmt.Errorf("oj-job-fail: bd label %s: %w", payload.BeadID, err)
	}
	return nil
}

// OjStepHandler updates bead labels when an OJ job advances to a new step.
// Priority 40.
type OjStepHandler struct{}

func (h *OjStepHandler) ID() string          { return "oj-step" }
func (h *OjStepHandler) Handles() []EventType { return []EventType{EventOjStepAdvanced} }
func (h *OjStepHandler) Priority() int        { return 40 }

func (h *OjStepHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	var payload OjStepEventPayload
	if err := unmarshalEventPayload(event, &payload); err != nil {
		return fmt.Errorf("oj-step: %w", err)
	}
	if payload.BeadID == "" {
		return nil
	}

	// Remove old step label, add new one.
	if payload.FromStep != "" {
		_, _, _ = runBDCommand(ctx, event.CWD, "label", payload.BeadID, "--remove=oj:step:"+payload.FromStep)
	}
	if payload.ToStep != "" {
		_, _, err := runBDCommand(ctx, event.CWD, "label", payload.BeadID, "--add=oj:step:"+payload.ToStep)
		if err != nil {
			return fmt.Errorf("oj-step: bd label %s: %w", payload.BeadID, err)
		}
	}
	return nil
}

// unmarshalEventPayload extracts a typed payload from the event's Raw field.
func unmarshalEventPayload(event *Event, dest interface{}) error {
	if len(event.Raw) == 0 {
		return fmt.Errorf("empty event payload")
	}
	return json.Unmarshal(event.Raw, dest)
}

// DefaultOjHandlers returns the OJ bead-sync handlers for daemon registration.
func DefaultOjHandlers() []Handler {
	return []Handler{
		&OjJobCompleteHandler{}, // 40
		&OjJobFailHandler{},     // 40
		&OjStepHandler{},        // 40
	}
}
