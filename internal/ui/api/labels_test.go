package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

type recordingDetailClient struct {
	response *rpc.Response
	err      error
	calls    []string
}

func (c *recordingDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	c.calls = append(c.calls, args.ID)
	return c.response, c.err
}

type labelPublisher struct {
	events []IssueEvent
}

func (p *labelPublisher) Publish(evt IssueEvent) {
	p.events = append(p.events, evt)
}

type recordingLabelClient struct {
	added   []*rpc.LabelAddArgs
	removed []*rpc.LabelRemoveArgs
}

func (c *recordingLabelClient) AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error) {
	c.added = append(c.added, args)
	return &rpc.Response{Success: true}, nil
}

func (c *recordingLabelClient) RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error) {
	c.removed = append(c.removed, args)
	return &rpc.Response{Success: true}, nil
}

func TestHandleLabelMutationValidatesPayload(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader("{invalid"))
	rec := httptest.NewRecorder()

	handleLabelMutation(rec, req, "ui-1", nil, nil, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader(`{"label":"   "}`))
	rec = httptest.NewRecorder()
	handleLabelMutation(rec, req, "ui-1", nil, nil, func(string) (*rpc.Response, error) {
		return nil, nil
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleLabelMutationInvokeError(t *testing.T) {
	t.Parallel()

	t.Run("transport error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader(`{"label":"frontend"}`))
		rec := httptest.NewRecorder()
		handleLabelMutation(rec, req, "ui-1", nil, nil, func(string) (*rpc.Response, error) {
			return nil, errors.New("boom")
		})
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "label mutation failed") {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	})

	t.Run("application error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader(`{"label":"frontend"}`))
		rec := httptest.NewRecorder()
		handleLabelMutation(rec, req, "ui-1", nil, nil, func(string) (*rpc.Response, error) {
			return &rpc.Response{Success: false, Error: "label already exists"}, nil
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", rec.Code)
		}
	})
}

func TestHandleLabelMutationHandlesEmptyResponse(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader(`{"label":"frontend"}`))
	rec := httptest.NewRecorder()

	handleLabelMutation(rec, req, "ui-1", nil, nil, func(string) (*rpc.Response, error) {
		return nil, nil
	})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestHandleLabelMutationSuccessWithoutDetail(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader(`{"label":"frontend"}`))
	rec := httptest.NewRecorder()

	handleLabelMutation(rec, req, "ui-1", nil, nil, func(string) (*rpc.Response, error) {
		return &rpc.Response{Success: true}, nil
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestLabelHandlerReturnsNoContentWithoutDetailClient(t *testing.T) {
	t.Parallel()

	labelClient := &recordingLabelClient{}
	handler := NewLabelHandler(nil, labelClient, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-42/labels", strings.NewReader(`{"label":" docs "}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if len(labelClient.added) != 1 || labelClient.added[0].Label != "docs" {
		t.Fatalf("expected label add invocation, got %#v", labelClient.added)
	}
}

func TestHandleLabelMutationSuccessWithDetailAndPublisher(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	payload := showPayload{
		Issue: &types.Issue{
			ID:        "ui-1",
			Title:     "Updated issue",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  2,
			UpdatedAt: now,
		},
		Labels: []string{"frontend"},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	detail := &recordingDetailClient{
		response: &rpc.Response{Success: true, Data: data},
	}
	publisher := &labelPublisher{}

	req := httptest.NewRequest(http.MethodPost, "/labels", bytes.NewReader([]byte(`{"label":"frontend"}`)))
	rec := httptest.NewRecorder()

	handleLabelMutation(rec, req, "ui-1", detail, publisher, func(string) (*rpc.Response, error) {
		return &rpc.Response{Success: true}, nil
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(detail.calls) != 1 || detail.calls[0] != "ui-1" {
		t.Fatalf("expected detail client called with ui-1, got %+v", detail.calls)
	}
	if len(publisher.events) != 1 || publisher.events[0].Issue.ID != "ui-1" {
		t.Fatalf("expected publisher event, got %+v", publisher.events)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `"labels":["frontend"]`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestMapLabelError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err      string
		expected int
	}{
		{"Issue not found", http.StatusNotFound},
		{"label already exists", http.StatusConflict},
		{"duplicate label", http.StatusConflict},
		{"invalid label format", http.StatusBadRequest},
		{"some other failure", http.StatusBadGateway},
	}

	for _, tc := range cases {
		if got := mapLabelError(tc.err); got != tc.expected {
			t.Fatalf("mapLabelError(%q) = %d, want %d", tc.err, got, tc.expected)
		}
	}
}

func TestParseLabelPath(t *testing.T) {
	t.Parallel()

	if got, ok := parseLabelPath("/api/issues/ui-1/labels"); !ok || got != "ui-1" {
		t.Fatalf("expected valid path, got (%q,%v)", got, ok)
	}
	if _, ok := parseLabelPath("/api/issues/ui-1/comments"); ok {
		t.Fatalf("expected comments path to be invalid")
	}
	if _, ok := parseLabelPath("/other/ui-1/labels"); ok {
		t.Fatalf("expected non-issue path to be invalid")
	}
}

func TestWithLabelEventPublisher(t *testing.T) {
	t.Parallel()

	var opts labelHandlerOptions
	WithLabelEventPublisher(nil)(&opts)
	if opts.publisher != nil {
		t.Fatalf("nil publisher should not set option")
	}

	var pub labelPublisher
	WithLabelEventPublisher(&pub)(&opts)
	if opts.publisher == nil {
		t.Fatalf("expected publisher to be set")
	}
}

func TestNewLabelHandlerHandlesMissingClients(t *testing.T) {
	t.Parallel()

	handler := NewLabelHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-1/labels", strings.NewReader(`{"label":"foo"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/issues/ui-1/labels", strings.NewReader(`{"label":"foo"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/issues/ui-1/labels", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
