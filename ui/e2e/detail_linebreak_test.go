//go:build ui_e2e

package e2e

import (
	"encoding/json"
	"net/http"
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

type lineBreakListClient struct {
	issues []*types.Issue
}

func (c *lineBreakListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	data, _ := json.Marshal(c.issues)
	return &rpc.Response{Success: true, Data: data}, nil
}

type lineBreakDetailClient struct {
	issue *types.Issue
}

func (c *lineBreakDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	if args == nil || args.ID != c.issue.ID {
		return &rpc.Response{Success: false, Error: "not found"}, nil
	}
	payload := struct {
		*types.Issue
	}{
		Issue: c.issue,
	}
	data, _ := json.Marshal(payload)
	return &rpc.Response{Success: true, Data: data}, nil
}

func TestIssueDetailRendersSlashNAsLineBreak(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 24, 21, 30, 0, 0, time.UTC)
	listClient := &lineBreakListClient{
		issues: []*types.Issue{
			{
				ID:        "ui-1200",
				Title:     "Line 1\\nLine 2",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  2,
				UpdatedAt: now,
			},
		},
	}
	detailClient := &lineBreakDetailClient{
		issue: &types.Issue{
			ID:          "ui-1200",
			Title:       "Line 1\\nLine 2",
			Status:      types.StatusOpen,
			IssueType:   types.TypeTask,
			Priority:    2,
			UpdatedAt:   now,
			Description: "Line A\\nLine B",
		},
	}

	baseHTML := renderBasePage(t, "Slash N Detail Harness")

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

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != http.StatusOK {
		t.Fatalf("GET / status = %d", status)
	}

	page := h.Page()

	if _, err := page.WaitForSelector("[data-role='issue-row']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for issue row: %v", err)
	}

	if err := page.Click("[data-issue-id='ui-1200'] [data-role='issue-row']"); err != nil {
		t.Fatalf("click issue row: %v", err)
	}

	if _, err := page.WaitForSelector("[data-testid='issue-detail'][data-issue-id='ui-1200']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for detail load: %v", err)
	}

	titleHTML, err := page.InnerHTML("[data-testid='issue-detail'] h1")
	if err != nil {
		t.Fatalf("read detail title: %v", err)
	}
	if titleHTML != "Line 1<br>Line 2" {
		t.Fatalf("expected <br> converted title, got %q", titleHTML)
	}

	bodyHTML, err := page.InnerHTML("[data-section='description'] .issue-detail__content")
	if err != nil {
		t.Fatalf("read description html: %v", err)
	}
	if !strings.Contains(bodyHTML, "Line A<br>Line B") {
		t.Fatalf("expected description to contain <br>, got %q", bodyHTML)
	}
	if strings.Contains(bodyHTML, "\\n") {
		t.Fatalf("expected description to omit literal \\n, got %q", bodyHTML)
	}
}
