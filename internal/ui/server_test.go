package ui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

func TestDetermineAccessLoopback(t *testing.T) {
	t.Parallel()

	requireAuth, err := ui.DetermineAccess("127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("DetermineAccess returned error: %v", err)
	}
	if requireAuth {
		t.Fatalf("expected loopback binding to skip auth requirement")
	}
}

func TestDetermineAccessRemoteWithoutAllow(t *testing.T) {
	t.Parallel()

	if _, err := ui.DetermineAccess("0.0.0.0:0", false); err == nil {
		t.Fatalf("expected remote binding to fail without allow-remote flag")
	}
}

func TestRemoteAuthEnforcement(t *testing.T) {
	t.Parallel()

	requireAuth, err := ui.DetermineAccess("0.0.0.0:0", true)
	if err != nil {
		t.Fatalf("DetermineAccess returned error: %v", err)
	}
	if !requireAuth {
		t.Fatalf("expected remote binding to require auth")
	}

	indexHTML, err := templates.RenderBasePage(templates.BasePageData{
		AppTitle:           "Beads",
		InitialFiltersJSON: mustDefaultFiltersJSON(t),
		EventStreamURL:     "/events",
		StaticPrefix:       "/.assets",
	})
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:    static.Files,
		IndexHTML:   indexHTML,
		RequireAuth: true,
		AuthToken:   "secret-token",
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz without auth: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // nolint:errcheck

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without Authorization header, got %d", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz with auth: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with Authorization header, got %d", resp.StatusCode)
	}
}

func TestStaticAssetsServeJavaScriptWithModuleMime(t *testing.T) {
	t.Parallel()

	indexHTML, err := templates.RenderBasePage(templates.BasePageData{
		AppTitle:           "Beads",
		InitialFiltersJSON: mustDefaultFiltersJSON(t),
		EventStreamURL:     "/events",
		StaticPrefix:       "/.assets",
	})
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/.assets/app.js")
	if err != nil {
		t.Fatalf("GET app.js: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.EqualFold(contentType, "text/javascript; charset=utf-8") {
		t.Fatalf("unexpected content type %q", contentType)
	}
}

func TestNewHandlerRequiresStaticFS(t *testing.T) {
	if _, err := ui.NewHandler(ui.HandlerConfig{}); err == nil || !strings.Contains(err.Error(), "StaticFS") {
		t.Fatalf("expected StaticFS error, got %v", err)
	}
}

func TestNewHandlerRequiresAuthTokenWhenEnabled(t *testing.T) {
	fs := fstest.MapFS{
		"index.html": {Data: []byte("<html></html>")},
	}

	_, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:    fs,
		RequireAuth: true,
		AuthToken:   "   ",
	})
	if err == nil || !strings.Contains(err.Error(), "auth token required") {
		t.Fatalf("expected auth token error, got %v", err)
	}
}

func TestNewHandlerIndexLoadFailure(t *testing.T) {
	fs := fstest.MapFS{}
	_, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  fs,
		IndexPath: "missing.html",
	})
	if err == nil || !strings.Contains(err.Error(), "load index template") {
		t.Fatalf("expected index load error, got %v", err)
	}
}

func TestDetermineAccessIPv6Loopback(t *testing.T) {
	requireAuth, err := ui.DetermineAccess("[::1]:0", false)
	if err != nil {
		t.Fatalf("DetermineAccess returned error: %v", err)
	}
	if requireAuth {
		t.Fatalf("expected IPv6 loopback to skip auth requirement")
	}
}

func TestDetermineAccessUnspecifiedHost(t *testing.T) {
	requireAuth, err := ui.DetermineAccess(":0", true)
	if err != nil {
		t.Fatalf("DetermineAccess returned error: %v", err)
	}
	if !requireAuth {
		t.Fatalf("expected unspecified host with allow remote to enforce auth")
	}
}
