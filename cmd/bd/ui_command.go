package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/shlex"
	"github.com/spf13/cobra"
	uiserver "github.com/steveyegge/beads/internal/ui"
	uiapi "github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

var (
	uiListenAddr  string
	uiNoOpen      bool
	uiOpenCmd     string
	uiAllowRemote bool
	uiAuthToken   string
	uiTLSCertPath string
	uiTLSKeyPath  string
	uiTLSSelfSign bool
)

var issueDetailTemplate = template.Must(templates.Parse("issue_detail.tmpl"))
var issueListTemplate = template.Must(templates.Parse("issue_list.tmpl"))

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Launch the lightweight Beads UI preview",
	Long: `Spin up a local HTTP server that hosts the experimental Beads UI shell.

The server binds to a loopback interface by default and opens your browser unless
disabled via --no-open.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUICommand(cmd)
	},
}

func init() {
	uiCmd.Flags().StringVar(&uiListenAddr, "listen", "127.0.0.1:0", "Address to bind the UI server to (host:port)")
	uiCmd.Flags().BoolVar(&uiNoOpen, "no-open", false, "Do not automatically launch a browser window")
	uiCmd.Flags().StringVar(&uiOpenCmd, "open-command", "", "Custom browser launch command (overrides platform default)")
	uiCmd.Flags().BoolVar(&uiAllowRemote, "allow-remote", false, "Permit binding to non-loopback addresses (requires auth token)")
	uiCmd.Flags().StringVar(&uiAuthToken, "auth-token", "", "Use the provided auth token instead of generating one")
	uiCmd.Flags().StringVar(&uiTLSCertPath, "tls-cert", "", "Path to PEM-encoded TLS certificate")
	uiCmd.Flags().StringVar(&uiTLSKeyPath, "tls-key", "", "Path to PEM-encoded TLS private key")
	uiCmd.Flags().BoolVar(&uiTLSSelfSign, "tls-self-signed", false, "Generate and use a self-signed TLS certificate stored under ~/.beads")

	rootCmd.AddCommand(uiCmd)
}

func runUICommand(cmd *cobra.Command) error {
	ctx := cmd.Context()

	listenAddr := uiListenAddr
	if strings.TrimSpace(listenAddr) == "" {
		listenAddr = "127.0.0.1:0"
	}

	requireRemoteAuth, err := uiserver.DetermineAccess(listenAddr, uiAllowRemote)
	if err != nil {
		return err
	}

	token := strings.TrimSpace(uiAuthToken)
	requireAuth := requireRemoteAuth || token != ""

	if requireAuth && token == "" {
		token, err = generateUIAuthToken()
		if err != nil {
			return fmt.Errorf("generate auth token: %w", err)
		}
	}

	useTLS := false
	certPath := strings.TrimSpace(uiTLSCertPath)
	keyPath := strings.TrimSpace(uiTLSKeyPath)

	if uiTLSSelfSign {
		if certPath != "" || keyPath != "" {
			return errors.New("--tls-self-signed cannot be combined with --tls-cert/--tls-key")
		}
		certPath, keyPath, err = ensureSelfSignedCertificate(listenAddr, cmd.OutOrStdout())
		if err != nil {
			return fmt.Errorf("generate self-signed certificate: %w", err)
		}
		useTLS = true
	} else if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return errors.New("both --tls-cert and --tls-key must be provided")
		}
		useTLS = true
	}

	if useTLS {
		if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
			return fmt.Errorf("load TLS certificate/key: %w", err)
		}
	}

	globalUISessionManager.SetAuthToken(token)

	session, err := globalUISessionManager.Open(ctx, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	listClient := session.ListClient
	createClient := session.CreateClient
	detailClient := session.DetailClient
	updateClient := session.UpdateClient
	labelClient := session.LabelClient
	deleteClient := session.DeleteClient
	bulkClient := session.BulkClient
	eventSource := session.EventSource
	searchService := session.SearchService

	renderer := uiapi.NewMarkdownRenderer()

	initialFilters := map[string]any{
		"query":         "",
		"status":        "open",
		"issueType":     "",
		"priority":      "",
		"assignee":      "",
		"labelsAll":     []string{},
		"labelsAny":     []string{},
		"prefix":        "",
		"numberMin":     "",
		"numberMax":     "",
		"sortPrimary":   "number-desc",
		"sortSecondary": "none",
	}
	filtersJSON, err := json.Marshal(initialFilters)
	if err != nil {
		return fmt.Errorf("marshal initial filters: %w", err)
	}

	pageData := templates.BasePageData{
		AppTitle:           "Beads",
		StaticPrefix:       "/.assets",
		InitialFiltersJSON: template.JS(filtersJSON),
	}
	if eventSource == nil {
		pageData.DisableEventStream = true
	} else {
		pageData.EventStreamURL = "/events"
	}

	indexHTML, err := templates.RenderBasePage(pageData)
	if err != nil {
		return fmt.Errorf("render base template: %w", err)
	}

	handlerConfig := uiserver.HandlerConfig{
		StaticFS:    static.Files,
		IndexHTML:   indexHTML,
		RequireAuth: requireAuth,
		AuthToken:   token,
		Register: func(mux *http.ServeMux) {
			unavailableDetail := "UI server cannot reach the Beads daemon; retry after restarting the daemon."
			unavailable := func(message string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					uiapi.WriteServiceUnavailable(w, message, unavailableDetail)
				})
			}

			var listHandler http.Handler
			if listClient != nil {
				listHandler = uiapi.NewListHandler(listClient)
			} else {
				listHandler = unavailable("issue list unavailable")
			}

			var createHandler http.Handler
			if createClient != nil {
				createHandler = uiapi.NewCreateHandler(createClient, nil)
			}

			mux.Handle("/api/issues", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					if createHandler != nil {
						createHandler.ServeHTTP(w, r)
					} else {
						http.Error(w, "issue creation unavailable", http.StatusServiceUnavailable)
					}
					return
				}
				if listHandler != nil {
					listHandler.ServeHTTP(w, r)
				} else {
					http.Error(w, "issue list unavailable", http.StatusServiceUnavailable)
				}
			}))

			if searchService != nil {
				mux.Handle("/api/search", uiapi.NewSearchHandler(searchService))
			} else {
				mux.Handle("/api/search", unavailable("search unavailable"))
			}

			if listClient != nil {
				mux.Handle("/fragments/issues", uiapi.NewListFragmentHandler(
					listClient,
					uiapi.WithListFragmentTemplate(issueListTemplate),
				))
			} else {
				mux.Handle("/fragments/issues", unavailable("issue list fragment unavailable"))
			}

			if detailClient != nil {
				handlerOpts := []uiapi.IssueHandlerOption{
					uiapi.WithLabelClient(labelClient),
					uiapi.WithDeleteClient(deleteClient),
				}
				issueHandler := uiapi.NewIssueHandler(
					detailClient,
					renderer,
					updateClient,
					handlerOpts...,
				)
				mux.Handle("/api/issues/", issueHandler)
			} else {
				mux.Handle("/api/issues/", unavailable("issue detail unavailable"))
			}

			if detailClient != nil {
				mux.Handle("/fragments/issue", uiapi.NewDetailFragmentHandler(detailClient, renderer, issueDetailTemplate))
			} else {
				mux.Handle("/fragments/issue", unavailable("issue detail fragment unavailable"))
			}

			mux.Handle("/events", uiapi.NewEventStreamHandler(eventSource))
			if bulkClient != nil {
				mux.Handle("/api/issues/bulk", uiapi.NewBulkHandler(bulkClient, nil))
			} else {
				mux.Handle("/api/issues/bulk", unavailable("bulk updates unavailable"))
			}

			// Theme preference endpoint (no dependencies, always available)
			mux.Handle("/api/theme", uiapi.NewThemeHandler())
		},
	}

	handler, err := uiserver.NewHandler(handlerConfig)
	if err != nil {
		return err
	}

	if requireAuth {
		tokenPath, err := persistUIAuthToken(token)
		if err != nil {
			return fmt.Errorf("persist auth token: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Beads UI auth token: %s\n", token)
		if tokenPath != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Token saved to %s\n", tokenPath)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Send requests with header: Authorization: Bearer %s\n", token)
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenAddr, err)
	}
	defer listener.Close()

	shutdownToken := strings.TrimSpace(os.Getenv("BD_UI_SHUTDOWN_TOKEN"))
	var server *http.Server

	if shutdownToken != "" {
		baseHandler := handler
		mux := http.NewServeMux()
		mux.HandleFunc("/__shutdown", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Query().Get("token") != shutdownToken {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"shutting_down"}`))

			go func() {
				if server == nil {
					return
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdownCtx)
			}()
		})
		mux.Handle("/", baseHandler)
		handler = mux
	}

	server = newUIServer(handler)

	serveErrCh := make(chan error, 1)
	go func() {
		var serveErr error
		if useTLS {
			serveErr = server.ServeTLS(listener, certPath, keyPath)
		} else {
			serveErr = server.Serve(listener)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serveErrCh <- serveErr
		}
		close(serveErrCh)
	}()

	baseURL := formatBaseURL(listener.Addr(), useTLS)
	globalUISessionManager.BindListenURL(baseURL)
	writeStructuredLog(cmd.ErrOrStderr(), "info", "ui.server.listening", map[string]any{
		"url": baseURL,
	})
	fmt.Fprintf(cmd.OutOrStdout(), "Beads UI listening on %s\n", baseURL)

	if !uiNoOpen {
		if err := launchBrowser(ctx, baseURL, uiOpenCmd, cmd.ErrOrStderr()); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to open browser automatically: %v\n", err)
		}
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("shutdown ui server: %w", err)
		}

		if err, ok := <-serveErrCh; ok && err != nil {
			return err
		}
		return nil
	case err, ok := <-serveErrCh:
		if ok && err != nil {
			return err
		}
		return nil
	}
}

