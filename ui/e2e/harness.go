//go:build ui_e2e

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	playwright "github.com/playwright-community/playwright-go"
)

// ServerHarness wraps an httptest.Server together with a headless Playwright page.
type ServerHarness struct {
	t          testing.TB
	server     *httptest.Server
	baseURL    string
	shutdown   func()
	pw         *playwright.Playwright
	browser    playwright.Browser
	browserCtx playwright.BrowserContext
	page       playwright.Page
}

// HarnessConfig controls the browser mode for a test.
type HarnessConfig struct {
	Headless          bool
	IgnoreHTTPSErrors bool
}

func newPlaywrightSession(t testing.TB, cfg HarnessConfig) (*playwright.Playwright, playwright.Browser, playwright.BrowserContext, playwright.Page) {
	if err := playwright.Install(&playwright.RunOptions{
		Browsers: []string{"chromium"},
	}); err != nil {
		t.Fatalf("install Playwright: %v", err)
	}

	headless := cfg.Headless
	if override := os.Getenv("BD_E2E_HEADLESS"); override != "" {
		if parsed, err := strconv.ParseBool(override); err == nil {
			headless = parsed
		}
	}

	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("start Playwright: %v", err)
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headless),
	})
	if err != nil {
		pw.Stop()
		t.Fatalf("launch chromium (headless=%v): %v", headless, err)
	}

	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		IgnoreHttpsErrors: playwright.Bool(cfg.IgnoreHTTPSErrors),
	})
	if err != nil {
		browser.Close()
		pw.Stop()
		t.Fatalf("create browser context: %v", err)
	}

	page, err := ctx.NewPage()
	if err != nil {
		ctx.Close()
		browser.Close()
		pw.Stop()
		t.Fatalf("create page: %v", err)
	}

	return pw, browser, ctx, page
}

// NewServerHarness installs Playwright (if necessary), starts a browser,
// and serves the provided handler from an httptest server.
func NewServerHarness(t testing.TB, handler http.Handler, cfg HarnessConfig) *ServerHarness {
	t.Helper()

	pw, browser, ctx, page := newPlaywrightSession(t, cfg)

	server := httptest.NewServer(handler)

	h := &ServerHarness{
		t:          t,
		server:     server,
		baseURL:    server.URL,
		pw:         pw,
		browser:    browser,
		browserCtx: ctx,
		page:       page,
	}

	t.Cleanup(h.Close)

	return h
}

// NewRemoteHarness connects Playwright to an externally managed server.
// The shutdown function will be invoked during cleanup (if non-nil).
func NewRemoteHarness(t testing.TB, baseURL string, shutdown func(), cfg HarnessConfig) *ServerHarness {
	t.Helper()

	pw, browser, ctx, page := newPlaywrightSession(t, cfg)

	h := &ServerHarness{
		t:          t,
		baseURL:    baseURL,
		shutdown:   shutdown,
		pw:         pw,
		browser:    browser,
		browserCtx: ctx,
		page:       page,
	}

	t.Cleanup(h.Close)
	return h
}

// Close shuts down browser and server resources; safe to call multiple times.
func (h *ServerHarness) Close() {
	if h.page != nil {
		_ = h.page.Close()
		h.page = nil
	}
	if h.browserCtx != nil {
		_ = h.browserCtx.Close()
		h.browserCtx = nil
	}
	if h.browser != nil {
		_ = h.browser.Close()
		h.browser = nil
	}
	if h.pw != nil {
		_ = h.pw.Stop()
		h.pw = nil
	}
	if h.server != nil {
		h.server.Close()
		h.server = nil
	}
	if h.shutdown != nil {
		h.shutdown()
		h.shutdown = nil
	}
}

// BaseURL returns the server root.
func (h *ServerHarness) BaseURL() string {
	return h.baseURL
}

// Page exposes the Playwright page for assertions.
func (h *ServerHarness) Page() playwright.Page {
	return h.page
}

// MustNavigate loads the given relative path and waits for DOM content loaded.
func (h *ServerHarness) MustNavigate(path string, opts ...playwright.PageGotoOptions) playwright.Response {
	h.t.Helper()

	if len(opts) == 0 {
		opts = append(opts, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
	}

	resp, err := h.page.Goto(h.BaseURL()+path, opts...)
	if err != nil {
		h.t.Fatalf("navigate to %s: %v", path, err)
	}
	if resp == nil {
		h.t.Fatalf("expected response for %s", path)
	}
	return resp
}

// MustWaitUntil runs the provided wait loop and fails the test on error.
func (h *ServerHarness) MustWaitUntil(ctx context.Context, fn func(context.Context) error) {
	h.t.Helper()
	if err := fn(ctx); err != nil {
		h.t.Fatalf("wait condition failed: %v", err)
	}
}
