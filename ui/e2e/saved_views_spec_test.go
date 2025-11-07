//go:build ui_e2e

package e2e

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type savedViewsListClient struct {
	mu     sync.Mutex
	issues []*types.Issue
}

func (c *savedViewsListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var filtered []*types.Issue
	for _, issue := range c.issues {
		if issue == nil {
			continue
		}
		if args != nil {
			if args.Status != "" && string(issue.Status) != args.Status {
				continue
			}
			if len(args.Labels) > 0 {
				match := true
				for _, label := range args.Labels {
					if !containsInsensitive(issue.Labels, label) {
						match = false
						break
					}
				}
				if !match {
					continue
				}
			}
			if args.Query != "" && !strings.Contains(strings.ToLower(issue.Title), strings.ToLower(args.Query)) {
				continue
			}
		}
		filtered = append(filtered, cloneIssue(issue))
	}

	data, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

type savedViewsDetailClient struct {
	list *savedViewsListClient
}

func (c *savedViewsDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	if args == nil || args.ID == "" {
		return &rpc.Response{Success: false, Error: "missing id"}, nil
	}
	c.list.mu.Lock()
	defer c.list.mu.Unlock()
	for _, issue := range c.list.issues {
		if issue != nil && issue.ID == args.ID {
			payload := struct {
				*types.Issue
				Labels []string `json:"labels"`
			}{
				Issue:  cloneIssue(issue),
				Labels: append([]string(nil), issue.Labels...),
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			return &rpc.Response{Success: true, Data: data}, nil
		}
	}
	return &rpc.Response{Success: false, Error: "not found"}, nil
}

func containsInsensitive(list []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	for _, item := range list {
		if strings.ToLower(item) == target {
			return true
		}
	}
	return false
}

func TestSavedViewsPersistAndApply(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 12, 0, 0, 0, time.UTC)

	listClient := &savedViewsListClient{
		issues: []*types.Issue{
			{
				ID:        "ui-900",
				Title:     "Investigate ready workflow",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  2,
				Labels:    []string{"ui", "ops"},
				UpdatedAt: now,
			},
			{
				ID:        "ui-901",
				Title:     "Blocked deployment follow-up",
				Status:    types.StatusBlocked,
				IssueType: types.TypeBug,
				Priority:  1,
				Labels:    []string{"ops"},
				UpdatedAt: now.Add(-2 * time.Hour),
			},
		},
	}
	detailClient := &savedViewsDetailClient{list: listClient}

	initialFilters := map[string]any{
		"query":     "",
		"status":    "open",
		"issueType": "",
		"priority":  "",
		"assignee":  "",
		"labelsAll": []string{},
		"labelsAny": []string{},
		"prefix":    "",
	}
	filtersJSON, err := json.Marshal(initialFilters)
	if err != nil {
		t.Fatalf("marshal initial filters: %v", err)
	}
	baseHTML, err := templates.RenderBasePage(templates.BasePageData{
		AppTitle:           "Saved Views Harness",
		InitialFiltersJSON: template.JS(filtersJSON),
		EventStreamURL:     "/events",
		StaticPrefix:       "/.assets",
	})
	if err != nil {
		t.Fatalf("render base page: %v", err)
	}

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
		Register: func(root *http.ServeMux) {
			root.Handle("/api/issues", api.NewListHandler(listClient))
			root.Handle("/api/issues/", api.NewIssueHandler(detailClient, renderer, nil))
			root.Handle("/fragments/issues", api.NewListFragmentHandler(
				listClient,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
			root.Handle("/fragments/issue", api.NewDetailFragmentHandler(detailClient, renderer, detailTemplate))
			root.Handle("/events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		t.Fatalf("GET / status = %d", status)
	}

	page := h.Page()

	if _, err := page.WaitForSelector("[data-issue-id='ui-900']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for ready issue: %v", err)
	}

	if err := page.Fill("[data-testid='search-labels-all-input']", "ops"); err != nil {
		t.Fatalf("fill labels input: %v", err)
	}
	if err := page.Fill("[data-testid='search-query-input']", "blocked"); err != nil {
		t.Fatalf("fill query input: %v", err)
	}
	if _, err := page.SelectOption("[data-testid='search-status-select']", playwright.SelectOptionValues{
		Values: &[]string{"blocked"},
	}); err != nil {
		t.Fatalf("select blocked status: %v", err)
	}
	if err := page.Click("[data-testid='search-apply']"); err != nil {
		t.Fatalf("apply filters: %v", err)
	}

	if _, err := page.WaitForSelector("[data-issue-id='ui-901']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for blocked issue: %v", err)
	}

	if _, err := page.Evaluate(`() => {
		window.__savedViewsPromptOriginal = window.prompt;
		window.prompt = () => "Blocked Ops";
	}`); err != nil {
		t.Fatalf("override prompt: %v", err)
	}
	t.Cleanup(func() {
		_, _ = page.Evaluate(`() => {
			if (Object.prototype.hasOwnProperty.call(window, "__savedViewsPromptOriginal")) {
				window.prompt = window.__savedViewsPromptOriginal;
				delete window.__savedViewsPromptOriginal;
			}
		}`)
	})
	if err := page.Click("[data-testid='saved-views-save']"); err != nil {
		t.Fatalf("save view: %v", err)
	}

	if _, err := page.WaitForSelector("[data-testid='saved-view-item'][data-view-name='Blocked Ops']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for saved view item: %v", err)
	}

	if err := page.Click("[data-testid='search-reset']"); err != nil {
		t.Fatalf("reset filters: %v", err)
	}
	if _, err := page.WaitForSelector("[data-issue-id='ui-900']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for ready issue after reset: %v", err)
	}

	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload page: %v", err)
	}

	if _, err := page.WaitForSelector("[data-testid='saved-view-item'][data-view-name='Blocked Ops']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("wait for persisted saved view: %v", err)
	}

	if _, err := page.WaitForSelector("[data-issue-id='ui-900']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("wait for ready issue after reload: %v", err)
	}

	response, err := page.ExpectResponse("**/fragments/issues**", func() error {
		return page.Click("[data-testid='saved-view-item'][data-view-name='Blocked Ops']")
	})
	if err != nil {
		t.Fatalf("apply saved view: %v", err)
	}
	if status := response.Status(); status != http.StatusOK {
		body, _ := response.Body()
		t.Fatalf("expected 200 from fragment fetch, got %d body=%s", status, string(body))
	}

	if _, err := page.WaitForSelector("[data-issue-id='ui-901']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(8000),
	}); err != nil {
		t.Fatalf("wait for blocked issue after apply: %v", err)
	}

	filters, err := page.Evaluate(`() => {
	  if (window.bdShellState && typeof window.bdShellState.getFilters === 'function') {
	    return window.bdShellState.getFilters();
	  }
	  return {};
	}`, nil)
	if err != nil {
		t.Fatalf("read filters: %v", err)
	}
	filterMap, ok := filters.(map[string]any)
	if !ok {
		t.Fatalf("expected filters map, got %T", filters)
	}
	if status, _ := filterMap["status"].(string); status != "blocked" {
		t.Fatalf("expected status blocked after apply, got %v", status)
	}
	if query, _ := filterMap["query"].(string); query != "blocked" {
		t.Fatalf("expected query filter restored, got %v", query)
	}
	labelsAny, ok := filterMap["labelsAll"].([]any)
	if !ok {
		if raw, ok := filterMap["labelsAll"].([]interface{}); ok {
			labelsAny = raw
		}
	}
	if len(labelsAny) == 0 || strings.ToLower(labelsAny[0].(string)) != "ops" {
		t.Fatalf("expected labels restored, got %v", labelsAny)
	}

	storagePayload, err := page.Evaluate("() => window.localStorage.getItem('beads_ui_views_v1')", nil)
	if err != nil {
		t.Fatalf("read saved views payload: %v", err)
	}
	if payloadStr, _ := storagePayload.(string); !strings.Contains(payloadStr, "Blocked Ops") {
		t.Fatalf("expected localStorage to contain saved view, payload=%v", payloadStr)
	}
}
