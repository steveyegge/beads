//go:build ui_e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

type labelUIListClient struct {
	issues []*types.Issue
}

func (c *labelUIListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	data, _ := json.Marshal(c.issues)
	return &rpc.Response{Success: true, Data: data}, nil
}

type labelUIDetailClient struct {
	payload func(id string) *types.Issue
}

func (c *labelUIDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	issue := c.payload(args.ID)
	data, _ := json.Marshal(struct {
		*types.Issue
		Labels []string `json:"labels,omitempty"`
	}{
		Issue:  issue,
		Labels: issue.Labels,
	})
	return &rpc.Response{Success: true, Data: data}, nil
}

type labelUIMutator struct {
	state *[]string
}

func (m *labelUIMutator) AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error) {
	if args != nil && args.Label != "" && m.state != nil {
		exists := false
		for _, label := range *m.state {
			if label == args.Label {
				exists = true
				break
			}
		}
		if !exists {
			*m.state = append(*m.state, args.Label)
		}
	}
	return &rpc.Response{Success: true}, nil
}

func (m *labelUIMutator) RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error) {
	if args != nil && args.Label != "" && m.state != nil {
		next := (*m.state)[:0]
		for _, label := range *m.state {
			if label == args.Label {
				continue
			}
			next = append(next, label)
		}
		*m.state = next
	}
	return &rpc.Response{Success: true}, nil
}

func TestLabelEditingUI(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 18, 0, 0, 0, time.UTC)
	labelState := []string{"ui"}

	listClient := &labelUIListClient{issues: []*types.Issue{
		{
			ID:        "ui-800",
			Title:     "Label UI Harness",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  2,
			Labels:    append([]string(nil), labelState...),
			UpdatedAt: now,
		},
	}}

	detailClient := &labelUIDetailClient{
		payload: func(id string) *types.Issue {
			return &types.Issue{
				ID:        id,
				Title:     "Label UI Harness",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  2,
				Labels:    append([]string(nil), labelState...),
				UpdatedAt: now,
			}
		},
	}

	mutator := &labelUIMutator{state: &labelState}

	baseHTML := renderBasePage(t, "Label UI Harness")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}
	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse detail template: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewIssueHandler(
				detailClient,
				api.NewMarkdownRenderer(),
				nil,
				api.WithLabelClient(mutator),
			))
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
		t.Fatalf("expected 200, got %d", status)
	}

	if _, err := h.Page().WaitForSelector("[data-role='issue-row']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for issue rows: %v", err)
	}

	if err := h.Page().Click("[data-issue-id='ui-800']"); err != nil {
		t.Fatalf("click issue row: %v", err)
	}

	if _, err := h.Page().WaitForSelector("[data-testid='label-editor']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for label editor: %v", err)
	}

	if _, err := h.Page().WaitForSelector("[data-role='label-chip'][data-label-value='ui']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for initial label chip: %v", err)
	}

	if err := h.Page().Fill("[data-testid='label-input']", "ops"); err != nil {
		t.Fatalf("fill label input: %v", err)
	}

	if err := h.Page().Click("[data-testid='label-submit']"); err != nil {
		t.Fatalf("submit new label: %v", err)
	}

	if _, err := h.Page().WaitForSelector("[data-role='label-chip'][data-label-value='ops']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for ops label chip: %v", err)
	}

	if err := h.Page().Click("[data-role='remove-label'][data-label-value='ui']"); err != nil {
		t.Fatalf("remove ui label: %v", err)
	}

	if _, err := h.Page().WaitForSelector("[data-role='label-chip'][data-label-value='ui']", playwright.PageWaitForSelectorOptions{
		State:   playwright.WaitForSelectorStateDetached,
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for ui label removal: %v", err)
	}

	if _, err := h.Page().Evaluate(`() => {
		const event = new CustomEvent("events:update", { detail: { issueId: "ui-800" } });
		document.body.dispatchEvent(event);
	}`); err != nil {
		t.Fatalf("dispatch events:update: %v", err)
	}

	if _, err := h.Page().WaitForFunction(`() => {
		const chips = Array.from(document.querySelectorAll("[data-role='label-chip']"));
		return chips.length === 1 && chips[0].dataset.labelValue === "ops";
	}`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for label persistence after SSE: %v", err)
	}

	value, err := h.Page().Evaluate(`() => window.localStorage.getItem("beads.ui.labels.recent")`)
	if err != nil {
		t.Fatalf("read localStorage: %v", err)
	}
	if value == nil {
		t.Fatalf("expected localStorage entry for recent labels")
	}
}
