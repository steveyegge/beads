//go:build ui_e2e

package e2e

import (
	"context"
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
	"github.com/steveyegge/beads/internal/ui/search"
	"github.com/steveyegge/beads/ui/static"
)

type searchListClientStub struct {
	issues []*types.Issue
}

func (s *searchListClientStub) List(args *rpc.ListArgs) (*rpc.Response, error) {
	data, err := json.Marshal(s.issues)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

func TestCommandPaletteSearchAPIOrdering(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 23, 9, 30, 0, 0, time.UTC)
	stub := &searchListClientStub{
		issues: []*types.Issue{
			{
				ID:        "daemon-keeper",
				Title:     "Keep background daemon alive",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  1,
				UpdatedAt: now.Add(-5 * time.Minute),
			},
			{
				ID:        "ui-search",
				Title:     "Command palette enhances Daemon search",
				Status:    types.StatusInProgress,
				IssueType: types.TypeFeature,
				Priority:  2,
				UpdatedAt: now.Add(-10 * time.Minute),
			},
			{
				ID:          "docs-refresh",
				Title:       "Refresh docs for operators",
				Status:      types.StatusOpen,
				IssueType:   types.TypeTask,
				Priority:    2,
				Description: "Explain daemon handshake flow for new contributors.",
				UpdatedAt:   now.Add(-20 * time.Minute),
			},
		},
	}

	searchService := search.NewService(stub,
		search.WithClock(func() time.Time { return now }),
		search.WithFetchLimit(50),
	)

	baseHTML := renderBasePage(t, "Beads")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(stub))
			mux.Handle("/api/search", api.NewSearchHandler(searchService))
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
		_, err := h.Page().WaitForSelector("[data-testid='ui-title']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	payloadRaw, err := h.Page().Evaluate("(async () => await fetch('/api/search?q=daemon').then(res => res.json()))", nil)
	if err != nil {
		t.Fatalf("fetch search payload: %v", err)
	}

	payloadMap, ok := payloadRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected payload to be a map, got %T", payloadRaw)
	}

	resultsAny, ok := payloadMap["results"]
	if !ok {
		t.Fatalf("expected results field in payload")
	}
	resultsSlice, ok := resultsAny.([]any)
	if !ok {
		t.Fatalf("expected results to be array, got %T", resultsAny)
	}

	if len(resultsSlice) != 3 {
		t.Fatalf("expected 3 search results, got %d", len(resultsSlice))
	}

	first, ok := resultsSlice[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first result to be map, got %T", resultsSlice[0])
	}

	if id, _ := first["id"].(string); id != "daemon-keeper" {
		t.Fatalf("expected first result id daemon-keeper, got %v", first["id"])
	}
}
