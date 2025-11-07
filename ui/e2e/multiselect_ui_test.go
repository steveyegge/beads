//go:build ui_e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type multiListClient struct {
	mu     sync.Mutex
	issues []*types.Issue
}

func (c *multiListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cloned := make([]*types.Issue, len(c.issues))
	for i, issue := range c.issues {
		cloned[i] = cloneIssue(issue)
	}

	data, err := json.Marshal(cloned)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func (c *multiListClient) get(id string) *types.Issue {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, issue := range c.issues {
		if issue != nil && issue.ID == id {
			return cloneIssue(issue)
		}
	}
	return nil
}

func (c *multiListClient) update(issue *types.Issue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for idx, existing := range c.issues {
		if existing != nil && existing.ID == issue.ID {
			c.issues[idx] = cloneIssue(issue)
			return
		}
	}
}

type multiDetailClient struct {
	list *multiListClient
}

func (c *multiDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	if args == nil || args.ID == "" {
		return &rpc.Response{Success: false, Error: "missing id"}, nil
	}
	issue := c.list.get(args.ID)
	if issue == nil {
		return &rpc.Response{Success: false, Error: "not found"}, nil
	}
	payload := struct {
		*types.Issue
		Labels []string `json:"labels"`
	}{
		Issue:  issue,
		Labels: []string{"ui"},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

type multiBulkClient struct {
	list  *multiListClient
	mu    sync.Mutex
	calls int
	last  *rpc.BatchArgs
}

func (c *multiBulkClient) Batch(args *rpc.BatchArgs) (*rpc.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls++
	c.last = args

	results := make([]rpc.BatchResult, 0, len(args.Operations))
	for _, op := range args.Operations {
		if op.Operation != rpc.OpUpdate {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   fmt.Sprintf("unexpected op %s", op.Operation),
			})
			continue
		}
		var update rpc.UpdateArgs
		if err := json.Unmarshal(op.Args, &update); err != nil {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   fmt.Sprintf("decode update args: %v", err),
			})
			continue
		}
		issue := c.list.get(update.ID)
		if issue == nil {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   "not found",
			})
			continue
		}

		if update.Status != nil {
			issue.Status = types.Status(*update.Status)
		}
		if update.Priority != nil {
			issue.Priority = *update.Priority
		}
		issue.UpdatedAt = issue.UpdatedAt.Add(2 * time.Minute)
		c.list.update(issue)

		data, err := json.Marshal(issue)
		if err != nil {
			return nil, err
		}
		results = append(results, rpc.BatchResult{
			Success: true,
			Data:    data,
		})
	}

	payload, err := json.Marshal(rpc.BatchResponse{Results: results})
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: payload}, nil
}

func TestMultiSelectBulkUpdates(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 24, 15, 0, 0, 0, time.UTC)
	listClient := &multiListClient{
		issues: []*types.Issue{
			{
				ID:        "ui-410",
				Title:     "Investigate SSE reconnect",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  2,
				UpdatedAt: now,
			},
			{
				ID:        "ui-411",
				Title:     "Add queue filters to toolbar",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  3,
				UpdatedAt: now.Add(30 * time.Second),
			},
			{
				ID:        "ui-412",
				Title:     "Document saved views behaviour",
				Status:    types.StatusOpen,
				IssueType: types.TypeChore,
				Priority:  2,
				UpdatedAt: now.Add(90 * time.Second),
			},
		},
	}
	detailClient := &multiDetailClient{list: listClient}
	bulkClient := &multiBulkClient{list: listClient}

	baseHTML := renderBasePage(t, "Multi-Select Harness")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}
	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse detail template: %v", err)
	}

	renderer := api.NewMarkdownRenderer()

	listHandler := api.NewListHandler(listClient)
	detailHandler := api.NewIssueHandler(detailClient, renderer, nil)
	bulkHandler := api.NewBulkHandler(bulkClient, nil)

	mux := http.NewServeMux()
	mux.Handle("/api/issues", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listHandler.ServeHTTP(w, r)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/issues/", detailHandler)
	mux.Handle("/api/issues/bulk", bulkHandler)
	mux.Handle("/fragments/issues", api.NewListFragmentHandler(
		listClient,
		api.WithListFragmentTemplate(listTemplate),
	))
	mux.Handle("/fragments/issue", api.NewDetailFragmentHandler(
		detailClient,
		renderer,
		detailTemplate,
	))
	mux.Handle("/.assets/", http.StripPrefix("/.assets/", http.FileServer(http.FS(static.Files))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(baseHTML); err != nil {
			t.Logf("write base page: %v", err)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("start playwright: %v", err)
	}
	t.Cleanup(func() {
		_ = pw.Stop()
	})

	browser, err := pw.Chromium.Launch()
	if err != nil {
		t.Fatalf("launch browser: %v", err)
	}
	t.Cleanup(func() {
		_ = browser.Close()
	})

	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}

	if _, err := page.Goto(server.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		t.Fatalf("goto: %v", err)
	}

	if _, err := page.WaitForSelector("[data-role='issue-row']"); err != nil {
		t.Fatalf("wait for issue rows: %v", err)
	}

	firstCheckbox := page.Locator("[data-role='issue-select']").First()
	if err := firstCheckbox.Click(); err != nil {
		t.Fatalf("click first checkbox: %v", err)
	}

	secondCheckbox := page.Locator("[data-role='issue-select']").Nth(1)
	if err := page.Keyboard().Down("Shift"); err != nil {
		t.Fatalf("press shift: %v", err)
	}
	if err := secondCheckbox.Click(); err != nil {
		t.Fatalf("shift-click second checkbox: %v", err)
	}
	if err := page.Keyboard().Up("Shift"); err != nil {
		t.Fatalf("release shift: %v", err)
	}

	if _, err := page.WaitForSelector("[data-role='bulk-toolbar']:not([hidden])"); err != nil {
		t.Fatalf("wait for bulk toolbar after multi-select: %v", err)
	}

	countText, err := page.Locator("[data-role='bulk-count']").TextContent()
	if err != nil {
		t.Fatalf("read selection count: %v", err)
	}
	if strings.TrimSpace(countText) == "" {
		t.Fatalf("selection count missing text")
	}

	if _, err := page.SelectOption("[data-role='bulk-status']", playwright.SelectOptionValues{
		Values: playwright.StringSlice("in_progress"),
	}); err != nil {
		t.Fatalf("select status: %v", err)
	}

	applyButton := page.Locator("[data-action='bulk-apply']")
	if err := applyButton.Click(); err != nil {
		t.Fatalf("apply bulk action: %v", err)
	}

	if _, err := page.WaitForSelector("[data-role='bulk-message']:not([hidden])"); err != nil {
		t.Fatalf("wait for success message: %v", err)
	}

	bulkClient.mu.Lock()
	defer bulkClient.mu.Unlock()
	if bulkClient.calls == 0 {
		t.Fatalf("expected bulk client to be invoked")
	}
	if bulkClient.last == nil || len(bulkClient.last.Operations) != 2 {
		t.Fatalf("expected two operations, got %+v", bulkClient.last)
	}
	for _, op := range bulkClient.last.Operations {
		if op.Operation != rpc.OpUpdate {
			t.Fatalf("unexpected operation: %+v", op)
		}
		var update rpc.UpdateArgs
		if err := json.Unmarshal(op.Args, &update); err != nil {
			t.Fatalf("decode update args: %v", err)
		}
		if update.Status == nil || *update.Status != "in_progress" {
			t.Fatalf("expected status in_progress, got %#v", update.Status)
		}
	}
}
