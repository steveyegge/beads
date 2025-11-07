//go:build ui_e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type labelsListClient struct {
	issues []*types.Issue
}

func (c *labelsListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	data, _ := json.Marshal(c.issues)
	return &rpc.Response{Success: true, Data: data}, nil
}

type labelsDetailClient struct {
	payload func(id string) *types.Issue
}

func (c *labelsDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
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

type labelsMutator struct {
	addCalls    []*rpc.LabelAddArgs
	removeCalls []*rpc.LabelRemoveArgs
	state       *[]string
}

func (m *labelsMutator) AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error) {
	m.addCalls = append(m.addCalls, args)
	if m.state != nil && args != nil {
		label := args.Label
		found := false
		for _, existing := range *m.state {
			if existing == label {
				found = true
				break
			}
		}
		if !found {
			*m.state = append(*m.state, label)
		}
	}
	return &rpc.Response{Success: true}, nil
}

func (m *labelsMutator) RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error) {
	m.removeCalls = append(m.removeCalls, args)
	if m.state != nil && args != nil {
		label := args.Label
		next := (*m.state)[:0]
		for _, existing := range *m.state {
			if existing == label {
				continue
			}
			next = append(next, existing)
		}
		*m.state = next
	}
	return &rpc.Response{Success: true}, nil
}

type noopUpdateClient struct{}

func (noopUpdateClient) Update(*rpc.UpdateArgs) (*rpc.Response, error) {
	return &rpc.Response{Success: true}, nil
}

func coerceStatus(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	default:
		return 0
	}
}

func TestLabelMutationEndpoints(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 15, 0, 0, 0, time.UTC)
	labelState := []string{"ui"}

	listClient := &labelsListClient{issues: []*types.Issue{
		{
			ID:        "ui-700",
			Title:     "Label API Test",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  2,
			Labels:    append([]string(nil), labelState...),
			UpdatedAt: now,
		},
	}}

	detailClient := &labelsDetailClient{
		payload: func(id string) *types.Issue {
			return &types.Issue{
				ID:        id,
				Title:     "Label API Test",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  2,
				Labels:    append([]string(nil), labelState...),
				UpdatedAt: now,
			}
		},
	}

	mutator := &labelsMutator{state: &labelState}

	baseHTML := renderBasePage(t, "Label API Harness")

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
				noopUpdateClient{},
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

	rawAdd, err := h.Page().Evaluate(`(async () => {
		const response = await fetch('/api/issues/ui-700/labels', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ label: 'blocked' })
		});
		const json = await response.json();
		return { status: response.status, body: json };
	})()`, nil)
	if err != nil {
		t.Fatalf("invoke add label: %v", err)
	}

	resultAdd := rawAdd.(map[string]any)
	if coerceStatus(resultAdd["status"]) != http.StatusOK {
		t.Fatalf("expected add status 200, got %#v", resultAdd)
	}
	bodyAdd := resultAdd["body"].(map[string]any)
	labelsAdd := bodyAdd["labels"].([]any)
	if len(labelsAdd) != 2 {
		t.Fatalf("expected 2 labels after add, got %#v", labelsAdd)
	}

	rawRemove, err := h.Page().Evaluate(`(async () => {
		const response = await fetch('/api/issues/ui-700/labels', {
			method: 'DELETE',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ label: 'ui' })
		});
		const json = await response.json();
		return { status: response.status, body: json };
	})()`, nil)
	if err != nil {
		t.Fatalf("invoke remove label: %v", err)
	}

	resultRemove := rawRemove.(map[string]any)
	if coerceStatus(resultRemove["status"]) != http.StatusOK {
		t.Fatalf("expected remove status 200, got %#v", resultRemove)
	}
	bodyRemove := resultRemove["body"].(map[string]any)
	labelsRemove := bodyRemove["labels"].([]any)
	if len(labelsRemove) != 1 || labelsRemove[0].(string) != "blocked" {
		t.Fatalf("unexpected labels after removal: %#v", labelsRemove)
	}

	if len(mutator.addCalls) != 1 || mutator.addCalls[0].Label != "blocked" {
		t.Fatalf("expected add call for blocked, got %#v", mutator.addCalls)
	}
	if len(mutator.removeCalls) != 1 || mutator.removeCalls[0].Label != "ui" {
		t.Fatalf("expected remove call for ui, got %#v", mutator.removeCalls)
	}
}
