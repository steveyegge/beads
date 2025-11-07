package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

type stubCreateClient struct {
	args *rpc.CreateArgs
	resp *rpc.Response
	err  error
}

func (s *stubCreateClient) Create(args *rpc.CreateArgs) (*rpc.Response, error) {
	s.args = args
	if s.err != nil || s.resp != nil {
		return s.resp, s.err
	}
	return &rpc.Response{Success: true}, nil
}

type stubCreatePublisher struct {
	events []IssueEvent
}

func (s *stubCreatePublisher) Publish(evt IssueEvent) {
	s.events = append(s.events, evt)
}

func TestCreateHandlerCreatesIssue(t *testing.T) {
	now := time.Date(2025, 10, 23, 8, 0, 0, 0, time.UTC)
	client := &stubCreateClient{
		resp: &rpc.Response{
			Success: true,
			Data: mustJSON(struct {
				*types.Issue
			}{
				Issue: &types.Issue{
					ID:        "ui-900",
					Title:     "Quick create",
					Status:    types.StatusOpen,
					IssueType: types.TypeTask,
					Priority:  1,
					Labels:    []string{"ui"},
					UpdatedAt: now,
				},
			}),
		},
	}
	publisher := &stubCreatePublisher{}

	handler := NewCreateHandler(client, publisher)

	body := bytes.NewBufferString(`{
		"title": "Quick create",
		"description": "via modal",
		"priority": 1,
		"issue_type": "task",
		"discovered_from": "bd-123",
		"labels": ["ui"]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/api/issues/ui-900" {
		t.Fatalf("expected Location header, got %q", location)
	}

	if client.args == nil {
		t.Fatalf("expected Create to be called")
	}
	if client.args.Title != "Quick create" {
		t.Fatalf("unexpected title: %q", client.args.Title)
	}
	if client.args.Priority != 1 {
		t.Fatalf("unexpected priority: %d", client.args.Priority)
	}
	if client.args.IssueType != "task" {
		t.Fatalf("unexpected issue type: %s", client.args.IssueType)
	}
	if len(client.args.Dependencies) != 1 || client.args.Dependencies[0] != "bd-123" {
		t.Fatalf("expected discovered_from dependency, got %#v", client.args.Dependencies)
	}
	if len(client.args.Labels) != 1 || client.args.Labels[0] != "ui" {
		t.Fatalf("expected labels propagated, got %#v", client.args.Labels)
	}

	var payload struct {
		Issue IssueSummary `json:"issue"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Issue.ID != "ui-900" || payload.Issue.Status != string(types.StatusOpen) {
		t.Fatalf("unexpected issue summary: %#v", payload.Issue)
	}
	if len(payload.Issue.Labels) != 1 || payload.Issue.Labels[0] != "ui" {
		t.Fatalf("expected labels in summary, got %#v", payload.Issue.Labels)
	}

	if len(publisher.events) != 1 {
		t.Fatalf("expected single SSE event, got %d", len(publisher.events))
	}
	if publisher.events[0].Type != EventTypeCreated {
		t.Fatalf("expected created event, got %s", publisher.events[0].Type)
	}
	if publisher.events[0].Issue.ID != "ui-900" {
		t.Fatalf("expected event for ui-900, got %s", publisher.events[0].Issue.ID)
	}
}

func TestCreateHandlerRejectsMissingTitle(t *testing.T) {
	handler := NewCreateHandler(&stubCreateClient{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"   "}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateHandlerMapsDaemonValidationErrors(t *testing.T) {
	client := &stubCreateClient{
		resp: &rpc.Response{
			Success: false,
			Error:   "validation failed: title is required",
		},
		err: errors.New("operation failed: validation failed: title is required"),
	}

	handler := NewCreateHandler(client, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Quick create"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "title is required") {
		t.Fatalf("expected error body to mention missing title, got %q", body)
	}
}

func TestCreateHandlerHonorsResponseStatusCode(t *testing.T) {
	client := &stubCreateClient{
		resp: &rpc.Response{
			Success:    false,
			Error:      "incompatible major versions: client 0.9.10, daemon 0.12.0. Client is older; upgrade the bd CLI to match the daemon's major version",
			StatusCode: http.StatusUpgradeRequired,
		},
		err: errors.New("operation failed: incompatible major versions: client 0.9.10, daemon 0.12.0. Client is older; upgrade the bd CLI to match the daemon's major version"),
	}

	handler := NewCreateHandler(client, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Seed"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("expected %d, got %d (%s)", http.StatusUpgradeRequired, rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "upgrade the bd CLI") {
		t.Fatalf("expected upgrade guidance, got %q", body)
	}
}

func TestCreateHandlerRejectsInvalidIssueType(t *testing.T) {
	handler := NewCreateHandler(&stubCreateClient{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Seed","issue_type":"unknown"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateHandlerRejectsInvalidPriority(t *testing.T) {
	handler := NewCreateHandler(&stubCreateClient{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Seed","priority":9}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateHandlerUnavailableWithoutClient(t *testing.T) {
	handler := NewCreateHandler(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Seed"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestCreateHandlerClientErrorWithoutResponse(t *testing.T) {
	client := &stubCreateClient{
		err: errors.New("transport failure"),
	}
	handler := NewCreateHandler(client, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Seed"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "transport failure") {
		t.Fatalf("expected transport failure message, got %q", rec.Body.String())
	}
}

func TestDecodeCreatedIssue(t *testing.T) {
	issue := &types.Issue{ID: "ui-501", Title: "Created"}
	raw, _ := json.Marshal(issue)

	if got, err := decodeCreatedIssue(raw); err != nil || got.ID != "ui-501" {
		t.Fatalf("direct decode mismatch: issue=%+v err=%v", got, err)
	}

	wrapped, _ := json.Marshal(map[string]any{"issue": issue})
	if got, err := decodeCreatedIssue(wrapped); err != nil || got.Title != "Created" {
		t.Fatalf("wrapped decode mismatch: issue=%+v err=%v", got, err)
	}

	if _, err := decodeCreatedIssue(nil); err == nil {
		t.Fatalf("expected error for empty payload")
	}
	if _, err := decodeCreatedIssue([]byte(`{"unexpected":true}`)); err == nil {
		t.Fatalf("expected error for unexpected payload")
	}
}

func TestNormalizeLabels(t *testing.T) {
	input := []string{" UI ", "backend", "ui", ""}
	got := normalizeLabels(input)
	if len(got) != 2 || got[0] != "UI" || got[1] != "backend" {
		t.Fatalf("unexpected normalized labels: %#v", got)
	}
	if res := normalizeLabels([]string{"   "}); res != nil {
		t.Fatalf("expected nil when no usable labels, got %#v", res)
	}
}

func TestMapCreateError(t *testing.T) {
	if status := mapCreateError("invalid status"); status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	if status := mapCreateError("duplicate title"); status != http.StatusConflict {
		t.Fatalf("expected 409, got %d", status)
	}
	if status := mapCreateError("daemon unreachable"); status != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", status)
	}
}

func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestCreateHandlerRejectsInvalidJSON(t *testing.T) {
	handler := NewCreateHandler(&stubCreateClient{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString("{invalid"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateHandlerHandlesNilResponse(t *testing.T) {
	client := &stubCreateClient{
		resp: nil,
	}
	handler := NewCreateHandler(client, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Seed"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

func TestCreateHandlerHandlesDecodeFailure(t *testing.T) {
	client := &stubCreateClient{
		resp: &rpc.Response{Success: true, Data: []byte(`{"unexpected":true}`)},
	}
	handler := NewCreateHandler(client, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues", bytes.NewBufferString(`{"title":"Seed"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "decode issue") {
		t.Fatalf("expected decode error, got %s", rec.Body.String())
	}
}
