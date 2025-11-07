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
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type navListClient struct {
	issues []*types.Issue
}

type navDetailClient struct {
	called chan string
}

func (c *navListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	data, err := json.Marshal(c.issues)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func (c *navDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	select {
	case c.called <- args.ID:
	default:
	}
	issue := &types.Issue{
		ID:        args.ID,
		Title:     fmt.Sprintf("Detail for %s", args.ID),
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(issue)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func TestKeyboardNavigationMovesSelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 22, 23, 0, 0, 0, time.UTC)

	listClient := &navListClient{
		issues: []*types.Issue{
			{
				ID:        "ui-400",
				Title:     "First issue",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  1,
				UpdatedAt: now,
			},
			{
				ID:        "ui-401",
				Title:     "Second issue",
				Status:    types.StatusInProgress,
				IssueType: types.TypeTask,
				Priority:  2,
				UpdatedAt: now.Add(-10 * time.Minute),
			},
		},
	}

	detailCalls := make(chan string, 4)
	detailClient := &navDetailClient{called: detailCalls}

	baseHTML := renderBasePage(t, "Beads")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse issue list template: %v", err)
	}

	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse issue detail template: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewDetailHandler(detailClient, api.NewMarkdownRenderer()))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				listClient,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
			mux.Handle("/fragments/issue", api.NewDetailFragmentHandler(detailClient, api.NewMarkdownRenderer(), detailTemplate))
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
		_, err := h.Page().WaitForSelector("[data-role='issue-row']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	if err := h.Page().Click("[data-role='issue-list']"); err != nil {
		t.Fatalf("focus issue list: %v", err)
	}

	if err := h.Page().Keyboard().Press("j"); err != nil {
		t.Fatalf("press j: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		text, err := h.Page().TextContent("[data-role='issue-row'].is-active .ui-issue-row-title")
		if err != nil {
			return err
		}
		if strings.TrimSpace(text) != "Second issue" {
			return fmt.Errorf("expected Second issue active, got %q", text)
		}
		return nil
	})

	if err := h.Page().Keyboard().Press("k"); err != nil {
		t.Fatalf("press k: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		text, err := h.Page().TextContent("[data-role='issue-row'].is-active .ui-issue-row-title")
		if err != nil {
			return err
		}
		if strings.TrimSpace(text) != "First issue" {
			return fmt.Errorf("expected First issue active after k, got %q", text)
		}
		return nil
	})

	if err := h.Page().Keyboard().Press("Enter"); err != nil {
		t.Fatalf("press enter: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-role='issue-detail']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	select {
	case <-detailCalls:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected detail handler to be invoked")
	}

	if _, err := h.Page().WaitForFunction(`() => {
      const active = document.activeElement;
      if (!active) {
        return false;
      }
      if (active.getAttribute && active.getAttribute("data-role") === "issue-row") {
        return true;
      }
      const button = active.closest ? active.closest("[data-role='issue-row']") : null;
      return !!(button && button.getAttribute("data-role") === "issue-row");
    }`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)}); err != nil {
		t.Fatalf("expected issue row to hold focus, wait error: %v", err)
	}

	activeRole, err := h.Page().Evaluate("(function () { const active = document.activeElement; return active ? active.getAttribute('data-role') : null; })")
	if err != nil {
		t.Fatalf("evaluate active role: %v", err)
	}
	if role, ok := activeRole.(string); !ok || role != "issue-row" {
		t.Fatalf("expected active element role issue-row, got %#v", activeRole)
	}

	activeIssueID, err := h.Page().Evaluate("(function () { const active = document.activeElement; if (!active) { return null; } if (active.dataset && active.dataset.issueId) { return active.dataset.issueId; } const button = active.closest ? active.closest('[data-role=\"issue-row\"]') : null; return button && button.dataset ? button.dataset.issueId : null; })")
	if err != nil {
		t.Fatalf("evaluate active issue id: %v", err)
	}
	if id, ok := activeIssueID.(string); !ok || strings.TrimSpace(id) != "ui-400" {
		t.Fatalf("expected focused issue row to remain ui-400, got %#v", activeIssueID)
	}
}
