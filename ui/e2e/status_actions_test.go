//go:build ui_e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type actionsState struct {
	mu    sync.RWMutex
	issue *types.Issue
}

func newActionsState(issue *types.Issue) *actionsState {
	return &actionsState{issue: issue}
}

func (s *actionsState) snapshot() *types.Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.issue == nil {
		return nil
	}
	copy := *s.issue
	copy.Labels = append([]string(nil), s.issue.Labels...)
	return &copy
}

func (s *actionsState) updateStatus(status types.Status) *types.Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.issue == nil {
		return nil
	}
	s.issue.Status = status
	s.issue.UpdatedAt = time.Now().UTC()
	copy := *s.issue
	copy.Labels = append([]string(nil), s.issue.Labels...)
	return &copy
}

type actionsListClient struct {
	state *actionsState
}

func (c *actionsListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	issue := c.state.snapshot()
	if issue == nil {
		issue = &types.Issue{}
	}
	data, err := json.Marshal([]*types.Issue{issue})
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

type actionsDetailClient struct {
	state *actionsState
}

func (c *actionsDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	issue := c.state.snapshot()
	if issue == nil {
		issue = &types.Issue{}
	}
	issue.ID = args.ID
	issue.Title = "Status Action " + args.ID

	payload := struct {
		*types.Issue
		Labels            []string            `json:"labels"`
		DependencyRecords []*types.Dependency `json:"dependency_records"`
		Dependencies      []*types.Issue      `json:"dependencies"`
	}{
		Issue: issue,
		Labels: []string{
			"ui",
			"status",
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

type actionsUpdateClient struct {
	state *actionsState

	mu    sync.Mutex
	calls []*rpc.UpdateArgs
}

func (c *actionsUpdateClient) Update(args *rpc.UpdateArgs) (*rpc.Response, error) {
	c.mu.Lock()
	c.calls = append(c.calls, args)
	c.mu.Unlock()

	status := types.StatusOpen
	if args != nil && args.Status != nil {
		status = types.Status(*args.Status)
	}

	issue := c.state.updateStatus(status)
	if issue == nil {
		issue = &types.Issue{
			ID:     args.ID,
			Status: status,
		}
	}
	data, _ := json.Marshal(issue)
	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

func (c *actionsUpdateClient) statuses() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.calls))
	for _, call := range c.calls {
		if call != nil && call.Status != nil {
			out = append(out, *call.Status)
		}
	}
	return out
}

func TestStatusActionsButtons(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 7, 0, 0, 0, time.UTC)

	initialIssue := &types.Issue{
		ID:        "ui-601",
		Title:     "Status button wiring",
		Status:    types.StatusOpen,
		IssueType: types.TypeFeature,
		Priority:  1,
		Labels:    []string{"ui", "status"},
		UpdatedAt: now.Add(-2 * time.Minute),
	}
	state := newActionsState(initialIssue)

	listClient := &actionsListClient{state: state}
	detailClient := &actionsDetailClient{state: state}
	updateClient := &actionsUpdateClient{state: state}

	baseHTML := renderBasePage(t, "Status Actions Harness")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}
	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse detail template: %v", err)
	}

	renderer := api.NewMarkdownRenderer()

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewIssueHandler(detailClient, renderer, updateClient))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				listClient,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
			mux.Handle("/fragments/issue", api.NewDetailFragmentHandler(detailClient, renderer, detailTemplate))
			mux.Handle("/events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	h := NewRemoteHarness(t, server.URL, nil, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	if _, err := h.Page().WaitForSelector("[data-role='issue-row']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for issue rows: %v", err)
	}

	if err := h.Page().Click("[data-issue-id='ui-601']"); err != nil {
		t.Fatalf("select issue row: %v", err)
	}

	if _, err := h.Page().WaitForSelector("[data-testid='status-action-in-progress']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for status action buttons: %v", err)
	}

	if err := h.Page().Click("[data-testid='status-action-in-progress']"); err != nil {
		t.Fatalf("click in-progress button: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
		const el = document.querySelector("[data-testid='status-action-message']");
		if (!el) { return false; }
		return (el.textContent || "").toLowerCase().includes("in progress");
	}`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for in-progress message: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
		const badge = document.querySelector("[data-testid='issue-detail'] [data-field='status']");
		if (!badge) { return false; }
		return (badge.textContent || "").trim().toLowerCase() === "in progress";
	}`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for status badge in progress: %v", err)
	}

	if err := h.Page().Keyboard().Press("Shift+D"); err != nil {
		t.Fatalf("press Shift+D shortcut: %v", err)
	}

	statuses := updateClient.statuses()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 status updates, got %d", len(statuses))
	}
	if statuses[0] != string(types.StatusInProgress) {
		t.Fatalf("expected first status in_progress, got %s", statuses[0])
	}
	if statuses[1] != string(types.StatusClosed) {
		t.Fatalf("expected second status closed, got %s", statuses[1])
	}
}

func TestStatusActionsShortcutIgnoredWhileEditing(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 7, 0, 0, 0, time.UTC)

	initialIssue := &types.Issue{
		ID:        "ui-601",
		Title:     "Status button wiring",
		Status:    types.StatusOpen,
		IssueType: types.TypeFeature,
		Priority:  1,
		Labels:    []string{"ui", "status"},
		UpdatedAt: now.Add(-2 * time.Minute),
	}
	state := newActionsState(initialIssue)

	listClient := &actionsListClient{state: state}
	detailClient := &actionsDetailClient{state: state}
	updateClient := &actionsUpdateClient{state: state}

	baseHTML := renderBasePage(t, "Status Actions Harness")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}
	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse detail template: %v", err)
	}

	renderer := api.NewMarkdownRenderer()

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewIssueHandler(detailClient, renderer, updateClient))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				listClient,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
			mux.Handle("/fragments/issue", api.NewDetailFragmentHandler(detailClient, renderer, detailTemplate))
			mux.Handle("/events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	h := NewRemoteHarness(t, server.URL, nil, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	if _, err := h.Page().WaitForSelector("[data-role='issue-row']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for issue rows: %v", err)
	}

	if err := h.Page().Click("[data-issue-id='ui-601']"); err != nil {
		t.Fatalf("select issue row: %v", err)
	}

	if _, err := h.Page().WaitForSelector("[data-testid='label-input']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for label input: %v", err)
	}

	if err := h.Page().Click("[data-testid='label-input']"); err != nil {
		t.Fatalf("focus label input: %v", err)
	}

	if err := h.Page().Keyboard().Type("Test"); err != nil {
		t.Fatalf("type into label input: %v", err)
	}

	if err := h.Page().Keyboard().Press("Shift+I"); err != nil {
		t.Fatalf("press Shift+I while editing: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	value, err := h.Page().InputValue("[data-testid='label-input']")
	if err != nil {
		t.Fatalf("read label input value: %v", err)
	}
	if !strings.HasSuffix(value, "I") {
		t.Fatalf("expected label input to receive Shift+I, got %q", value)
	}

	if statuses := updateClient.statuses(); len(statuses) != 0 {
		t.Fatalf("expected no status updates, got %d", len(statuses))
	}

	badgeText, err := h.Page().InnerText("[data-testid='issue-detail'] [data-field='status']")
	if err != nil {
		t.Fatalf("read status badge: %v", err)
	}
	normalized := strings.TrimSpace(strings.ToLower(badgeText))
	if normalized != "ready" && normalized != "open" {
		t.Fatalf("expected status badge to remain Ready/Open, got %q", badgeText)
	}
}
