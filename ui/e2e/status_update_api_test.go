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
	"github.com/steveyegge/beads/ui/static"
)

type statusListClient struct{}

func (c *statusListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	data, _ := json.Marshal([]*types.Issue{})
	return &rpc.Response{Success: true, Data: data}, nil
}

type statusDetailClient struct{}

func (c *statusDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	if args == nil {
		return &rpc.Response{Success: false, Error: "invalid issue id"}, nil
	}
	payload := struct {
		*types.Issue
	}{
		Issue: &types.Issue{
			ID:        args.ID,
			Title:     "Placeholder detail",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  2,
			UpdatedAt: time.Now().UTC(),
		},
	}
	data, _ := json.Marshal(payload)
	return &rpc.Response{Success: true, Data: data}, nil
}

type statusUpdateClient struct {
	lastArgs *rpc.UpdateArgs
}

func (c *statusUpdateClient) Update(args *rpc.UpdateArgs) (*rpc.Response, error) {
	c.lastArgs = args

	issue := &types.Issue{
		ID:        args.ID,
		Title:     "Placeholder detail",
		Status:    types.StatusInProgress,
		IssueType: types.TypeTask,
		Priority:  2,
		UpdatedAt: time.Date(2025, 10, 23, 12, 0, 0, 0, time.UTC),
	}

	data, _ := json.Marshal(issue)
	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

func TestStatusUpdateAPI(t *testing.T) {
	t.Parallel()

	baseHTML := renderBasePage(t, "Status Update Test")

	listClient := &statusListClient{}
	detailClient := &statusDetailClient{}
	updateClient := &statusUpdateClient{}
	renderer := api.NewMarkdownRenderer()

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewIssueHandler(detailClient, renderer, updateClient))
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
	if resp.Status() != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Status())
	}

	rawResult, err := h.Page().Evaluate(`(async () => {
		const response = await fetch('/api/issues/ui-777/status', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ status: 'in_progress' })
		});
		const json = await response.json();
		return { status: response.status, ok: response.ok, body: json };
	})()`, nil)
	if err != nil {
		t.Fatalf("invoke status update: %v", err)
	}

	result, ok := rawResult.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", rawResult)
	}

	statusValue := 0
	switch v := result["status"].(type) {
	case float64:
		statusValue = int(v)
	case int:
		statusValue = v
	case int32:
		statusValue = int(v)
	case int64:
		statusValue = int(v)
	default:
		t.Fatalf("unexpected status type %T", result["status"])
	}
	if statusValue != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %v", result["status"])
	}
	body, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body map, got %T", result["body"])
	}
	issue, ok := body["issue"].(map[string]any)
	if !ok {
		t.Fatalf("expected issue map, got %T", body["issue"])
	}
	if issue["id"] != "ui-777" || issue["status"] != string(types.StatusInProgress) {
		t.Fatalf("unexpected issue payload: %#v", issue)
	}

	if updateClient.lastArgs == nil {
		t.Fatalf("expected update client invocation")
	}
	if updateClient.lastArgs.ID != "ui-777" {
		t.Fatalf("expected update ID ui-777, got %s", updateClient.lastArgs.ID)
	}
	if updateClient.lastArgs.Status == nil || *updateClient.lastArgs.Status != "in_progress" {
		t.Fatalf("expected status pointer in_progress, got %#v", updateClient.lastArgs.Status)
	}
}
