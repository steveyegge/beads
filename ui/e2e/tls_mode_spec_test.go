//go:build ui_e2e

package e2e

import (
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/ui/static"
)

func TestUIServerTLSMode(t *testing.T) {
	t.Parallel()

	baseHTML := renderBasePage(t, "TLS Harness")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: baseHTML,
		Register: func(mux *http.ServeMux) {
			mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	certPEM, keyPEM, err := ui.GenerateSelfSignedCertificate([]string{"127.0.0.1", "localhost"}, 24*time.Hour)
	if err != nil {
		listener.Close()
		t.Fatalf("GenerateSelfSignedCertificate: %v", err)
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		listener.Close()
		t.Fatalf("X509KeyPair: %v", err)
	}

	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		cfg := &tls.Config{Certificates: []tls.Certificate{tlsCert}}
		_ = server.Serve(tls.NewListener(listener, cfg))
		close(done)
	}()

	shutdown := func() {
		server.Close()
		<-done
	}

	baseURL := "https://" + listener.Addr().String()
	h := NewRemoteHarness(t, baseURL, shutdown, HarnessConfig{Headless: true, IgnoreHTTPSErrors: true})

	resp := h.MustNavigate("/")
	if resp.Status() != http.StatusOK {
		t.Fatalf("GET / expected 200, got %d", resp.Status())
	}

	page := h.Page()
	if _, err := page.WaitForSelector("[data-testid='ui-title']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(4000),
	}); err != nil {
		t.Fatalf("wait for UI title: %v", err)
	}
}
