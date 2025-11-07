package ui

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"strings"
)

func init() {
	// Ensure JavaScript and CSS assets are served with explicit MIME types even on
	// platforms that default to text/plain, which breaks ES module loading.
	_ = mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".mjs", "text/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
}

// DetermineAccess inspects the requested listen address and returns whether
// authentication is required (i.e., binding to a non-loopback/unspecified host).
// It rejects remote bindings unless allowRemote is explicitly enabled.
func DetermineAccess(listenAddr string, allowRemote bool) (bool, error) {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false, fmt.Errorf("invalid listen address %q: %w", listenAddr, err)
	}

	normalizedHost := host
	if normalizedHost == "" {
		normalizedHost = "0.0.0.0"
	}

	if isLoopbackHost(normalizedHost) {
		return false, nil
	}

	if !allowRemote {
		return false, fmt.Errorf("refusing remote bind to %q without --allow-remote", normalizedHost)
	}

	return true, nil
}

// HandlerConfig captures the inputs required to build the UI HTTP handler.
type HandlerConfig struct {
	StaticFS    fs.FS
	IndexPath   string
	IndexHTML   []byte
	RequireAuth bool
	AuthToken   string
	Register    func(*http.ServeMux)
}

// NewHandler constructs the HTTP handler for the UI server using the provided configuration.
func NewHandler(cfg HandlerConfig) (http.Handler, error) {
	if cfg.StaticFS == nil {
		return nil, errors.New("StaticFS is required")
	}

	var indexHTML []byte
	if len(cfg.IndexHTML) > 0 {
		indexHTML = append([]byte(nil), cfg.IndexHTML...)
	} else {
		indexPath := cfg.IndexPath
		if indexPath == "" {
			indexPath = "index.html"
		}

		var err error
		indexHTML, err = fs.ReadFile(cfg.StaticFS, indexPath)
		if err != nil {
			return nil, fmt.Errorf("load index template: %w", err)
		}
	}

	if cfg.RequireAuth && strings.TrimSpace(cfg.AuthToken) == "" {
		return nil, errors.New("auth token required when authentication is enabled")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/.assets/", http.StripPrefix("/.assets/", assetHandler(cfg.StaticFS)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexHTML)
	})

	if cfg.Register != nil {
		cfg.Register(mux)
	}

	if !cfg.RequireAuth {
		return mux, nil
	}

	expectedHeader := "Bearer " + strings.TrimSpace(cfg.AuthToken)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := strings.TrimSpace(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare([]byte(actual), []byte(expectedHeader)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="bd-ui"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	resp := map[string]string{"status": "ok"}
	enc := json.NewEncoder(w)
	enc.Encode(resp) // nolint:errchkjson
}

func assetHandler(staticFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, r)
	})
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return true
		}
		if ip.IsUnspecified() {
			return false
		}
		return false
	}

	return false
}
