package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubDeleteClient struct {
	calls []string
	err   error
}

func (s *stubDeleteClient) DeleteIssue(ctx context.Context, id string) error {
	s.calls = append(s.calls, id)
	if s.err != nil {
		return s.err
	}
	return nil
}

type deleteRecordingPublisher struct {
	events []IssueEvent
}

func (r *deleteRecordingPublisher) Publish(evt IssueEvent) {
	r.events = append(r.events, evt)
}

func TestDeleteHandlerMethodNotAllowed(t *testing.T) {
	handler := NewDeleteHandler(&stubDeleteClient{})

	req := httptest.NewRequest(http.MethodGet, "/api/issues/ui-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestDeleteHandlerUnavailable(t *testing.T) {
	handler := NewDeleteHandler(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/ui-1?confirm=ui-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "issue deletion unavailable") {
		t.Fatalf("expected body to mention unavailable, got %q", rec.Body.String())
	}
}

func TestDeleteHandlerInvalidPath(t *testing.T) {
	handler := NewDeleteHandler(&stubDeleteClient{})

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteHandlerRequiresConfirmation(t *testing.T) {
	handler := NewDeleteHandler(&stubDeleteClient{})

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/ui-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "confirmation") {
		t.Fatalf("expected confirmation error, got %q", rec.Body.String())
	}
}

func TestDeleteHandlerConfirmationMismatch(t *testing.T) {
	handler := NewDeleteHandler(&stubDeleteClient{})

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/ui-1?confirm=ui-2", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "confirmation does not match") {
		t.Fatalf("expected mismatch error, got %q", rec.Body.String())
	}
}

func TestDeleteHandlerMapsNotFound(t *testing.T) {
	client := &stubDeleteClient{
		err: errors.New("issue not found: ui-1"),
	}
	handler := NewDeleteHandler(client)

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/ui-1?confirm=ui-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if len(client.calls) != 1 || client.calls[0] != "ui-1" {
		t.Fatalf("expected delete call for ui-1, got %v", client.calls)
	}
}

func TestDeleteHandlerSuccessPublishesEvent(t *testing.T) {
	client := &stubDeleteClient{}
	publisher := &deleteRecordingPublisher{}
	handler := NewDeleteHandler(client, WithDeleteEventPublisher(publisher))

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/UI-9?confirm=ui-9", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if len(client.calls) != 1 || client.calls[0] != "UI-9" {
		t.Fatalf("expected delete call for UI-9, got %v", client.calls)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected one event, got %d", len(publisher.events))
	}
	event := publisher.events[0]
	if event.Type != EventTypeDeleted {
		t.Fatalf("expected deleted event, got %s", event.Type)
	}
	if event.Issue.ID != "UI-9" {
		t.Fatalf("expected event for UI-9, got %s", event.Issue.ID)
	}
}

func TestMapDeleteError(t *testing.T) {
	if status := mapDeleteError(errors.New("issue not found")); status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
	if status := mapDeleteError(context.Canceled); status != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 for canceled context, got %d", status)
	}
	if status := mapDeleteError(errors.New("unexpected failure")); status != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", status)
	}
}

func TestParseDeletePath(t *testing.T) {
	if id, ok := parseDeletePath("/api/issues/ui-1"); !ok || id != "ui-1" {
		t.Fatalf("expected ui-1, got %q (ok=%v)", id, ok)
	}
	if _, ok := parseDeletePath("/api/issues/ui-1/labels"); ok {
		t.Fatalf("expected composite path to be invalid")
	}
}
