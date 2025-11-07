package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	uiapi "github.com/steveyegge/beads/internal/ui/api"
)

type stubUpdateClient struct {
	args *rpc.UpdateArgs
	resp *rpc.Response
	err  error
}

func (s *stubUpdateClient) Update(args *rpc.UpdateArgs) (*rpc.Response, error) {
	s.args = args
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &rpc.Response{Success: true}, nil
}

type stubEventPublisher struct {
	events []uiapi.IssueEvent
}

func (s *stubEventPublisher) Publish(evt uiapi.IssueEvent) {
	s.events = append(s.events, evt)
}

type stubDetailResponder struct {
	resp   *rpc.Response
	err    error
	calls  int
	lastID string
}

func (s *stubDetailResponder) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	s.calls++
	if args != nil {
		s.lastID = args.ID
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

type recordingLabelClient struct {
	added   []*rpc.LabelAddArgs
	removed []*rpc.LabelRemoveArgs
	addErr  error
	rmErr   error
}

func (c *recordingLabelClient) AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error) {
	c.added = append(c.added, args)
	if c.addErr != nil {
		return nil, c.addErr
	}
	return &rpc.Response{Success: true}, nil
}

func (c *recordingLabelClient) RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error) {
	c.removed = append(c.removed, args)
	if c.rmErr != nil {
		return nil, c.rmErr
	}
	return &rpc.Response{Success: true}, nil
}

type recordingLabelAdder struct {
	added []*rpc.LabelAddArgs
	err   error
}

func (c *recordingLabelAdder) AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error) {
	c.added = append(c.added, args)
	if c.err != nil {
		return nil, c.err
	}
	return &rpc.Response{Success: true}, nil
}

type recordingLabelRemover struct {
	removed []*rpc.LabelRemoveArgs
	err     error
}

func (c *recordingLabelRemover) RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error) {
	c.removed = append(c.removed, args)
	if c.err != nil {
		return nil, c.err
	}
	return &rpc.Response{Success: true}, nil
}

type stubDeleteClient struct {
	id     string
	called bool
	err    error
}

func (s *stubDeleteClient) DeleteIssue(_ context.Context, id string) error {
	s.called = true
	s.id = id
	return s.err
}

