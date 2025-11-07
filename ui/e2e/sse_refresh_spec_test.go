//go:build ui_e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
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

type liveIssueStore struct {
	mu    sync.RWMutex
	issue *types.Issue
}

func (s *liveIssueStore) Set(issue *types.Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if issue == nil {
		s.issue = nil
		return
	}
	clone := copyIssue(issue)
	s.issue = clone
}

func (s *liveIssueStore) List(args *rpc.ListArgs) (*rpc.Response, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var issues []*types.Issue
	if s.issue != nil {
		include := true
		if args != nil && args.Status != "" {
			include = string(s.issue.Status) == args.Status
		}
		if include {
			issues = append(issues, copyIssue(s.issue))
		}
	}

	data, err := json.Marshal(issues)
	if err != nil {
		return nil, err
	}

	return &rpc.Response{Success: true, Data: data}, nil
}

func (s *liveIssueStore) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.issue == nil || args == nil || s.issue.ID != args.ID {
		return &rpc.Response{Success: false, Error: "issue not found"}, nil
	}

	payload := struct {
		*types.Issue
	}{
		Issue: copyIssue(s.issue),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &rpc.Response{Success: true, Data: data}, nil
}

func copyIssue(issue *types.Issue) *types.Issue {
	if issue == nil {
		return nil
	}
	clone := *issue
	if issue.Labels != nil {
		labels := make([]string, len(issue.Labels))
		copy(labels, issue.Labels)
		clone.Labels = labels
	}
	return &clone
}

func TestSSERefreshUpdatesListAndDetail(t *testing.T) {
	t.Parallel()

	store := &liveIssueStore{}
	now := time.Date(2025, 10, 24, 17, 0, 0, 0, time.UTC)

	initial := &types.Issue{
		ID:        "ui-900",
		Title:     "Awaiting SSE",
		Status:    types.StatusOpen,
		IssueType: types.TypeFeature,
		Priority:  2,
		UpdatedAt: now,
		Labels:    []string{"triage"},
	}
	store.Set(initial)

	baseHTML := renderBasePage(t, "Beads UI")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}

	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse detail template: %v", err)
	}

	renderer := api.NewMarkdownRenderer()
	source := newSSEStubSource(4)
	t.Cleanup(source.Close)

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(store))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				store,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
			mux.Handle("/api/issues/", api.NewDetailHandler(store, renderer))
			mux.Handle("/fragments/issue", api.NewDetailFragmentHandler(store, renderer, detailTemplate))
			mux.Handle("/events", api.NewEventStreamHandler(source, api.WithHeartbeatInterval(200*time.Millisecond)))
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer waitCancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-issue-id='ui-900']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	if err := h.Page().Click("[data-issue-id='ui-900'] [data-role='issue-row']"); err != nil {
		t.Fatalf("click issue row: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-testid='issue-detail'][data-issue-id='ui-900']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	select {
	case <-source.subscribed:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for SSE subscription")
	}

	updated := &types.Issue{
		ID:        "ui-900",
		Title:     "Updated via SSE",
		Status:    types.StatusInProgress,
		IssueType: types.TypeFeature,
		Priority:  1,
		Assignee:  "codex",
		UpdatedAt: now.Add(45 * time.Second),
		Labels:    []string{"ui", "live"},
	}
	store.Set(updated)

	source.Publish(api.IssueEvent{
		Type:  api.EventTypeUpdated,
		Issue: api.IssueToSummary(updated),
	})

	if _, err := h.Page().WaitForFunction(`() => {
      const row = document.querySelector("[data-role='issue-row'][data-issue-id='ui-900']");
      return !row;
    }`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)}); err != nil {
		t.Fatalf("issue row did not leave ready queue: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
      const badge = document.querySelector("[data-testid='issue-detail'] [data-field='status']");
      if (!badge || !badge.textContent) return false;
      return badge.textContent.trim().toLowerCase() === "in progress";
    }`, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)}); err != nil {
		t.Fatalf("detail status did not refresh: %v", err)
	}

	statusText, err := h.Page().TextContent("[data-testid='issue-detail'] [data-field='status']")
	if err != nil {
		t.Fatalf("read detail status: %v", err)
	}
	if trimmed := strings.TrimSpace(statusText); trimmed == "" || !strings.EqualFold(trimmed, "In Progress") {
		t.Fatalf("expected detail status 'In Progress', got %q", statusText)
	}

	priorityText, err := h.Page().TextContent("[data-testid='issue-detail'] [data-field='priority']")
	if err != nil {
		t.Fatalf("read priority badge: %v", err)
	}
	if strings.TrimSpace(priorityText) == "" {
		t.Fatalf("expected priority badge to remain visible")
	}
}
