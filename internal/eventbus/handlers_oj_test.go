package eventbus

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestOjJobCompleteHandlerMetadata(t *testing.T) {
	h := &OjJobCompleteHandler{}
	if h.ID() != "oj-job-complete" {
		t.Errorf("ID() = %q, want %q", h.ID(), "oj-job-complete")
	}
	if h.Priority() != 40 {
		t.Errorf("Priority() = %d, want 40", h.Priority())
	}
	if len(h.Handles()) != 1 || h.Handles()[0] != EventOjJobCompleted {
		t.Errorf("Handles() = %v, want [OjJobCompleted]", h.Handles())
	}
}

func TestOjJobFailHandlerMetadata(t *testing.T) {
	h := &OjJobFailHandler{}
	if h.ID() != "oj-job-fail" {
		t.Errorf("ID() = %q, want %q", h.ID(), "oj-job-fail")
	}
	if h.Priority() != 40 {
		t.Errorf("Priority() = %d, want 40", h.Priority())
	}
	if len(h.Handles()) != 1 || h.Handles()[0] != EventOjJobFailed {
		t.Errorf("Handles() = %v, want [OjJobFailed]", h.Handles())
	}
}

func TestOjStepHandlerMetadata(t *testing.T) {
	h := &OjStepHandler{}
	if h.ID() != "oj-step" {
		t.Errorf("ID() = %q, want %q", h.ID(), "oj-step")
	}
	if h.Priority() != 40 {
		t.Errorf("Priority() = %d, want 40", h.Priority())
	}
	if len(h.Handles()) != 1 || h.Handles()[0] != EventOjStepAdvanced {
		t.Errorf("Handles() = %v, want [OjStepAdvanced]", h.Handles())
	}
}

func TestOjJobCompleteHandler_NoBead(t *testing.T) {
	h := &OjJobCompleteHandler{}
	payload := OjJobEventPayload{JobID: "job-1"}
	raw, _ := json.Marshal(payload)
	event := &Event{Type: EventOjJobCompleted, Raw: raw}
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, event, result)
	if err != nil {
		t.Errorf("expected nil error for no bead, got: %v", err)
	}
}

func TestOjJobFailHandler_NoBead(t *testing.T) {
	h := &OjJobFailHandler{}
	payload := OjJobEventPayload{JobID: "job-1", Error: "build failed"}
	raw, _ := json.Marshal(payload)
	event := &Event{Type: EventOjJobFailed, Raw: raw}
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, event, result)
	if err != nil {
		t.Errorf("expected nil error for no bead, got: %v", err)
	}
}

func TestOjStepHandler_NoBead(t *testing.T) {
	h := &OjStepHandler{}
	payload := OjStepEventPayload{JobID: "job-1", FromStep: "init", ToStep: "build"}
	raw, _ := json.Marshal(payload)
	event := &Event{Type: EventOjStepAdvanced, Raw: raw}
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, event, result)
	if err != nil {
		t.Errorf("expected nil error for no bead, got: %v", err)
	}
}

func TestOjJobCompleteHandler_EmptyPayload(t *testing.T) {
	h := &OjJobCompleteHandler{}
	event := &Event{Type: EventOjJobCompleted}
	result := &Result{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.Handle(ctx, event, result)
	if err == nil {
		t.Error("expected error for empty payload, got nil")
	}
}

func TestDefaultOjHandlers(t *testing.T) {
	handlers := DefaultOjHandlers()
	if len(handlers) != 3 {
		t.Fatalf("expected 3 OJ handlers, got %d", len(handlers))
	}

	ids := map[string]bool{}
	for _, h := range handlers {
		ids[h.ID()] = true
	}

	for _, expected := range []string{"oj-job-complete", "oj-job-fail", "oj-step"} {
		if !ids[expected] {
			t.Errorf("missing OJ handler: %s", expected)
		}
	}
}

func TestUnmarshalEventPayload(t *testing.T) {
	payload := OjJobEventPayload{
		JobID:   "j-42",
		JobName: "Build X",
		BeadID:  "gt-abc",
	}
	raw, _ := json.Marshal(payload)
	event := &Event{Raw: raw}

	var out OjJobEventPayload
	if err := unmarshalEventPayload(event, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.JobID != "j-42" {
		t.Errorf("JobID = %q, want %q", out.JobID, "j-42")
	}
	if out.BeadID != "gt-abc" {
		t.Errorf("BeadID = %q, want %q", out.BeadID, "gt-abc")
	}
}

func TestUnmarshalEventPayload_Empty(t *testing.T) {
	event := &Event{}
	var out OjJobEventPayload
	if err := unmarshalEventPayload(event, &out); err == nil {
		t.Error("expected error for empty payload")
	}
}