func TestStatusHandlerUpdatesIssue(t *testing.T) {
	issue := &types.Issue{
		ID:        "ui-42",
		Title:     "Polish command palette",
		Status:    types.StatusInProgress,
		IssueType: types.TypeFeature,
		Priority:  1,
		UpdatedAt: time.Date(2025, 10, 23, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(issue)
	if err != nil {
		t.Fatalf("marshal issue: %v", err)
	}

	stub := &stubUpdateClient{
		resp: &rpc.Response{
			Success: true,
			Data:    data,
		},
	}

	publisher := &stubEventPublisher{}

	handler := uiapi.NewStatusHandler(stub, uiapi.WithStatusEventPublisher(publisher))

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-42/status", bytes.NewReader([]byte(`{"status":"in_progress"}`)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if stub.args == nil {
		t.Fatalf("expected update client to be invoked")
	}
	if stub.args.ID != "ui-42" {
		t.Fatalf("expected ID ui-42, got %s", stub.args.ID)
	}
	if stub.args.Status == nil || *stub.args.Status != "in_progress" {
		t.Fatalf("expected status pointer to be in_progress, got %#v", stub.args.Status)
	}

	var payload struct {
		Issue uiapi.IssueSummary `json:"issue"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Issue.ID != "ui-42" || payload.Issue.Status != string(types.StatusInProgress) {
		t.Fatalf("unexpected issue summary: %#v", payload.Issue)
	}

	if len(publisher.events) != 1 {
		t.Fatalf("expected one published event, got %d", len(publisher.events))
	}
	if publisher.events[0].Type != uiapi.EventTypeUpdated {
		t.Fatalf("expected event type updated, got %s", publisher.events[0].Type)
	}
	if publisher.events[0].Issue.ID != "ui-42" {
		t.Fatalf("expected event for ui-42, got %s", publisher.events[0].Issue.ID)
	}
}

func TestStatusHandlerMissingStatus(t *testing.T) {
	handler := uiapi.NewStatusHandler(&stubUpdateClient{})

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-7/status", bytes.NewReader([]byte(`{"status":""}`)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestStatusHandlerMapsNotFound(t *testing.T) {
	stub := &stubUpdateClient{
		resp: &rpc.Response{
			Success: false,
			Error:   "issue not found",
		},
	}

	handler := uiapi.NewStatusHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-404/status", bytes.NewReader([]byte(`{"status":"ready"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestStatusHandlerUnavailableWithoutClient(t *testing.T) {
	handler := uiapi.NewStatusHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-5/status", bytes.NewReader([]byte(`{"status":"ready"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON response, got %q", ct)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json payload: %v", err)
	}
	if strings.TrimSpace(payload["error"]) == "" {
		t.Fatalf("expected error message in payload, got %#v", payload)
	}
}

func TestStatusHandlerRejectsInvalidPath(t *testing.T) {
	handler := uiapi.NewStatusHandler(&stubUpdateClient{})

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-9", bytes.NewReader([]byte(`{"status":"ready"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestStatusHandlerRejectsGet(t *testing.T) {
	handler := uiapi.NewStatusHandler(&stubUpdateClient{})

	req := httptest.NewRequest(http.MethodGet, "/api/issues/ui-9/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestStatusHandlerMapsInvalidInput(t *testing.T) {
	stub := &stubUpdateClient{
		resp: &rpc.Response{
			Success: false,
			Error:   "invalid status provided",
		},
	}
	handler := uiapi.NewStatusHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-10/status", bytes.NewReader([]byte(`{"status":"done"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestStatusHandlerMapsConflict(t *testing.T) {
	stub := &stubUpdateClient{
		resp: &rpc.Response{
			Success: false,
			Error:   "update failed: stale version",
		},
	}
	handler := uiapi.NewStatusHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-11/status", bytes.NewReader([]byte(`{"status":"ready"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestStatusHandlerMapsGateway(t *testing.T) {
	stub := &stubUpdateClient{
		resp: &rpc.Response{
			Success: false,
			Error:   "backend unavailable",
		},
	}
	handler := uiapi.NewStatusHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-12/status", bytes.NewReader([]byte(`{"status":"ready"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

func TestNewIssueHandlerRoutesLabelOperations(t *testing.T) {
	now := time.Date(2025, 10, 30, 17, 0, 0, 0, time.UTC)
	detailPayload := struct {
		*types.Issue
		Labels []string `json:"labels,omitempty"`
	}{
		Issue: &types.Issue{
			ID:        "ui-55",
			Title:     "Label target",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  2,
			UpdatedAt: now,
		},
		Labels: []string{"existing", "ready"},
	}
	data, err := json.Marshal(detailPayload)
	if err != nil {
		t.Fatalf("marshal detail payload: %v", err)
	}

	detail := &stubDetailResponder{
		resp: &rpc.Response{Success: true, Data: data},
	}
	labelClient := &recordingLabelClient{}
	publisher := &stubEventPublisher{}

	handler := uiapi.NewIssueHandler(
		detail,
		uiapi.NewMarkdownRenderer(),
		&stubUpdateClient{},
		uiapi.WithLabelClient(labelClient),
		uiapi.WithEventPublisher(publisher),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-55/labels", bytes.NewReader([]byte(`{"label":" ready "}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(labelClient.added) != 1 || labelClient.added[0].Label != "ready" {
		t.Fatalf("expected label add call for 'ready', got %#v", labelClient.added)
	}
	if detail.calls == 0 || detail.lastID != "ui-55" {
		t.Fatalf("expected detail lookup, got calls=%d id=%q", detail.calls, detail.lastID)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != uiapi.EventTypeUpdated {
		t.Fatalf("expected updated event, got %#v", publisher.events)
	}

	var payload struct {
		Labels []string           `json:"labels"`
		Issue  uiapi.IssueSummary `json:"issue"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Issue.ID != "ui-55" || len(payload.Labels) != 2 {
		t.Fatalf("unexpected response payload: %#v", payload)
	}
}

func TestNewIssueHandlerUsesLabelHandlers(t *testing.T) {
	now := time.Date(2025, 10, 30, 18, 0, 0, 0, time.UTC)
	detailPayload := struct {
		*types.Issue
		Labels []string `json:"labels,omitempty"`
	}{
		Issue: &types.Issue{
			ID:        "ui-56",
			Title:     "Label removal",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			UpdatedAt: now,
		},
		Labels: []string{"triaged"},
	}
	data, _ := json.Marshal(detailPayload)

	detail := &stubDetailResponder{
		resp: &rpc.Response{Success: true, Data: data},
	}
	adder := &recordingLabelAdder{}
	remover := &recordingLabelRemover{}
	publisher := &stubEventPublisher{}

	handler := uiapi.NewIssueHandler(
		detail,
		uiapi.NewMarkdownRenderer(),
		&stubUpdateClient{},
		uiapi.WithLabelHandlers(adder, remover),
		uiapi.WithEventPublisher(publisher),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-56/labels", bytes.NewReader([]byte(`{"label":" triaged "}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(adder.added) != 1 || adder.added[0].Label != "triaged" {
		t.Fatalf("expected add call via handlers, got %#v", adder.added)
	}
	if len(remover.removed) != 0 {
		t.Fatalf("did not expect remove calls, got %#v", remover.removed)
	}
	if detail.lastID != "ui-56" {
		t.Fatalf("expected detail lookup for ui-56, got %q", detail.lastID)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != uiapi.EventTypeUpdated {
		t.Fatalf("expected updated event, got %#v", publisher.events)
	}
}

func TestNewIssueHandlerDeleteSupport(t *testing.T) {
	handler := uiapi.NewIssueHandler(
		&stubDetailResponder{},
		uiapi.NewMarkdownRenderer(),
		&stubUpdateClient{},
	)
	req := httptest.NewRequest(http.MethodDelete, "/api/issues/ui-77?confirm=ui-77", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when delete client missing, got %d", rec.Code)
	}

	deleteClient := &stubDeleteClient{}
	publisher := &stubEventPublisher{}
	handler = uiapi.NewIssueHandler(
		&stubDetailResponder{},
		uiapi.NewMarkdownRenderer(),
		&stubUpdateClient{},
		uiapi.WithDeleteClient(deleteClient),
		uiapi.WithEventPublisher(publisher),
	)

	req = httptest.NewRequest(http.MethodDelete, "/api/issues/ui-77?confirm=ui-77", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !deleteClient.called || deleteClient.id != "ui-77" {
		t.Fatalf("expected delete invocation for ui-77, got called=%v id=%q", deleteClient.called, deleteClient.id)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != uiapi.EventTypeDeleted {
		t.Fatalf("expected deleted event, got %#v", publisher.events)
	}
}

func TestNewIssueHandlerPatchUpdatesFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 30, 18, 45, 0, 0, time.UTC)
	issue := types.Issue{
		ID:          "ui-77",
		Title:       "Patch target",
		Status:      types.StatusOpen,
		Priority:    2,
		UpdatedAt:   now,
		Description: "Original description",
		Notes:       "Original notes",
	}

	detailPayload := struct {
		*types.Issue
	}{
		Issue: &issue,
	}
	detailData, err := json.Marshal(detailPayload)
	if err != nil {
		t.Fatalf("marshal detail payload: %v", err)
	}

	updatedIssue := issue
	updatedIssue.Description = "Updated copy"
	updatedIssue.Notes = "New note"
	updatedData, err := json.Marshal(updatedIssue)
	if err != nil {
		t.Fatalf("marshal updated issue: %v", err)
	}

	stubUpdate := &stubUpdateClient{
		resp: &rpc.Response{Success: true, Data: updatedData},
	}
	publisher := &stubEventPublisher{}

	handler := uiapi.NewIssueHandler(
		&stubDetailResponder{resp: &rpc.Response{Success: true, Data: detailData}},
		uiapi.NewMarkdownRenderer(),
		stubUpdate,
		uiapi.WithEventPublisher(publisher),
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/issues/ui-77", bytes.NewBufferString(`{"description":"Updated copy","notes":"New note"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stubUpdate.args == nil {
		t.Fatalf("expected update args to be recorded")
	}
	if stubUpdate.args.ID != "ui-77" {
		t.Fatalf("expected update id ui-77, got %s", stubUpdate.args.ID)
	}
	if stubUpdate.args.Description == nil || *stubUpdate.args.Description != "Updated copy" {
		t.Fatalf("expected description update, got %#v", stubUpdate.args.Description)
	}
	if stubUpdate.args.Notes == nil || *stubUpdate.args.Notes != "New note" {
		t.Fatalf("expected notes update, got %#v", stubUpdate.args.Notes)
	}

	var payload struct {
		Issue         uiapi.IssueSummary `json:"issue"`
		UpdatedFields []string           `json:"updated_fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Issue.ID != "ui-77" {
		t.Fatalf("expected summary for ui-77, got %s", payload.Issue.ID)
	}
	if len(payload.UpdatedFields) != 2 {
		t.Fatalf("expected two updated fields, got %#v", payload.UpdatedFields)
	}

	if len(publisher.events) != 1 || publisher.events[0].Type != uiapi.EventTypeUpdated {
		t.Fatalf("expected updated event, got %#v", publisher.events)
	}
}
