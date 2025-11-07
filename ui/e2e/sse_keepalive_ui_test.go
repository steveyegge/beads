//go:build ui_e2e

package e2e

import (
	"context"
	"net/http"
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

type keepaliveEventSource struct {
	subscribed chan struct{}
}

func newKeepaliveEventSource() *keepaliveEventSource {
	return &keepaliveEventSource{
		subscribed: make(chan struct{}),
	}
}

func (s *keepaliveEventSource) Subscribe(ctx context.Context) (<-chan api.IssueEvent, error) {
	ch := make(chan api.IssueEvent)

	go func() {
		close(s.subscribed)
		<-ctx.Done()
	}()

	return ch, nil
}

func TestEventStreamNoConsoleErrorsDuringKeepalive(t *testing.T) {
	t.Parallel()

	const (
		scaledSecond      = 40 * time.Millisecond
		heartbeatInterval = 30 * scaledSecond
		waitDuration      = 65 * scaledSecond
	)

	store := &liveIssueStore{}
	now := time.Date(2025, 10, 24, 18, 0, 0, 0, time.UTC)

	store.Set(&types.Issue{
		ID:        "ui-905",
		Title:     "Keepalive smoke issue",
		Status:    types.StatusOpen,
		IssueType: types.TypeFeature,
		Priority:  2,
		UpdatedAt: now,
	})

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
	source := newKeepaliveEventSource()

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
			mux.Handle("/events", api.NewEventStreamHandler(
				source,
				api.WithHeartbeatInterval(heartbeatInterval),
			))
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("unexpected status %d", status)
	}

	page := h.Page()

	var (
		consoleMu  sync.Mutex
		consoleLog []string
	)

	page.On("console", func(msg playwright.ConsoleMessage) {
		consoleMu.Lock()
		consoleLog = append(consoleLog, msg.Text())
		consoleMu.Unlock()
	})

	select {
	case <-source.subscribed:
	case <-time.After(2 * scaledSecond):
		consoleMu.Lock()
		logs := append([]string(nil), consoleLog...)
		consoleMu.Unlock()
		t.Fatalf("timed out waiting for SSE subscription (console=%v)", logs)
	}

	if _, err := page.WaitForFunction(`() => {
      const api = window.bdEventStream && window.bdEventStream.get && window.bdEventStream.get();
      if (!api) return false;
      return api.state && api.state() === "open";
    }`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("event stream did not open: %v", err)
	}

	page.WaitForTimeout(float64(waitDuration.Milliseconds()))

	result, err := page.Evaluate(`() => {
      const api = window.bdEventStream && window.bdEventStream.get && window.bdEventStream.get();
      if (!api || !api.isConnected()) return false;
      const last = api.getLastHeartbeat && api.getLastHeartbeat();
      if (!last) return false;
      return Date.now() - last < 2_000;
    }`)
	if err != nil {
		t.Fatalf("event stream lost heartbeat: %v", err)
	}

	isHealthy, ok := result.(bool)
	if !ok || !isHealthy {
		t.Fatalf("event stream heartbeat check returned %v", result)
	}

	consoleMu.Lock()
	defer consoleMu.Unlock()

	disallowed := []string{
		"event_stream:error",
		"net::err_incomplete_chunked_encoding",
	}
	for _, entry := range consoleLog {
		lower := strings.ToLower(entry)
		for _, target := range disallowed {
			if strings.Contains(lower, target) {
				t.Fatalf("unexpected console message %q", entry)
			}
		}
	}
}

func TestLiveUpdateBannerRecoversAfterLateSSE(t *testing.T) {
	t.Parallel()

	const (
		scaledSecond      = 40 * time.Millisecond
		heartbeatInterval = 200 * time.Millisecond
	)

	store := &liveIssueStore{}
	now := time.Date(2025, 10, 24, 19, 0, 0, 0, time.UTC)

	store.Set(&types.Issue{
		ID:        "ui-906",
		Title:     "Delayed daemon availability",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		UpdatedAt: now,
	})

	baseHTML := renderBasePage(t, "Beads UI", WithoutEventStream())

	listTemplate, err := templates.Parse("issue_list.tmpl")
	if err != nil {
		t.Fatalf("parse list template: %v", err)
	}
	detailTemplate, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse detail template: %v", err)
	}

	renderer := api.NewMarkdownRenderer()
	source := newKeepaliveEventSource()

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
			mux.Handle("/events", api.NewEventStreamHandler(
				source,
				api.WithHeartbeatInterval(heartbeatInterval),
			))
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != http.StatusOK {
		t.Fatalf("unexpected status %d", status)
	}

	page := h.Page()

	select {
	case <-source.subscribed:
	case <-time.After(10 * scaledSecond):
		t.Fatalf("timed out waiting for SSE subscription")
	}

	if _, err := page.WaitForFunction(`() => {
      const banner = document.querySelector("[data-role='live-update-warning']");
      if (!banner) return false;
      return banner.hidden === true && banner.dataset.state === "hidden";
    }`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("live update banner did not hide after SSE became available: %v", err)
	}

	if _, err := page.WaitForFunction(`() => {
      const body = document.body;
      if (!body) return false;
      return body.dataset.liveUpdates === "on";
    }`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("body live update state did not switch to on: %v", err)
	}

}
