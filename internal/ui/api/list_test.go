package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	uiapi "github.com/steveyegge/beads/internal/ui/api"
)

type stubListClient struct {
	t        testing.TB
	called   int
	lastArgs *rpc.ListArgs
	resp     *rpc.Response
	err      error
}

func (s *stubListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	s.called++
	s.lastArgs = args
	return s.resp, s.err
}

func TestListHandlerAppliesFilters(t *testing.T) {
	now := time.Date(2025, 10, 22, 18, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{
			ID:        "ui-issue-1",
			Title:     "Seed Issue",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  1,
			Assignee:  "alice",
			UpdatedAt: now,
			Labels:    []string{"bug", "critical"},
		},
	}
	data, err := json.Marshal(issues)
	if err != nil {
		t.Fatalf("marshal issues: %v", err)
	}

	client := &stubListClient{
		resp: &rpc.Response{
			Success: true,
			Data:    data,
		},
		t: t,
	}

	handler := uiapi.NewListHandler(client)
	req := httptest.NewRequest(http.MethodGet, "/api/issues?queue=ready&priority=1&labels=bug,critical&labels_any=recent&q=search&limit=5&assignee=alice&sort=updated-desc&sort_secondary=title-asc&id_prefix=ui-", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if client.called != 1 {
		t.Fatalf("expected client to be called once, got %d", client.called)
	}

	args := client.lastArgs
	if args == nil {
		t.Fatal("expected args to be recorded")
	}
	if args.Status != string(types.StatusOpen) {
		t.Fatalf("expected status %s, got %s", types.StatusOpen, args.Status)
	}
	if args.Priority == nil || *args.Priority != 1 {
		t.Fatalf("expected priority 1, got %v", args.Priority)
	}
	if args.Assignee != "alice" {
		t.Fatalf("expected assignee alice, got %s", args.Assignee)
	}
	if args.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", args.Limit)
	}
	if args.Query != "search" {
		t.Fatalf("expected query 'search', got %q", args.Query)
	}
	if len(args.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %v", args.Labels)
	}
	if len(args.LabelsAny) != 1 || args.LabelsAny[0] != "recent" {
		t.Fatalf("unexpected labelsAny: %v", args.LabelsAny)
	}
	if args.Order != "updated-desc,title-asc" {
		t.Fatalf("expected combined order, got %q", args.Order)
	}
	if args.IDPrefix != "ui-" {
		t.Fatalf("expected id prefix ui-, got %q", args.IDPrefix)
	}

	var payload struct {
		Issues []uiapi.IssueSummary `json:"issues"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(payload.Issues))
	}
	if payload.Issues[0].UpdatedAt != now.Format(time.RFC3339) {
		t.Fatalf("unexpected updated_at: %s", payload.Issues[0].UpdatedAt)
	}
}

func TestListHandlerRecentQueueSetsDefaultLimit(t *testing.T) {
	client := &stubListClient{
		resp: &rpc.Response{
			Success: true,
			Data:    []byte("[]"),
		},
		t: t,
	}

	handler := uiapi.NewListHandler(client)
	req := httptest.NewRequest(http.MethodGet, "/api/issues?queue=recent", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if client.lastArgs == nil {
		t.Fatal("expected args to be recorded")
	}
	if client.lastArgs.Limit != 20 {
		t.Fatalf("expected default limit 20, got %d", client.lastArgs.Limit)
	}
}

func TestListHandlerClosedQueueSortsDescending(t *testing.T) {
	now := time.Date(2025, 10, 30, 12, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{ID: "ui-02", UpdatedAt: now.Add(-5 * time.Minute), ClosedAt: timePtr(now.Add(-5 * time.Minute))},
		{ID: "ui-11", UpdatedAt: now.Add(-1 * time.Minute), ClosedAt: timePtr(now.Add(-1 * time.Minute))},
		{ID: "ui-03", UpdatedAt: now.Add(-3 * time.Minute), ClosedAt: timePtr(now.Add(-3 * time.Minute))},
	}
	data, err := json.Marshal(issues)
	if err != nil {
		t.Fatalf("marshal issues: %v", err)
	}

	client := &stubListClient{
		resp: &rpc.Response{
			Success: true,
			Data:    data,
		},
		t: t,
	}

	handler := uiapi.NewListHandler(client)
	req := httptest.NewRequest(http.MethodGet, "/api/issues?queue=closed&limit=2", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	var payload struct {
		Issues     []uiapi.IssueSummary `json:"issues"`
		HasMore    bool                 `json:"has_more"`
		NextCursor string               `json:"next_cursor"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(payload.Issues))
	}
	expected := []string{"ui-11", "ui-03"}
	for i, want := range expected {
		if payload.Issues[i].ID != want {
			t.Fatalf("expected issue %q at index %d, got %q", want, i, payload.Issues[i].ID)
		}
	}
	if !payload.HasMore {
		t.Fatalf("expected has_more to be true")
	}
	if payload.NextCursor == "" {
		t.Fatalf("expected next_cursor to be populated")
	}

	if client.lastArgs == nil {
		t.Fatal("expected args to be recorded")
	}
	if want := 3; client.lastArgs.Limit != want {
		t.Fatalf("expected closed queue to request limit %d, got %d", want, client.lastArgs.Limit)
	}
	if client.lastArgs.Cursor != "" {
		t.Fatalf("expected initial cursor to be empty, got %q", client.lastArgs.Cursor)
	}
}

func TestListHandlerClosedQueueRespectsCursor(t *testing.T) {
	now := time.Date(2025, 10, 30, 15, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{
			ID:        "ui-50",
			Title:     "Older closed",
			Status:    types.StatusClosed,
			ClosedAt:  timePtr(now.Add(-30 * time.Minute)),
			UpdatedAt: now.Add(-30 * time.Minute),
		},
	}
	data, err := json.Marshal(issues)
	if err != nil {
		t.Fatalf("marshal issues: %v", err)
	}

	client := &stubListClient{
		resp: &rpc.Response{Success: true, Data: data},
		t:    t,
	}

	handler := uiapi.NewListHandler(client)
	cursor := now.Add(-10*time.Minute).UTC().Format(time.RFC3339Nano) + "|ui-75"
	req := httptest.NewRequest(http.MethodGet, "/api/issues?queue=closed&cursor="+cursor+"&limit=1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	if client.lastArgs == nil {
		t.Fatalf("expected args to be captured")
	}
	if client.lastArgs.Cursor != cursor {
		t.Fatalf("expected cursor %q to propagate, got %q", cursor, client.lastArgs.Cursor)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
