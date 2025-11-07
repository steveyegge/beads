//go:build ui_e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/search"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type paletteListClient struct {
	issues []*types.Issue
}

func (c *paletteListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	data, err := json.Marshal(c.issues)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

type paletteDetailClient struct {
	byID map[string]*types.Issue
}

func (c *paletteDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	issue, ok := c.byID[args.ID]
	if !ok {
		return &rpc.Response{Success: false, Error: "not_found"}, nil
	}
	data, err := json.Marshal(struct {
		*types.Issue
		Labels []string `json:"labels,omitempty"`
	}{
		Issue:  issue,
		Labels: issue.Labels,
	})
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func TestCommandPaletteNavigatesToIssue(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 10, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{
			ID:          "ui-600",
			Title:       "Daemon health overview",
			Status:      types.StatusOpen,
			IssueType:   types.TypeFeature,
			Priority:    1,
			Labels:      []string{"daemon"},
			UpdatedAt:   now.Add(-5 * time.Minute),
			Description: "Expose daemon heartbeat and queue depth in the UI.",
		},
		{
			ID:          "ui-601",
			Title:       "Command palette polish",
			Status:      types.StatusInProgress,
			IssueType:   types.TypeTask,
			Priority:    2,
			Labels:      []string{"ui", "palette", "daemon"},
			UpdatedAt:   now.Add(-10 * time.Minute),
			Description: "Refine keyboard navigation and empty states.",
		},
	}

	listClient := &paletteListClient{issues: issues}
	detailClient := &paletteDetailClient{byID: map[string]*types.Issue{
		issues[0].ID: issues[0],
		issues[1].ID: issues[1],
	}}

	searchService := search.NewService(listClient,
		search.WithClock(func() time.Time { return now }),
		search.WithFetchLimit(20),
	)

	baseHTML := renderBasePage(t, "Beads")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse issue_list.tmpl: %v", err)
	}
	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse issue_detail.tmpl: %v", err)
	}

	renderer := api.NewMarkdownRenderer()

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewDetailHandler(detailClient, renderer))
			mux.Handle("/api/search", api.NewSearchHandler(searchService))
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
		t.Fatalf("issue list not rendered: %v", err)
	}

	_, _ = h.Page().Evaluate("() => { window.__bdOriginalHTMX = window.htmx; window.htmx = undefined; return true; }")
	_, _ = h.Page().Evaluate("() => { try { window.Alpine && window.Alpine.start && window.Alpine.start(); } catch (error) { console.warn('Alpine.start', error); } return true; }")
	if _, err := h.Page().Evaluate(`() => {
	const originalFetch = window.fetch;
	if (typeof originalFetch !== 'function') {
	  return false;
	}
	window.__paletteRequests = [];
	window.fetch = function(...args) {
	  try {
	    const input = args[0];
	    const url = typeof input === 'string' ? input : (input && input.url) || '';
	    window.__paletteRequests.push(url);
	  } catch (error) {
	    console.warn('record fetch error', error);
	  }
	  return originalFetch.apply(this, args);
	};
	return true;
}`); err != nil {
		t.Fatalf("install fetch recorder: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`
		() => {
			const el = document.querySelector('[data-testid="command-palette"]');
			return !!(el && (el.__x || el._x_dataStack));
		}
	`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("command palette Alpine controller not ready: %v", err)
	}

	if _, err := h.Page().Evaluate(`() => new Promise((resolve, reject) => {
	const el = document.querySelector('[data-testid="command-palette"]');
	if (!el) { reject('command palette element missing'); return; }
	const stack = Array.isArray(el._x_dataStack) && el._x_dataStack.length ? el._x_dataStack[0] : null;
	const instance = stack?.$data || el.__x?.$data || stack;
	if (!instance || typeof instance.fetchResults !== 'function') {
		reject('command palette instance not ready');
		return;
	}
	instance.query = 'daemon';
	instance.isOpen = true;
	try {
		const pending = instance.fetchResults();
		if (pending && typeof pending.then === 'function') {
			pending.catch((error) => reject(error?.message || error));
		}
	} catch (error) {
		reject(error?.message || error);
		return;
	}
	const tick = () => {
		if (Array.isArray(instance.results) && instance.results.length >= 2) {
			resolve(true);
			return;
		}
		if (instance.error) {
			reject(instance.error);
			return;
		}
		setTimeout(tick, 50);
	};
	tick();
})`); err != nil {
		t.Fatalf("waiting for search results: %v", err)
	}

	reqSliceAny, err := h.Page().Evaluate(`() => Array.isArray(window.__paletteRequests) ? window.__paletteRequests.slice() : []`, nil)
	if err != nil {
		t.Fatalf("read palette requests: %v", err)
	}
	reqSlice, ok := reqSliceAny.([]any)
	if !ok {
		t.Fatalf("unexpected request list type %T", reqSliceAny)
	}
	if len(reqSlice) == 0 {
		t.Fatalf("expected at least one search request")
	}
	lastInitial, _ := reqSlice[len(reqSlice)-1].(string)
	if !strings.Contains(lastInitial, "sort=relevance") {
		t.Fatalf("expected relevance sort param, got %q", lastInitial)
	}

	sortValue, err := h.Page().InputValue("[data-testid='command-palette-sort']")
	if err != nil {
		t.Fatalf("read sort select value: %v", err)
	}
	if sortValue != "relevance" {
		t.Fatalf("expected sort select to default to relevance, got %s", sortValue)
	}

	initialRequests := len(reqSlice)

	if _, err := h.Page().SelectOption("[data-testid='command-palette-sort']", playwright.SelectOptionValues{Values: playwright.StringSlice("priority")}); err != nil {
		t.Fatalf("select priority sort: %v", err)
	}

	if _, err := h.Page().Evaluate(`expected => { window.__expectedPaletteRequests = expected; return true; }`, initialRequests); err != nil {
		t.Fatalf("record expected request count: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
  const list = Array.isArray(window.__paletteRequests) ? window.__paletteRequests : [];
  const expected = typeof window.__expectedPaletteRequests === 'number' ? window.__expectedPaletteRequests : 0;
  return list.length > expected;
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("wait for priority fetch: %v", err)
	}

	updatedAny, err := h.Page().Evaluate(`() => window.__paletteRequests.slice()`, nil)
	if err != nil {
		t.Fatalf("read updated requests: %v", err)
	}
	updatedSlice, ok := updatedAny.([]any)
	if !ok || len(updatedSlice) == 0 {
		t.Fatalf("unexpected updated request slice: %T", updatedAny)
	}
	lastURL, _ := updatedSlice[len(updatedSlice)-1].(string)
	if !strings.Contains(lastURL, "sort=priority") {
		t.Fatalf("expected priority sort param, got %q", lastURL)
	}

	if err := h.Page().Click("[data-testid='command-palette-results'] li:nth-of-type(2)"); err != nil {
		t.Fatalf("click second command palette result: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
	const el = document.querySelector('[data-testid="command-palette"]');
	if (!el) { return true; }
	const stack = Array.isArray(el._x_dataStack) && el._x_dataStack.length ? el._x_dataStack[0] : null;
	const instance = stack?.$data || el.__x?.$data || stack;
	return !!instance && instance.isOpen === false;
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("palette did not close after selection: %v", err)
	}

	_, _ = h.Page().Evaluate("() => { if (window.__bdOriginalHTMX !== undefined) { window.htmx = window.__bdOriginalHTMX; } return true; }")

	if _, err := h.Page().WaitForFunction(`
        () => {
            const title = document.querySelector('[data-testid="issue-detail"] .issue-detail__title');
            return !!title && title.textContent?.trim() === 'Command palette polish';
        }
    `, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("issue detail did not update: %v", err)
	}
}

func TestCommandPaletteHandlesHTMXPromiseAjax(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 10, 30, 0, 0, time.UTC)
	issues := []*types.Issue{
		{
			ID:          "ui-700",
			Title:       "Daemon insights in palette",
			Status:      types.StatusOpen,
			IssueType:   types.TypeFeature,
			Priority:    1,
			Description: "Surfacing daemon metrics via command palette.",
			UpdatedAt:   now.Add(-3 * time.Minute),
		},
		{
			ID:          "ui-701",
			Title:       "Palette Promise interop",
			Status:      types.StatusInProgress,
			IssueType:   types.TypeTask,
			Priority:    2,
			Description: "Ensure palette works with modern htmx ajax Promise.",
			UpdatedAt:   now.Add(-6 * time.Minute),
		},
	}

	listClient := &paletteListClient{issues: issues}
	detailClient := &paletteDetailClient{byID: map[string]*types.Issue{
		issues[0].ID: issues[0],
		issues[1].ID: issues[1],
	}}

	searchService := search.NewService(listClient,
		search.WithClock(func() time.Time { return now }),
		search.WithFetchLimit(20),
	)

	baseHTML := renderBasePage(t, "Beads")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse issue_list.tmpl: %v", err)
	}
	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse issue_detail.tmpl: %v", err)
	}

	renderer := api.NewMarkdownRenderer()

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewDetailHandler(detailClient, renderer))
			mux.Handle("/api/search", api.NewSearchHandler(searchService))
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
		t.Fatalf("issue list not rendered: %v", err)
	}

	if _, err := h.Page().Evaluate(`() => {
  if (!window.htmx || typeof window.htmx.ajax !== 'function') {
    throw new Error('htmx.ajax not available');
  }
  window.__paletteOriginalAjax = window.htmx.ajax;
  window.htmx.ajax = function(method, url, options) {
    const headers = (options && options.headers) || {};
    return fetch(url, {
      method,
      headers,
      credentials: 'same-origin'
    }).then(async (resp) => {
      const text = await resp.text();
      return {
        xhr: {
          status: resp.status,
          responseText: text,
          getResponseHeader: (name) => resp.headers.get(name) || null,
        },
      };
    });
  };
  return true;
}`); err != nil {
		t.Fatalf("install Promise-based htmx ajax stub: %v", err)
	}
	defer func() {
		_, _ = h.Page().Evaluate(`() => {
    if (window.__paletteOriginalAjax) {
      window.htmx.ajax = window.__paletteOriginalAjax;
    }
    return true;
  }`)
	}()

	if _, err := h.Page().WaitForFunction(`
		() => {
			const el = document.querySelector('[data-testid="command-palette"]');
			return !!(el && (el.__x || el._x_dataStack));
		}
	`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("command palette Alpine controller not ready: %v", err)
	}

	if _, err := h.Page().Evaluate(`() => new Promise((resolve, reject) => {
	const el = document.querySelector('[data-testid="command-palette"]');
	if (!el) { reject('command palette element missing'); return; }
	const stack = Array.isArray(el._x_dataStack) && el._x_dataStack.length ? el._x_dataStack[0] : null;
	const instance = stack?.$data || el.__x?.$data || stack;
	if (!instance || typeof instance.fetchResults !== 'function') {
		reject('command palette instance not ready');
		return;
	}
	instance.query = 'palette';
	instance.isOpen = true;
	try {
		const pending = instance.fetchResults();
		if (pending && typeof pending.catch === 'function') {
			pending.catch((error) => reject(error?.message || error));
		}
	} catch (error) {
		reject(error?.message || error);
		return;
	}
	let attempts = 0;
	const maxAttempts = 80;
	const tick = () => {
		if (Array.isArray(instance.results) && instance.results.length >= 2) {
			resolve(true);
			return;
		}
		if (instance.error) {
			reject(instance.error);
			return;
		}
		attempts += 1;
		if (attempts > maxAttempts) {
			reject('results not loaded');
			return;
		}
		setTimeout(tick, 50);
	};
	tick();
})`); err != nil {
		t.Fatalf("waiting for search results with Promise ajax: %v", err)
	}

	countAny, err := h.Page().Evaluate(`() => {
	const el = document.querySelector('[data-testid="command-palette"]');
	if (!el) { return 0; }
	const stack = Array.isArray(el._x_dataStack) && el._x_dataStack.length ? el._x_dataStack[0] : null;
	const instance = stack?.$data || el.__x?.$data || stack;
	if (!instance || !Array.isArray(instance.results)) {
		return 0;
	}
	return instance.results.length;
}`, nil)
	if err != nil {
		t.Fatalf("read palette results length: %v", err)
	}
	var count int
	switch v := countAny.(type) {
	case float64:
		count = int(v)
	case float32:
		count = int(v)
	case int:
		count = v
	case int32:
		count = int(v)
	case int64:
		count = int(v)
	default:
		t.Fatalf("unexpected result length type %T", countAny)
	}
	if count < 2 {
		t.Fatalf("expected at least 2 results, got %d", count)
	}
}