func launchBrowser(ctx context.Context, url string, override string, stderr io.Writer) error {
	var execCmd *exec.Cmd

	if strings.TrimSpace(override) != "" {
		args, err := shlex.Split(override)
		if err != nil {
			return fmt.Errorf("parse open-command: %w", err)
		}
		if len(args) == 0 {
			return errors.New("open-command resolved to empty executable")
		}
		execCmd = exec.CommandContext(ctx, args[0], append(args[1:], url)...)
	} else {
		execCmd = defaultBrowserCommand(ctx, url)
	}

	if execCmd == nil {
		return errors.New("no browser command available for this platform")
	}

	execCmd.Stdout = io.Discard
	if stderr != nil {
		execCmd.Stderr = stderr
	} else {
		execCmd.Stderr = io.Discard
	}

	if err := execCmd.Start(); err != nil {
		return err
	}

	go func() {
		_ = execCmd.Wait()
	}()

	return nil
}

func defaultBrowserCommand(ctx context.Context, url string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", url)
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return exec.CommandContext(ctx, "xdg-open", url)
	}
}

func newUIServer(handler http.Handler) *http.Server {
	// Disable WriteTimeout to avoid terminating long-lived SSE connections.
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		WriteTimeout:      0,
	}
}

func formatBaseURL(addr net.Addr, useTLS bool) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		scheme := "http"
		if useTLS {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s", scheme, addr.String())
	}

	host := tcpAddr.IP.String()
	if tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		host = "127.0.0.1"
	}

	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}

	scheme := "http"
	if useTLS {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, host, tcpAddr.Port)
}

func generateUIAuthToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := strings.TrimRight(base64.URLEncoding.EncodeToString(buf), "=")
	return token, nil
}

func persistUIAuthToken(token string) (string, error) {
	path, err := uiTokenFilePath()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func uiTokenFilePath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("BD_UI_TOKEN_FILE")); custom != "" {
		return custom, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".beads", "ui-token"), nil
}

func ensureSelfSignedCertificate(listenAddr string, out io.Writer) (string, string, error) {
	certDir, err := uiCertDirectory()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return "", "", err
	}

	certPath := filepath.Join(certDir, "ui-cert.pem")
	keyPath := filepath.Join(certDir, "ui-key.pem")

	hosts := certHosts(listenAddr)
	certPEM, keyPEM, err := uiserver.GenerateSelfSignedCertificate(hosts, 365*24*time.Hour)
	if err != nil {
		return "", "", err
	}

	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return "", "", err
	}

	if out != nil {
		fmt.Fprintf(out, "Self-signed TLS certificate written to %s (key %s)\n", certPath, keyPath)
	}

	return certPath, keyPath, nil
}

func uiCertDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".beads"), nil
}

func certHosts(listenAddr string) []string {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		host = ""
	}
	host = strings.TrimSpace(host)

	set := make(map[string]struct{})
	add := func(values ...string) {
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if _, exists := set[trimmed]; exists {
				continue
			}
			set[trimmed] = struct{}{}
		}
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		add("127.0.0.1", "::1", "localhost")
	} else {
		add(host)
		add("127.0.0.1", "::1", "localhost")
	}

	hosts := make([]string, 0, len(set))
	for value := range set {
		hosts = append(hosts, value)
	}
	return hosts
}
