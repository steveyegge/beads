package ui_test

import (
	"bytes"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/ui/static"
)

func TestUIServerTLS(t *testing.T) {
	t.Parallel()

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: []byte("<html><body>TLS OK</body></html>"),
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	certPEM, keyPEM, err := ui.GenerateSelfSignedCertificate([]string{"127.0.0.1", "localhost"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("generate certificate: %v", err)
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("parse key pair: %v", err)
	}

	server := &http.Server{Handler: handler}
	go func() {
		cfg := &tls.Config{Certificates: []tls.Certificate{tlsCert}}
		tlsListener := tls.NewListener(listener, cfg)
		_ = server.Serve(tlsListener)
	}()
	defer server.Close()

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // nolint:gosec
		},
	}

	httpsURL := "https://" + listener.Addr().String() + "/healthz"
	resp, err := client.Get(httpsURL)
	if err != nil {
		t.Fatalf("GET %s: %v", httpsURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 over HTTPS, got %d", resp.StatusCode)
	}

	// Plain HTTP against the TLS listener should fail.
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial plain HTTP: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: example\r\n\r\n")); err != nil {
		t.Fatalf("write plain HTTP request: %v", err)
	}

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err == nil {
		if !bytes.HasPrefix(buf[:n], []byte("HTTP/")) {
			t.Fatalf("expected HTTP error response, got %q", buf[:n])
		}
		payload := string(buf[:n])
		if !strings.Contains(payload, "400 Bad Request") {
			t.Fatalf("expected HTTP 400 for plaintext request, got %q", payload)
		}
	}
}

func TestGenerateSelfSignedCertificateIncludesDefaults(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM, err := ui.GenerateSelfSignedCertificate(nil, 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCertificate: %v", err)
	}

	if len(certPEM) == 0 || len(keyPEM) == 0 {
		t.Fatalf("expected non-empty certificate and key")
	}

	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
}
