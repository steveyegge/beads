//go:build ui_e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

type quickListClient struct {
	mu     sync.Mutex
	issues []*types.Issue
}

func (c *quickListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	clone := make([]*types.Issue, len(c.issues))
	for i, issue := range c.issues {
		clone[i] = cloneIssue(issue)
	}

	data, err := json.Marshal(clone)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func (c *quickListClient) add(issue *types.Issue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	clone := cloneIssue(issue)
	c.issues = append([]*types.Issue{clone}, c.issues...)
}

func (c *quickListClient) get(id string) *types.Issue {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, issue := range c.issues {
		if issue != nil && issue.ID == id {
			return cloneIssue(issue)
		}
	}
	return nil
}

type quickDetailClient struct {
	list *quickListClient
}

func (c *quickDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	if args == nil || args.ID == "" {
		return &rpc.Response{Success: false, Error: "missing id"}, nil
	}
	issue := c.list.get(args.ID)
	if issue == nil {
		return &rpc.Response{Success: false, Error: "not found"}, nil
	}
	payload := struct {
		*types.Issue
		Labels            []string            `json:"labels"`
		DependencyRecords []*types.Dependency `json:"dependency_records"`
		Dependencies      []*types.Issue      `json:"dependencies"`
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

type quickCreateClient struct {
	list *quickListClient
	mu   sync.Mutex
	next int
}

func (c *quickCreateClient) Create(args *rpc.CreateArgs) (*rpc.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if args == nil {
		return nil, fmt.Errorf("missing args")
	}

	if c.next == 0 {
		c.next = 801
	}

	id := fmt.Sprintf("ui-%d", c.next)
	c.next++

	now := time.Now().UTC()

	issue := &types.Issue{
		ID:          id,
		Title:       args.Title,
		Description: args.Description,
		Status:      types.StatusOpen,
		IssueType:   types.IssueType(args.IssueType),
		Priority:    args.Priority,
		Labels:      append([]string(nil), args.Labels...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	c.list.add(issue)

	data, err := json.Marshal(issue)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func cloneIssue(issue *types.Issue) *types.Issue {
	if issue == nil {
		return nil
	}
	copy := *issue
	if issue.Labels != nil {
		copy.Labels = append([]string(nil), issue.Labels...)
	}
	return &copy
}

func TestQuickCreateModalCreatesIssue(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 11, 0, 0, 0, time.UTC)

	listClient := &quickListClient{
		issues: []*types.Issue{
			{
				ID:        "ui-700",
				Title:     "Audit active issue for quick create",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  1,
				UpdatedAt: now,
			},
		},
	}
	detailClient := &quickDetailClient{list: listClient}
	createClient := &quickCreateClient{list: listClient, next: 701}

	baseHTML := renderBasePage(t, "Quick Create Harness")

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
	createHandler := api.NewCreateHandler(createClient, nil)

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(root *http.ServeMux) {
			root.Handle("/api/issues", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					createHandler.ServeHTTP(w, r)
					return
				}
				listHandler.ServeHTTP(w, r)
			}))
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

	page.On("console", func(msg playwright.ConsoleMessage) {
		t.Logf("console[%s]: %s", msg.Type(), msg.Text())
	})

	page.On("response", func(resp playwright.Response) {
		t.Logf("response %d %s", resp.Status(), resp.URL())
	})

	page.On("request", func(req playwright.Request) {
		t.Logf("request %s", req.URL())
	})

	if _, err := page.Evaluate(`() => {
  const bucket = [];
  Object.defineProperty(window, '__quickCreateSyntaxErrors', {
    configurable: true,
    enumerable: false,
    get() {
      return bucket.slice();
    },
  });
  if (window.htmx && typeof window.htmx.on === 'function') {
    window.htmx.on('htmx:syntax:error', (ev) => {
      const detail = ev && ev.detail && ev.detail.xhr ? ev.detail.xhr.responseText : '(no response)';
      const target = ev && ev.detail && ev.detail.target ? ev.detail.target.getAttribute('hx-get') || ev.detail.target.outerHTML : '(no target)';
      bucket.push({ detail, target });
      console.error('htmx syntax error detail', detail, target);
    });
  }
}`, nil); err != nil {
		t.Fatalf("install htmx error logger: %v", err)
	}

	if _, err := page.WaitForSelector("[data-role='issue-row']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for issue list: %v", err)
	}

	if err := page.Click("[data-issue-id='ui-700']"); err != nil {
		t.Fatalf("select issue row: %v", err)
	}

	if _, err := page.WaitForSelector("[data-testid='issue-detail']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for issue detail: %v", err)
	}

	if err := page.Keyboard().Press("c"); err != nil {
		t.Fatalf("press quick create shortcut: %v", err)
	}

	var syntaxErrors []map[string]any
	if raw, err := page.Evaluate(`() => Array.isArray(window.__quickCreateSyntaxErrors) ? window.__quickCreateSyntaxErrors : []`, nil); err != nil {
		t.Fatalf("read htmx syntax errors: %v", err)
	} else {
		if list, ok := raw.([]any); ok {
			syntaxErrors = make([]map[string]any, 0, len(list))
			for _, entry := range list {
				if item, ok := entry.(map[string]any); ok {
					syntaxErrors = append(syntaxErrors, item)
				}
			}
		}
	}
	if len(syntaxErrors) > 0 {
		first := syntaxErrors[0]
		detail, _ := first["detail"].(string)
		target, _ := first["target"].(string)
		t.Fatalf("quick create triggered htmx syntax error: detail=%q target=%q", detail, target)
	}

	if html, err := page.Evaluate(`() => {
  const el = document.querySelector('[data-testid="quick-create-overlay"]');
  if (!el) {
    return { present: false };
  }
  return {
    present: true,
    className: el.className,
    hidden: el.hasAttribute('hidden')
  };
}`); err == nil {
		t.Logf("overlay state after shortcut: %v", html)
	}

	if _, err := page.WaitForSelector("[data-testid='quick-create-overlay']", playwright.PageWaitForSelectorOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for quick create overlay: %v", err)
	}

	discoveredValue, err := page.InputValue("[data-testid='quick-create-discovered-from']")
	if err != nil {
		t.Fatalf("read discovered_from input: %v", err)
	}
	if discoveredValue != "ui-700" {
		t.Fatalf("expected discovered_from ui-700, got %q", discoveredValue)
	}

	if err := page.Fill("[data-testid='quick-create-title']", "Quick modal created issue"); err != nil {
		t.Fatalf("fill title: %v", err)
	}
	if err := page.Fill("[data-testid='quick-create-description']", "Created from modal test"); err != nil {
		t.Fatalf("fill description: %v", err)
	}
	if _, err := page.SelectOption("[data-testid='quick-create-type']", playwright.SelectOptionValues{
		Values: playwright.StringSlice(string(types.TypeTask)),
	}); err != nil {
		t.Fatalf("select issue type: %v", err)
	}
	if _, err := page.SelectOption("[data-testid='quick-create-priority']", playwright.SelectOptionValues{
		Values: playwright.StringSlice("2"),
	}); err != nil {
		t.Fatalf("select priority: %v", err)
	}

	if err := page.Click("[data-testid='quick-create-submit']"); err != nil {
		t.Fatalf("submit quick create form: %v", err)
	}

	if _, err := page.WaitForSelector("[data-testid='quick-create-overlay']", playwright.PageWaitForSelectorOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for overlay to close: %v", err)
	}

	if _, err := page.WaitForSelector("[data-testid='quick-create-toast']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for success toast: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	h.MustWaitUntil(ctx, func(ctx context.Context) error {
		_, err := page.WaitForFunction("() => !!document.querySelector(\"[data-issue-id='ui-701']\")", playwright.PageWaitForFunctionOptions{
			Timeout: playwright.Float(3500),
		})
		return err
	})
}
