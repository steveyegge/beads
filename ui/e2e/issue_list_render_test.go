//go:build ui_e2e

package e2e

import (
	"context"
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
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type listClientStub struct {
	issues []*types.Issue
}

func (s *listClientStub) List(args *rpc.ListArgs) (*rpc.Response, error) {
	data, err := json.Marshal(s.issues)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

func TestIssueListFragmentRendersWithHTMX(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 22, 22, 0, 0, 0, time.UTC)
	client := &listClientStub{
		issues: []*types.Issue{
			{
				ID:        "ui-300",
				Title:     "Queued for action",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  1,
				UpdatedAt: now.Add(-15 * time.Minute),
			},
			{
				ID:        "ui-301",
				Title:     "Another task",
				Status:    types.StatusInProgress,
				IssueType: types.TypeTask,
				Priority:  2,
				UpdatedAt: now.Add(-45 * time.Minute),
			},
		},
	}

	baseHTML := renderBasePage(t, "Beads")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(client))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				client,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
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

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-role='issue-list-rows']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	if _, err := h.Page().Evaluate(`() => {
		const target = document.querySelector("[data-role='issue-list']");
		if (!target || !window.htmx || typeof window.htmx.ajax !== "function") {
			throw new Error("htmx target unavailable");
		}
		const result = window.htmx.ajax("GET", "/fragments/issues?status=open", { target, swap: "innerHTML" });
		if (result && typeof result.then === "function") {
			return result.then(() => true);
		}
		return true;
	}`); err != nil {
		t.Fatalf("dispatch htmx reload: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-issue-id='ui-300']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	text, err := h.Page().TextContent("[data-issue-id='ui-300'] .ui-issue-row-title")
	if err != nil {
		t.Fatalf("read issue title: %v", err)
	}
	if want := "Queued for action"; strings.TrimSpace(text) != want {
		t.Fatalf("expected issue title %q, got %q", want, text)
	}
}

func TestIssueListRendersUnknownPriorityBadges(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 22, 23, 0, 0, 0, time.UTC)
	client := &listClientStub{
		issues: []*types.Issue{
			{
				ID:        "ui-neg",
				Title:     "Negative priority issue",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  -1,
				UpdatedAt: now.Add(-45 * time.Minute),
			},
			{
				ID:        "ui-high",
				Title:     "Out of range priority",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  7,
				UpdatedAt: now.Add(-30 * time.Minute),
			},
		},
	}

	baseHTML := renderBasePage(t, "Beads Unknown Priorities")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(client))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				client,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
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

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-role='issue-list-rows']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	for _, tc := range []struct {
		issueID   string
		wantClass string
		wantLabel string
	}{
		{issueID: "ui-neg", wantClass: "ui-badge--priority-p?", wantLabel: "P?"},
		{issueID: "ui-high", wantClass: "ui-badge--priority-p?", wantLabel: "P?"},
	} {
		selector := "[data-issue-id='" + tc.issueID + "'] .ui-badge--priority"
		handle, err := h.Page().WaitForSelector(selector, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		if err != nil {
			t.Fatalf("wait for priority badge %s: %v", tc.issueID, err)
		}
		text, err := handle.TextContent()
		if err != nil {
			t.Fatalf("read priority label %s: %v", tc.issueID, err)
		}
		if got := strings.TrimSpace(text); got != tc.wantLabel {
			t.Fatalf("priority label for %s = %q, want %q", tc.issueID, got, tc.wantLabel)
		}
		className, err := handle.GetAttribute("class")
		if err != nil {
			t.Fatalf("read priority class %s: %v", tc.issueID, err)
		}
		if !strings.Contains(className, tc.wantClass) {
			t.Fatalf("priority class for %s = %q, want contains %q", tc.issueID, className, tc.wantClass)
		}
		box, err := handle.BoundingBox()
		if err != nil {
			t.Fatalf("read badge bounding box for %s: %v", tc.issueID, err)
		}
		if box == nil || box.Width <= 0 || box.Height <= 0 {
			t.Fatalf("badge for %s should occupy space, got %+v", tc.issueID, box)
		}
	}
}

func TestIssueRowClickUpdatesActiveStyling(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 10, 0, 0, 0, time.UTC)
	client := &listClientStub{
		issues: []*types.Issue{
			{
				ID:        "ui-410",
				Title:     "First issue",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  1,
				UpdatedAt: now.Add(-10 * time.Minute),
			},
			{
				ID:        "ui-411",
				Title:     "Second issue",
				Status:    types.StatusInProgress,
				IssueType: types.TypeTask,
				Priority:  2,
				UpdatedAt: now.Add(-20 * time.Minute),
			},
		},
	}

	baseHTML := renderBasePage(t, "Beads")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(client))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				client,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
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

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-issue-id='ui-410'] [data-role='issue-row'].is-active", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	if err := h.Page().Click("[data-issue-id='ui-411'] [data-role='issue-row']"); err != nil {
		t.Fatalf("click second issue: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		active, err := h.Page().Evaluate("(function () { const active = document.querySelector(\"[data-role='issue-row'].is-active\"); return active ? active.dataset.issueId : null; })")
		if err != nil {
			return err
		}
		if activeStr, _ := active.(string); activeStr != "ui-411" {
			return playwright.ErrTimeout
		}
		return nil
	})

	roleValue, err := h.Page().GetAttribute("[data-issue-id='ui-410'] [data-role='issue-row']", "aria-selected")
	if err != nil {
		t.Fatalf("read aria-selected for first row: %v", err)
	}
	if roleValue == "true" {
		t.Fatalf("expected first issue to lose active selection after click")
	}
}
