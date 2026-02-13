package eventbus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMailNudgeHandlerMetadata(t *testing.T) {
	h := &MailNudgeHandler{}
	if h.ID() != "mail-nudge" {
		t.Errorf("ID() = %q, want %q", h.ID(), "mail-nudge")
	}
	if h.Priority() != 50 {
		t.Errorf("Priority() = %d, want 50", h.Priority())
	}
	if len(h.Handles()) != 1 || h.Handles()[0] != EventMailSent {
		t.Errorf("Handles() = %v, want [MailSent]", h.Handles())
	}
}

func TestMailNudgeHandler_EmptyTo(t *testing.T) {
	h := &MailNudgeHandler{}
	payload := MailEventPayload{From: "mayor/", To: "", Subject: "hello"}
	raw, _ := json.Marshal(payload)
	event := &Event{Type: EventMailSent, Raw: raw}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Errorf("expected nil error for empty To, got: %v", err)
	}
}

func TestMailNudgeHandler_BadPayload(t *testing.T) {
	h := &MailNudgeHandler{}
	event := &Event{Type: EventMailSent, Raw: json.RawMessage(`{invalid`)}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err == nil {
		t.Error("expected error for invalid JSON payload")
	}
}

func TestMailAddressToAgentID(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		// Town-level agents
		{"mayor", "gt-mayor"},
		{"mayor/", "gt-mayor"},
		{"deacon", "gt-deacon"},
		{"deacon/", "gt-deacon"},

		// Rig-scoped: known roles
		{"gastown/witness", "gt-gastown-witness"},
		{"gastown/refinery", "gt-gastown-refinery"},

		// Rig-scoped: default to polecat
		{"gastown/Toast", "gt-gastown-polecat-Toast"},
		{"dev/worker1", "gt-dev-polecat-worker1"},

		// Three-part: explicit polecats
		{"gastown/polecats/Toast", "gt-gastown-polecat-Toast"},
		{"dev/polecats/p1", "gt-dev-polecat-p1"},

		// Three-part: crew
		{"gastown/crew/max", "gt-gastown-crew-max"},
		{"hq/crew/test", "gt-hq-crew-test"},

		// Unrecognized patterns
		{"", ""},
		{"a/b/c/d", ""},           // Too many parts
		{"gastown/unknown/foo", ""}, // Unknown middle component
	}

	for _, tt := range tests {
		got := mailAddressToAgentID(tt.address)
		if got != tt.want {
			t.Errorf("mailAddressToAgentID(%q) = %q, want %q", tt.address, got, tt.want)
		}
	}
}

func TestMailNudgeHandler_PostNudge_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/nudge" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["message"] == "" {
			t.Error("expected non-empty message in nudge request")
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"delivered":    true,
			"state_before": "idle",
		})
	}))
	defer server.Close()

	h := &MailNudgeHandler{httpClient: server.Client()}
	delivered, reason, err := postNudge(context.Background(), h.httpClient, server.URL, "You have new mail")
	if err != nil {
		t.Fatalf("postNudge error: %v", err)
	}
	if !delivered {
		t.Errorf("expected delivered=true, got false (reason: %s)", reason)
	}
}

func TestMailNudgeHandler_PostNudge_NotDelivered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"delivered":    false,
			"state_before": "working",
			"reason":       "agent is busy",
		})
	}))
	defer server.Close()

	h := &MailNudgeHandler{httpClient: server.Client()}
	delivered, reason, err := postNudge(context.Background(), h.httpClient, server.URL, "test")
	if err != nil {
		t.Fatalf("postNudge error: %v", err)
	}
	if delivered {
		t.Error("expected delivered=false")
	}
	if reason != "agent is busy" {
		t.Errorf("expected reason=%q, got %q", "agent is busy", reason)
	}
}

func TestMailNudgeHandler_PostNudge_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	h := &MailNudgeHandler{httpClient: server.Client()}
	_, _, err := postNudge(context.Background(), h.httpClient, server.URL, "test")
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

func TestPostNudge_ConnectionRefused(t *testing.T) {
	_, _, err := postNudge(context.Background(), nil, "http://127.0.0.1:1", "test")
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

func TestDefaultMailHandlers(t *testing.T) {
	handlers := DefaultMailHandlers()
	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}
	if handlers[0].ID() != "mail-nudge" {
		t.Errorf("expected handler[0] ID %q, got %q", "mail-nudge", handlers[0].ID())
	}
	if handlers[1].ID() != "decision-nudge" {
		t.Errorf("expected handler[1] ID %q, got %q", "decision-nudge", handlers[1].ID())
	}
}

// TestMailNudgeInDefaultHandlers verifies the mail nudge handler is included
// in the default handler set registered at daemon startup.
func TestMailNudgeInDefaultHandlers(t *testing.T) {
	handlers := DefaultHandlers()
	found := false
	for _, h := range handlers {
		if h.ID() == "mail-nudge" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mail-nudge handler not found in DefaultHandlers()")
	}
}

func TestDecisionNudgeHandlerMetadata(t *testing.T) {
	h := &DecisionNudgeHandler{}
	if h.ID() != "decision-nudge" {
		t.Errorf("ID() = %q, want %q", h.ID(), "decision-nudge")
	}
	if h.Priority() != 50 {
		t.Errorf("Priority() = %d, want 50", h.Priority())
	}
	if len(h.Handles()) != 1 || h.Handles()[0] != EventDecisionResponded {
		t.Errorf("Handles() = %v, want [DecisionResponded]", h.Handles())
	}
}

func TestDecisionNudgeHandler_EmptyRequestedBy(t *testing.T) {
	h := &DecisionNudgeHandler{}
	payload := DecisionEventPayload{DecisionID: "test-123", RequestedBy: ""}
	raw, _ := json.Marshal(payload)
	event := &Event{Type: EventDecisionResponded, Raw: raw}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err != nil {
		t.Errorf("expected nil error for empty RequestedBy, got: %v", err)
	}
}

func TestDecisionNudgeHandler_BadPayload(t *testing.T) {
	h := &DecisionNudgeHandler{}
	event := &Event{Type: EventDecisionResponded, Raw: json.RawMessage(`{invalid`)}
	result := &Result{}

	err := h.Handle(context.Background(), event, result)
	if err == nil {
		t.Error("expected error for invalid JSON payload")
	}
}

func TestDecisionNudgeInDefaultHandlers(t *testing.T) {
	handlers := DefaultHandlers()
	found := false
	for _, h := range handlers {
		if h.ID() == "decision-nudge" {
			found = true
			break
		}
	}
	if !found {
		t.Error("decision-nudge handler not found in DefaultHandlers()")
	}
}

// TestMailEventSubjectRouting verifies MailSent events route to the mail.> subject.
func TestMailEventSubjectRouting(t *testing.T) {
	subject := SubjectForEvent(EventMailSent)
	if subject != "mail.MailSent" {
		t.Errorf("SubjectForEvent(MailSent) = %q, want %q", subject, "mail.MailSent")
	}
}

// TestMailEventIsMailEvent verifies MailSent is classified correctly.
func TestMailEventIsMailEvent(t *testing.T) {
	if !EventMailSent.IsMailEvent() {
		t.Error("expected EventMailSent.IsMailEvent() = true")
	}
	if EventSessionStart.IsMailEvent() {
		t.Error("expected EventSessionStart.IsMailEvent() = false")
	}
	if EventOjJobCreated.IsMailEvent() {
		t.Error("expected EventOjJobCreated.IsMailEvent() = false")
	}
}
