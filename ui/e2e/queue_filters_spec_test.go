//go:build ui_e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"github.com/steveyegge/beads/internal/types"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

func TestIssueListRefreshRetainsCustomQueueFilters(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 24, 21, 0, 0, 0, time.UTC)

	client := &listClientStub{
		issues: []*types.Issue{
			{
				ID:        "ui-902",
				Title:     "Escalated follow-up",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  1,
				UpdatedAt: now.Add(-15 * time.Minute),
			},
		},
	}

	baseHTML := renderBasePage(t, "Beads UI")

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}

	fragmentHandler := api.NewListFragmentHandler(
		client,
		api.WithListFragmentTemplate(listTemplate),
		api.WithListFragmentClock(func() time.Time { return now }),
	)

	var (
		mu       sync.Mutex
		captured []url.Values
	)

	recordQuery := func(values url.Values) {
		clone := url.Values{}
		for key, vals := range values {
			dup := make([]string, len(vals))
			copy(dup, vals)
			clone[key] = dup
		}
		mu.Lock()
		captured = append(captured, clone)
		mu.Unlock()
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", api.NewListHandler(client))
			mux.Handle("/fragments/issues", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recordQuery(r.URL.Query())
				fragmentHandler.ServeHTTP(w, r)
			}))
			mux.Handle("/events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		t.Fatalf("expected status 200, got %d", status)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer waitCancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-role='issue-list']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	mu.Lock()
	captured = nil
	mu.Unlock()

	statusFilter := "blocked"
	selectedID := "ui-902"
	params := url.Values{}
	params.Set("status", statusFilter)
	params.Set("selected", selectedID)
	params.Add("labels", "customer/enterprise")
	params.Add("labels", "ops team")
	params.Set("q", "priority>1")
	params.Set("id_prefix", "ui")
	customURL := "/fragments/issues?" + params.Encode()

	_, err = h.Page().Evaluate(`async (url) => {
        const target = document.querySelector("[data-role='issue-list']");
        if (!target) {
            throw new Error("issue list target missing");
        }
        await new Promise((resolve, reject) => {
            let cleanedUp = false;
            const cleanup = () => {
                if (cleanedUp) {
                    return;
                }
                cleanedUp = true;
                document.body.removeEventListener("htmx:afterSwap", afterSwap);
                document.body.removeEventListener("htmx:sendError", sendError);
                if (req && typeof req.removeEventListener === "function") {
                    req.removeEventListener("loadend", onLoadEnd);
                    req.removeEventListener("error", onError);
                }
            };
            const onLoadEnd = () => {
                cleanup();
                resolve(true);
            };
            const onError = () => {
                cleanup();
                reject(new Error("htmx.ajax request failed"));
            };
            const afterSwap = (event) => {
                if (event && event.target === target) {
                    cleanup();
                    resolve(true);
                }
            };
            const sendError = () => {
                cleanup();
                reject(new Error("htmx ajax send error"));
            };
            document.body.addEventListener("htmx:afterSwap", afterSwap);
            document.body.addEventListener("htmx:sendError", sendError);
            const req = window.htmx.ajax("GET", url, { target, swap: "innerHTML" });
            if (!req) {
                cleanup();
                reject(new Error("htmx.ajax returned no request"));
                return;
            }
            if (typeof req.addEventListener === "function") {
                req.addEventListener("loadend", onLoadEnd);
                req.addEventListener("error", onError);
            } else if (typeof req.then === "function") {
                req.then(onLoadEnd).catch(onError);
            }
        });
    }`, customURL)
	if err != nil {
		t.Fatalf("dispatch htmx ajax: %v", err)
	}

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		_, err := h.Page().WaitForSelector("[data-issue-id='ui-902']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		return err
	})

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if len(captured) == 0 {
			return fmt.Errorf("no fragment requests recorded yet")
		}
		return nil
	})

	attr, err := h.Page().GetAttribute("[data-role='issue-list-rows']", "hx-get")
	if err != nil {
		t.Fatalf("read hx-get attribute: %v", err)
	}
	if attr == "" {
		t.Fatalf("expected hx-get attribute to be set")
	}

	expectedFragments := []string{
		"status=blocked",
		"labels=customer%2Fenterprise",
		"labels=ops+team",
		"q=priority%3E1",
		"id_prefix=ui",
		"selected=ui-902",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(attr, fragment) {
			t.Fatalf("hx-get missing fragment %q (value=%s)", fragment, attr)
		}
	}

	mu.Lock()
	captured = nil
	mu.Unlock()

	_, err = h.Page().Evaluate(`() => {
        document.body.dispatchEvent(new CustomEvent("events:update"));
    }`)
	if err != nil {
		t.Fatalf("dispatch events:update: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		count := len(captured)
		mu.Unlock()
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no refresh request observed")
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	lastQuery := captured[len(captured)-1]
	mu.Unlock()

	if got := lastQuery.Get("status"); got != statusFilter {
		t.Fatalf("expected status %q, got %q", statusFilter, got)
	}
	if got := lastQuery.Get("selected"); got != selectedID {
		t.Fatalf("expected selected %q, got %q", selectedID, got)
	}
	if got := strings.TrimSpace(lastQuery.Get("id_prefix")); got != "ui" {
		t.Fatalf("expected id_prefix to round-trip, got %q", got)
	}
	if got := strings.TrimSpace(lastQuery.Get("q")); got != "priority>1" {
		t.Fatalf("expected q to persist, got %q", got)
	}

	labels := lastQuery["labels"]
	if len(labels) != 2 {
		t.Fatalf("expected two labels, got %v", labels)
	}
	wantLabels := map[string]struct{}{
		"customer/enterprise": {},
		"ops team":            {},
	}
	for _, label := range labels {
		if _, ok := wantLabels[label]; !ok {
			t.Fatalf("unexpected label value %q", label)
		}
		delete(wantLabels, label)
	}
	if len(wantLabels) != 0 {
		t.Fatalf("missing labels after refresh: %v", wantLabels)
	}

	if got := lastQuery.Get("q"); got != "priority>1" {
		t.Fatalf("expected query filter to round-trip, got %q", got)
	}

	text, err := h.Page().TextContent("[data-issue-id='ui-902'] .ui-issue-row-title")
	if err != nil {
		t.Fatalf("read issue title: %v", err)
	}
	if strings.TrimSpace(text) != "Escalated follow-up" {
		t.Fatalf("expected refreshed title, got %q", text)
	}
}
