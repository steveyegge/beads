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

type drawerListClient struct {
	issues []*types.Issue
}

type drawerDetailClient struct{}

func (c *drawerListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	data, err := json.Marshal(c.issues)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func (c *drawerDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	issue := &types.Issue{
		ID:          args.ID,
		Title:       "Drawer Detail " + args.ID,
		Status:      types.StatusInProgress,
		IssueType:   types.TypeFeature,
		Priority:    1,
		Description: "Detail body for " + args.ID,
		Notes:       "Notes for " + args.ID,
		UpdatedAt:   time.Now().UTC(),
	}

	payload := struct {
		*types.Issue
		Labels            []string            `json:"labels"`
		DependencyRecords []*types.Dependency `json:"dependency_records"`
		Dependencies      []*types.Issue      `json:"dependencies"`
	}{
		Issue:  issue,
		Labels: []string{"ui", "drawer"},
		DependencyRecords: []*types.Dependency{
			{DependsOnID: "bd-42", Type: types.DepBlocks},
		},
		Dependencies: []*types.Issue{
			{
				ID:        "bd-42",
				Title:     "Blocking issue",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  2,
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{Success: true, Data: data}, nil
}

func TestDetailDrawerPopulatesOnSelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 22, 23, 30, 0, 0, time.UTC)

	listClient := &drawerListClient{issues: []*types.Issue{
		{
			ID:        "ui-500",
			Title:     "First list issue",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  2,
			UpdatedAt: now,
		},
		{
			ID:        "ui-501",
			Title:     "Second list issue",
			Status:    types.StatusInProgress,
			IssueType: types.TypeFeature,
			Priority:  1,
			UpdatedAt: now.Add(-5 * time.Minute),
		},
	}}

	detailClient := &drawerDetailClient{}

	baseHTML := renderBasePage(t, "Beads")

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
			mux.Handle("/api/issues/", api.NewDetailHandler(detailClient, renderer))
			mux.Handle("/fragments/issues", api.NewListFragmentHandler(
				listClient,
				api.WithListFragmentTemplate(listTemplate),
				api.WithListFragmentClock(func() time.Time { return now }),
			))
			mux.Handle("/fragments/issue", api.NewDetailFragmentHandler(detailClient, renderer, detailTemplate))
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
		_, err := h.Page().WaitForSelector("[data-role='issue-row']", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(2000)})
		return err
	})

	if err := h.Page().Click("[data-issue-id='ui-501']"); err != nil {
		t.Fatalf("click second issue: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-testid='issue-detail'] [data-field='status']", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(2000)})
		return err
	})

	idBadge, err := h.Page().TextContent("[data-testid='issue-detail'] [data-testid='issue-detail-id']")
	if err != nil {
		t.Fatalf("read detail id: %v", err)
	}
	if trimmed := strings.TrimSpace(idBadge); trimmed != "ui-501" {
		t.Fatalf("expected detail id ui-501, got %q", trimmed)
	}

	description, err := h.Page().TextContent("[data-testid='issue-detail'] [data-section='description']")
	if err != nil {
		t.Fatalf("read description: %v", err)
	}
	if !strings.Contains(description, "Detail body for ui-501") {
		t.Fatalf("expected description content, got %q", description)
	}

	depText, err := h.Page().TextContent("[data-testid='issue-detail'] [data-dependency='blocks']")
	if err != nil {
		t.Fatalf("read dependency group: %v", err)
	}
	if !strings.Contains(depText, "bd-42") {
		t.Fatalf("expected dependency id in drawer, got %q", depText)
	}
}
