package api

import (
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

type stubBulkClient struct {
	args  *rpc.BatchArgs
	resp  *rpc.Response
	err   error
	calls int
}

func (c *stubBulkClient) Batch(args *rpc.BatchArgs) (*rpc.Response, error) {
	c.calls++
	c.args = args
	if c.err != nil {
		return nil, c.err
	}
	if c.resp != nil {
		return c.resp, nil
	}
	return &rpc.Response{Success: true, Data: []byte(`{"results":[]}`)}, nil
}

type recordingPublisher struct {
	events []IssueEvent
}

func (p *recordingPublisher) Publish(evt IssueEvent) {
	p.events = append(p.events, evt)
}

func TestBulkHandlerUpdatesIssues(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 18, 30, 0, 0, time.UTC)

	issueOne := &types.Issue{
		ID:        "ui-101",
		Title:     "One",
		Status:    types.StatusInProgress,
		IssueType: types.TypeFeature,
		Priority:  3,
		UpdatedAt: now,
	}
	issueTwo := &types.Issue{
		ID:        "ui-102",
		Title:     "Two",
		Status:    types.StatusInProgress,
		IssueType: types.TypeTask,
		Priority:  3,
		UpdatedAt: now.Add(2 * time.Minute),
	}

	batchResp := rpc.BatchResponse{
		Results: []rpc.BatchResult{
			{Success: true, Data: mustJSON(issueOne)},
			{Success: true, Data: mustJSON(issueTwo)},
		},
	}

	client := &stubBulkClient{
		resp: &rpc.Response{
			Success: true,
			Data:    mustJSON(batchResp),
		},
	}
	publisher := &recordingPublisher{}

	handler := NewBulkHandler(client, publisher)

	body := `{"ids":["ui-101","ui-102"],"action":{"status":"in_progress","priority":3}}`
	req := httptest.NewRequest(http.MethodPost, "/api/issues/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Results []struct {
			Success bool         `json:"success"`
			Error   string       `json:"error"`
			Issue   IssueSummary `json:"issue"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(payload.Results) != 2 {
		t.Fatalf("expected 2 results, got %d (%+v)", len(payload.Results), payload)
	}

	for i, result := range payload.Results {
		if !result.Success {
			t.Fatalf("result %d marked unsuccessful: %+v", i, result)
		}
		if result.Error != "" {
			t.Fatalf("result %d error not empty: %q", i, result.Error)
		}
	}

	if payload.Results[0].Issue.ID != "ui-101" || payload.Results[0].Issue.Status != "in_progress" {
		t.Fatalf("unexpected first issue summary: %+v", payload.Results[0].Issue)
	}
	if payload.Results[1].Issue.ID != "ui-102" || payload.Results[1].Issue.Priority != 3 {
		t.Fatalf("unexpected second issue summary: %+v", payload.Results[1].Issue)
	}

	if client.calls != 1 {
		t.Fatalf("expected single batch call, got %d", client.calls)
	}
	if client.args == nil || len(client.args.Operations) != 2 {
		t.Fatalf("expected 2 batch operations, got %+v", client.args)
	}

	var update rpc.UpdateArgs
	if err := json.Unmarshal(client.args.Operations[0].Args, &update); err != nil {
		t.Fatalf("decode first operation: %v", err)
	}
	if update.ID != "ui-101" {
		t.Fatalf("first operation id mismatch: %s", update.ID)
	}
	if update.Status == nil || strings.ToLower(strings.TrimSpace(*update.Status)) != "in_progress" {
		t.Fatalf("first operation status unexpected: %#v", update.Status)
	}
	if update.Priority == nil || *update.Priority != 3 {
		t.Fatalf("first operation priority unexpected: %#v", update.Priority)
	}

	if len(publisher.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(publisher.events))
	}
	if publisher.events[0].Issue.ID != "ui-101" || publisher.events[0].Type != EventTypeUpdated {
		t.Fatalf("unexpected first event: %+v", publisher.events[0])
	}
	if publisher.events[1].Issue.ID != "ui-102" {
		t.Fatalf("unexpected second event: %+v", publisher.events[1])
	}
}

func TestBulkHandlerValidatesInput(t *testing.T) {
	t.Parallel()

	handler := NewBulkHandler(&stubBulkClient{}, &recordingPublisher{})

	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing ids",
			body: `{"ids":[],"action":{"status":"open"}}`,
		},
		{
			name: "missing action",
			body: `{"ids":["ui-1"]}`,
		},
		{
			name: "invalid priority",
			body: `{"ids":["ui-1"],"action":{"priority":9}}`,
		},
		{
			name: "empty status",
			body: `{"ids":["ui-1"],"action":{"status":"   "}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/issues/bulk", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBulkHandlerPropagatesBatchFailure(t *testing.T) {
	t.Parallel()

	client := &stubBulkClient{
		resp: &rpc.Response{
			Success: true,
			Data:    mustJSON(rpc.BatchResponse{Results: []rpc.BatchResult{{Success: false, Error: "boom"}}}),
		},
	}
	handler := NewBulkHandler(client, &recordingPublisher{})

	req := httptest.NewRequest(http.MethodPost, "/api/issues/bulk", strings.NewReader(`{"ids":["ui-1"],"action":{"status":"blocked"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload struct {
		Results []struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(payload.Results))
	}
	if payload.Results[0].Success {
		t.Fatalf("expected result flagged as failure: %+v", payload.Results[0])
	}
	if payload.Results[0].Error == "" {
		t.Fatalf("expected error message for failed result")
	}
}

func TestBulkHandlerRejectsNonPost(t *testing.T) {
	handler := NewBulkHandler(&stubBulkClient{}, &recordingPublisher{})
	req := httptest.NewRequest(http.MethodGet, "/api/issues/bulk", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestBulkHandlerUnavailable(t *testing.T) {
	handler := NewBulkHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/bulk", strings.NewReader(`{"ids":["ui-1"],"action":{"status":"open"}}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestBulkHandlerClientErrorWithResponse(t *testing.T) {
	client := &stubBulkClient{
		resp: &rpc.Response{
			Success:    false,
			Error:      "bulk rejected",
			StatusCode: http.StatusConflict,
		},
	}
	handler := NewBulkHandler(client, &recordingPublisher{})

	req := httptest.NewRequest(http.MethodPost, "/api/issues/bulk", strings.NewReader(`{"ids":["ui-1"],"action":{"status":"open"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bulk rejected") {
		t.Fatalf("expected error body, got %s", rec.Body.String())
	}
}

func TestBulkHandlerClientTransportError(t *testing.T) {
	client := &stubBulkClient{
		err: errors.New("dial failed"),
	}
	handler := NewBulkHandler(client, &recordingPublisher{})

	req := httptest.NewRequest(http.MethodPost, "/api/issues/bulk", strings.NewReader(`{"ids":["ui-1"],"action":{"status":"open"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bulk update failed: dial failed") {
		t.Fatalf("expected transport error message, got %s", rec.Body.String())
	}
}

func TestNormalizeIDs(t *testing.T) {
	input := []string{" ui-1 ", "", "ui-2", "   "}
	got := normalizeIDs(input)
	if len(got) != 2 || got[0] != "ui-1" || got[1] != "ui-2" {
		t.Fatalf("normalizeIDs returned %#v", got)
	}
	if res := normalizeIDs(nil); res != nil {
		t.Fatalf("expected nil when input nil, got %#v", res)
	}
}

func TestNormalizeActionValidation(t *testing.T) {
	action, err := normalizeAction(bulkAction{Status: " Ready ", Priority: intPtr(2)})
	if err != nil {
		t.Fatalf("normalizeAction returned error: %v", err)
	}
	if action.Status != "ready" || action.Priority == nil || *action.Priority != 2 {
		t.Fatalf("normalized action mismatch: %+v", action)
	}

	if _, err := normalizeAction(bulkAction{}); err == nil {
		t.Fatalf("expected error when both status and priority absent")
	}
	if _, err := normalizeAction(bulkAction{Priority: intPtr(9)}); err == nil {
		t.Fatalf("expected error for out-of-range priority")
	}
}

func TestBuildBatchOperations(t *testing.T) {
	ops, err := buildBatchOperations([]string{"ui-1", "ui-2"}, normalizedAction{Status: "ready", Priority: intPtr(3)})
	if err != nil {
		t.Fatalf("buildBatchOperations error: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected two operations, got %d", len(ops))
	}
	var update rpc.UpdateArgs
	if err := json.Unmarshal(ops[0].Args, &update); err != nil {
		t.Fatalf("decode operation: %v", err)
	}
	if update.ID != "ui-1" || update.Status == nil || *update.Status != "ready" || update.Priority == nil || *update.Priority != 3 {
		t.Fatalf("unexpected operation payload: %+v", update)
	}

	if _, err := buildBatchOperations([]string{""}, normalizedAction{Status: "open"}); err == nil {
		t.Fatalf("expected error when ids empty after normalization")
	}
}

func TestDecodeIssueFromResult(t *testing.T) {
	issue := &types.Issue{ID: "ui-99", Title: "Decoded"}
	raw, _ := json.Marshal(issue)
	got, err := decodeIssueFromResult(raw)
	if err != nil {
		t.Fatalf("decodeIssueFromResult direct err: %v", err)
	}
	if got.ID != "ui-99" {
		t.Fatalf("expected ui-99, got %+v", got)
	}

	wrapper, _ := json.Marshal(map[string]any{"issue": issue})
	got, err = decodeIssueFromResult(wrapper)
	if err != nil {
		t.Fatalf("decodeIssueFromResult wrapper err: %v", err)
	}
	if got.Title != "Decoded" {
		t.Fatalf("unexpected issue %+v", got)
	}

	if _, err := decodeIssueFromResult([]byte(`{"unexpected":true}`)); err == nil {
		t.Fatalf("expected error for unexpected payload")
	}
	if _, err := decodeIssueFromResult(nil); err == nil {
		t.Fatalf("expected error for empty input")
	}
}

func intPtr(v int) *int {
	return &v
}
