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

type bulkListClient struct{}

func (c *bulkListClient) List(*rpc.ListArgs) (*rpc.Response, error) {
	data, _ := json.Marshal([]*types.Issue{})
	return &rpc.Response{Success: true, Data: data}, nil
}

type bulkDetailClient struct{}

func (c *bulkDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	if args == nil {
		return &rpc.Response{Success: false, Error: "invalid issue id"}, nil
	}
	payload := struct {
		*types.Issue
	}{
		Issue: &types.Issue{
			ID:        args.ID,
			Title:     "Bulk detail",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  2,
			UpdatedAt: time.Now().UTC(),
		},
	}
	data, _ := json.Marshal(payload)
	return &rpc.Response{Success: true, Data: data}, nil
}

type bulkBatchClient struct {
	args *rpc.BatchArgs
}

func (c *bulkBatchClient) Batch(args *rpc.BatchArgs) (*rpc.Response, error) {
	c.args = args
	now := time.Date(2025, 10, 23, 18, 0, 0, 0, time.UTC)
	records := rpc.BatchResponse{
		Results: []rpc.BatchResult{
			{
				Success: true,
				Data: mustJSON(struct {
					Issue *types.Issue `json:"issue"`
				}{
					Issue: &types.Issue{
						ID:        "ui-900",
						Title:     "First bulk result",
						Status:    types.StatusClosed,
						IssueType: types.TypeFeature,
						Priority:  1,
						UpdatedAt: now,
					},
				}),
			},
			{
				Success: true,
				Data: mustJSON(struct {
					Issue *types.Issue `json:"issue"`
				}{
					Issue: &types.Issue{
						ID:        "ui-901",
						Title:     "Second bulk result",
						Status:    types.StatusClosed,
						IssueType: types.TypeTask,
						Priority:  1,
						UpdatedAt: now.Add(2 * time.Minute),
					},
				}),
			},
		},
	}
	return &rpc.Response{
		Success: true,
		Data:    mustJSON(records),
	}, nil
}

func TestBulkUpdateAPI(t *testing.T) {
	t.Parallel()

	baseHTML := renderBasePage(t, "Bulk Update Harness")

	listClient := &bulkListClient{}
	detailClient := &bulkDetailClient{}
	batchClient := &bulkBatchClient{}
	renderer := api.NewMarkdownRenderer()

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(listClient))
			mux.Handle("/api/issues/", api.NewIssueHandler(detailClient, renderer, nil))
			mux.Handle("/api/issues/bulk", api.NewBulkHandler(batchClient, nil))
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
		const response = await fetch('/api/issues/bulk', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				ids: ['ui-900', 'ui-901'],
				action: { status: 'closed', priority: 1 }
			})
		});
		const json = await response.json();
		return { status: response.status, body: json };
	})()`, nil)
	if err != nil {
		t.Fatalf("invoke bulk update: %v", err)
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
		t.Fatalf("expected HTTP 200, got %v", statusValue)
	}

	body, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body map, got %T", result["body"])
	}
	items, ok := body["results"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 results, got %#v", body["results"])
	}

	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first result map, got %T", items[0])
	}
	if successVal, _ := first["success"].(bool); !successVal {
		t.Fatalf("expected first result success, got %+v", first)
	}
	issue, ok := first["issue"].(map[string]any)
	if !ok {
		t.Fatalf("expected issue object, got %T", first["issue"])
	}
	if issue["id"] != "ui-900" || issue["status"] != string(types.StatusClosed) {
		t.Fatalf("unexpected first issue payload: %#v", issue)
	}

	if batchClient.args == nil || len(batchClient.args.Operations) != 2 {
		t.Fatalf("expected two operations recorded, got %+v", batchClient.args)
	}

	var update rpc.UpdateArgs
	if err := json.Unmarshal(batchClient.args.Operations[0].Args, &update); err != nil {
		t.Fatalf("decode first operation: %v", err)
	}
	if update.Status == nil || *update.Status != "closed" {
		t.Fatalf("expected status closed, got %#v", update.Status)
	}
	if update.Priority == nil || *update.Priority != 1 {
		t.Fatalf("expected priority 1, got %#v", update.Priority)
	}
}

func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
