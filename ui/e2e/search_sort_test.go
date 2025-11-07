//go:build ui_e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
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

type sortListClient struct {
	issues []*types.Issue
}

func (c *sortListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	data, err := json.Marshal(c.issues)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

type sortDetailClient struct {
	byID map[string]*types.Issue
}

func (c *sortDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	issue, ok := c.byID[args.ID]
	if !ok {
		return &rpc.Response{Success: false, Error: "not_found"}, nil
	}
	payload, err := json.Marshal(struct {
		*types.Issue
		Labels []string `json:"labels,omitempty"`
	}{
		Issue:  issue,
		Labels: issue.Labels,
	})
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: payload}, nil
}

func TestCommandPaletteSortShortcuts(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 24, 12, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{
			ID:          "quick-hero",
			Title:       "Quick search hero result",
			Status:      types.StatusOpen,
			IssueType:   types.TypeFeature,
			Priority:    2,
			Description: "Ensure query matching places this issue first by relevance.",
			UpdatedAt:   now.Add(-30 * time.Minute),
		},
		{
			ID:        "ui-202",
			Title:     "Quick search indexing",
			Status:    types.StatusOpen,
			IssueType: types.TypeFeature,
			Priority:  1,
			UpdatedAt: now.Add(-5 * time.Minute),
		},
		{
			ID:          "ui-203",
			Title:       "Improve search responses",
			Status:      types.StatusInProgress,
			IssueType:   types.TypeTask,
			Priority:    0,
			Description: "Ensure quick fallback behaviour for palette.",
			UpdatedAt:   now.Add(-15 * time.Minute),
		},
	}

	listClient := &sortListClient{issues: issues}
	detailClient := &sortDetailClient{
		byID: map[string]*types.Issue{
			issues[0].ID: issues[0],
			issues[1].ID: issues[1],
			issues[2].ID: issues[2],
		},
	}

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

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-testid='ui-title']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	_, _ = h.Page().Evaluate("() => { window.__bdOriginalHTMX = window.htmx; window.htmx = undefined; return true; }")

	_, _ = h.Page().Evaluate("() => { try { window.Alpine && window.Alpine.start && window.Alpine.start(); } catch (error) { console.warn('Alpine.start', error); } return true; }")

	_, _ = h.Page().Evaluate(`() => {
  const originalFetch = window.fetch;
  if (typeof originalFetch !== 'function') {
    return false;
  }
  window.__paletteRequests = [];
  window.fetch = function(...args) {
    try {
      const input = args[0];
      const url = typeof input === 'string' ? input : (input && input.url) || '';
      if (Array.isArray(window.__paletteRequests)) {
        window.__paletteRequests.push(url);
      }
    } catch (error) {
      console.warn('record fetch error', error);
    }
    return originalFetch.apply(this, args);
  };
  return true;
}`)

	if _, err := h.Page().WaitForFunction(`() => typeof window.bdPaletteOpen === 'function'`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("bdPaletteOpen not ready: %v", err)
	}

	openedAny, err := h.Page().Evaluate(`() => {
  if (typeof window.bdPaletteOpen === 'function') {
    return window.bdPaletteOpen();
  }
  const trigger = document.querySelector('[data-testid="command-palette-trigger"]');
  if (trigger && typeof trigger.click === 'function') {
    trigger.click();
    return true;
  }
  return false;
}`)
	if err != nil {
		t.Fatalf("invoke palette open: %v", err)
	}
	if opened, _ := openedAny.(bool); !opened {
		t.Fatalf("command palette failed to open")
	}

	if _, err := h.Page().WaitForSelector("[data-testid='command-palette-panel']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("command palette panel visible: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
  const inst = window.__bdPalette;
  return !!(inst && inst.isOpen === true);
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("palette instance did not report open state: %v", err)
	}

	if _, err := h.Page().Evaluate(`value => {
  const input = document.querySelector("[data-testid='command-palette-input']");
  if (!input) {
    return false;
  }
  if (typeof input.focus === "function") {
    input.focus();
  }
  input.value = value;
  const evt = new Event("input", { bubbles: true, composed: true });
  input.dispatchEvent(evt);
  return true;
}`, "quick"); err != nil {
		t.Fatalf("fill palette input: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
  const list = window.__paletteRequests;
  return Array.isArray(list) && list.length > 0;
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("search request not issued: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
  const inst = window.__bdPalette;
  return !!inst && inst.query === 'quick';
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("palette query not updated: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
  const inst = window.__bdPalette;
  return !!(inst && Array.isArray(inst.results) && inst.results.length >= 3);
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("wait for initial results: %v", err)
	}

	expectOrder := func(expected []string) {
		want := strings.Join(expected, ",")
		script := fmt.Sprintf(`() => {
  const inst = window.__bdPalette;
  if (!inst || !Array.isArray(inst.results)) {
    return false;
  }
  return inst.results.map(r => r.id).join(",") === %q;
}`, want)
		if _, err := h.Page().WaitForFunction(script, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
			t.Fatalf("wait for order %v: %v", expected, err)
		}
	}

	expectOrder([]string{"quick-hero", "ui-202", "ui-203"})

	if err := h.Page().Keyboard().Press("Control+Alt+Digit3"); err != nil {
		t.Fatalf("press priority shortcut: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
  const inst = window.__bdPalette;
  return !!inst && inst.sortMode === 'priority';
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("wait for priority sort mode: %v", err)
	}

	expectOrder([]string{"ui-203", "ui-202", "quick-hero"})

	valueAny, err := h.Page().Evaluate("() => window.localStorage.getItem('bd:palette:sort')", nil)
	if err != nil {
		t.Fatalf("read stored sort: %v", err)
	}
	if value, _ := valueAny.(string); value != "priority" {
		t.Fatalf("expected stored priority sort, got %v", valueAny)
	}

	if err := h.Page().Keyboard().Press("Control+Alt+Digit2"); err != nil {
		t.Fatalf("press recent shortcut: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
  const inst = window.__bdPalette;
  return !!inst && inst.sortMode === 'recent';
}`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(4000)}); err != nil {
		t.Fatalf("wait for recent sort mode: %v", err)
	}

	expectOrder([]string{"ui-202", "ui-203", "quick-hero"})
}
