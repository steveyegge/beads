//go:build ui_e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/ui/static"
)

type logBuffer struct {
	mu    sync.Mutex
	lines []string
}

func (b *logBuffer) Append(line string) {
	b.mu.Lock()
	b.lines = append(b.lines, line)
	b.mu.Unlock()
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}

func TestPlaywrightCanLoadStubUI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `
			<!doctype html>
			<html lang="en">
				<head><title>Beads UI Smoke</title></head>
				<body>
					<h1 data-testid="smoke-title">Beads</h1>
				</body>
			</html>
		`)
	})

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	text, err := h.Page().TextContent("[data-testid='smoke-title']")
	if err != nil {
		t.Fatalf("read smoke title: %v", err)
	}

	if got := text; got != "Beads" {
		t.Fatalf("expected title text %q, got %q", "Beads", got)
	}
}

func TestPlaywrightCanLoadBDUICommand(t *testing.T) {
	repoRoot := findRepoRoot(t)

	t.Run("loopback without auth", func(t *testing.T) {
		tmpDir, dbPath := setupTempDB(t)
		_ = tmpDir

		baseURL, shutdown, lines, stderr := launchBDUICommand(t, repoRoot, dbPath,
			[]string{"--listen", "127.0.0.1:0", "--no-open"},
			nil,
		)

		h := NewRemoteHarness(t, baseURL, shutdown, HarnessConfig{Headless: true})

		resp := h.MustNavigate("/")
		if status := resp.Status(); status != http.StatusOK {
			t.Fatalf("expected status 200, got %d (stdout=%s stderr=%s)", status, lines.String(), stderr.String())
		}

		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer waitCancel()

		h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
			_, err := h.Page().WaitForSelector("[data-testid='ui-title']", playwright.PageWaitForSelectorOptions{
				Timeout: playwright.Float(2000),
			})
			return err
		})

		title, err := h.Page().TextContent("[data-testid='ui-title']")
		if err != nil {
			t.Fatalf("read ui title: %v (stdout=%s stderr=%s)", err, lines.String(), stderr.String())
		}

		if got := strings.TrimSpace(title); got != "Beads" {
			t.Fatalf("unexpected ui title %q (stdout=%s stderr=%s)", got, lines.String(), stderr.String())
		}

		h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
			_, err := h.Page().WaitForSelector("[data-role='issue-row'] [data-testid='issue-id-pill']", playwright.PageWaitForSelectorOptions{
				Timeout: playwright.Float(2000),
			})
			return err
		})

		idText, err := h.Page().TextContent("[data-role='issue-row'] [data-testid='issue-id-pill']")
		if err != nil {
			t.Fatalf("read row id badge: %v", err)
		}
		if trimmed := strings.TrimSpace(idText); trimmed == "" {
			t.Fatalf("expected issue row ID badge to have text (stdout=%s stderr=%s)", lines.String(), stderr.String())
		}

		respList, err := http.Get(baseURL + "/api/issues?queue=ready")
		if err != nil {
			t.Fatalf("GET /api/issues: %v", err)
		}
		defer respList.Body.Close()
		if respList.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200 for list, got %d", respList.StatusCode)
		}
		var payload struct {
			Issues []struct {
				ID string `json:"id"`
			} `json:"issues"`
		}
		if err := json.NewDecoder(respList.Body).Decode(&payload); err != nil {
			t.Fatalf("decode list payload: %v", err)
		}
		if len(payload.Issues) == 0 {
			t.Fatalf("expected seeded issues in payload, got none")
		}

		respFrag, err := http.Get(baseURL + "/fragments/issue?id=ui-issue-1")
		if err != nil {
			t.Fatalf("GET /fragments/issue: %v", err)
		}
		defer respFrag.Body.Close()
		if respFrag.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200 for fragment, got %d", respFrag.StatusCode)
		}
		body, err := io.ReadAll(respFrag.Body)
		if err != nil {
			t.Fatalf("read fragment body: %v", err)
		}
		if !strings.Contains(string(body), "data-testid=\"issue-detail\"") {
			t.Fatalf("fragment missing issue detail markup: %s", string(body))
		}
		if !strings.Contains(string(body), `data-testid="issue-detail-id"`) {
			t.Fatalf("fragment missing issue id badge: %s", string(body))
		}
	})

	t.Run("remote binding requires auth", func(t *testing.T) {
		tmpDir, dbPath := setupTempDB(t)
		token := "remote-test-token"
		tokenFile := filepath.Join(tmpDir, "ui-token.txt")

		baseURL, shutdown, lines, stderr := launchBDUICommand(t, repoRoot, dbPath,
			[]string{"--listen", "0.0.0.0:0", "--no-open", "--allow-remote", "--auth-token", token},
			map[string]string{"BD_UI_TOKEN_FILE": tokenFile},
		)

		resp, err := http.Get(baseURL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz without auth: %v", err)
		}
		io.Copy(io.Discard, resp.Body) // nolint:errcheck
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 without Authorization header, got %d", resp.StatusCode)
		}

		req, err := http.NewRequest(http.MethodGet, baseURL+"/healthz", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /healthz with auth: %v", err)
		}
		io.Copy(io.Discard, resp.Body) // nolint:errcheck
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 with Authorization header, got %d", resp.StatusCode)
		}

		content, err := os.ReadFile(tokenFile)
		if err != nil {
			t.Fatalf("read token file: %v", err)
		}
		if strings.TrimSpace(string(content)) != token {
			t.Fatalf("token file mismatch: want %q got %q", token, strings.TrimSpace(string(content)))
		}

		respList, err := http.Get(baseURL + "/api/issues?queue=ready")
		if err != nil {
			t.Fatalf("GET /api/issues without auth: %v", err)
		}
		io.Copy(io.Discard, respList.Body) // nolint:errcheck
		respList.Body.Close()
		if respList.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 for list without auth, got %d", respList.StatusCode)
		}

		reqList, err := http.NewRequest(http.MethodGet, baseURL+"/api/issues?queue=ready", nil)
		if err != nil {
			t.Fatalf("NewRequest list: %v", err)
		}
		reqList.Header.Set("Authorization", "Bearer "+token)
		respList, err = http.DefaultClient.Do(reqList)
		if err != nil {
			t.Fatalf("GET /api/issues with auth: %v", err)
		}
		defer respList.Body.Close()
		if respList.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for list with auth, got %d", respList.StatusCode)
		}
		var listPayload struct {
			Issues []struct {
				ID string `json:"id"`
			} `json:"issues"`
		}
		if err := json.NewDecoder(respList.Body).Decode(&listPayload); err != nil {
			t.Fatalf("decode list payload: %v", err)
		}
		if len(listPayload.Issues) == 0 {
			t.Fatalf("expected seeded issues in list payload, got none")
		}

		reqFrag, err := http.NewRequest(http.MethodGet, baseURL+"/fragments/issue?id=ui-issue-1", nil)
		if err != nil {
			t.Fatalf("NewRequest fragment: %v", err)
		}
		reqFrag.Header.Set("Authorization", "Bearer "+token)
		respFrag, err := http.DefaultClient.Do(reqFrag)
		if err != nil {
			t.Fatalf("GET /fragments/issue with auth: %v", err)
		}
		defer respFrag.Body.Close()
		if respFrag.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for fragment with auth, got %d", respFrag.StatusCode)
		}
		body, err := io.ReadAll(respFrag.Body)
		if err != nil {
			t.Fatalf("read fragment body: %v", err)
		}
		if !strings.Contains(string(body), `data-testid="issue-detail"`) {
			t.Fatalf("fragment missing issue detail markup: %s", string(body))
		}

		h := NewRemoteHarness(t, baseURL, shutdown, HarnessConfig{Headless: true})
		if err := h.Page().SetExtraHTTPHeaders(map[string]string{
			"Authorization": "Bearer " + token,
		}); err != nil {
			t.Fatalf("set extra headers: %v", err)
		}

		respNav := h.MustNavigate("/")
		if status := respNav.Status(); status != http.StatusOK {
			t.Fatalf("expected status 200 with auth, got %d (stdout=%s stderr=%s)", status, lines.String(), stderr.String())
		}

		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer waitCancel()

		h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
			_, err := h.Page().WaitForSelector("[data-testid='ui-title']", playwright.PageWaitForSelectorOptions{
				Timeout: playwright.Float(2000),
			})
			return err
		})
	})
}

func setupTempDB(t testing.TB) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vc.db")
	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "ui"); err != nil {
		t.Fatalf("configure issue prefix: %v", err)
	}

	now := time.Now().UTC()
	issue := &types.Issue{
		ID:        "ui-issue-1",
		Title:     "Seed Issue For UI",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}
	blocker := &types.Issue{
		ID:        "ui-issue-2",
		Title:     "Blocking Issue",
		Status:    types.StatusInProgress,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.CreateIssue(ctx, issue, "ui-e2e"); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if err := store.CreateIssue(ctx, blocker, "ui-e2e"); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	dep := &types.Dependency{
		IssueID:     issue.ID,
		DependsOnID: blocker.ID,
		Type:        types.DepBlocks,
	}
	if err := store.AddDependency(ctx, dep, "ui-e2e"); err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	return tmpDir, dbPath
}

func launchBDUICommand(t testing.TB, repoRoot, dbPath string, uiArgs []string, extraEnv map[string]string) (string, func(), *logBuffer, *bytes.Buffer) {
	t.Helper()

	shutdownToken := fmt.Sprintf("ui-e2e-%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())

	workspaceDir := filepath.Dir(dbPath)
	socketPath := filepath.Join(workspaceDir, "bd.sock")
	_ = os.Remove(socketPath)

	serverStore, err := sqlite.New(dbPath)
	if err != nil {
		cancel()
		t.Fatalf("create rpc store: %v", err)
	}
	server := rpc.NewServer(socketPath, serverStore, workspaceDir, dbPath)
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.Start(context.Background())
	}()

	select {
	case <-server.WaitReady():
	case err := <-serverErrCh:
		cancel()
		t.Fatalf("start rpc server: %v", err)
	}

	fullArgs := append([]string{"run", "./cmd/bd"}, append([]string{"ui"}, uiArgs...)...)
	cmd := exec.CommandContext(ctx, "go", fullArgs...)
	cmd.Dir = repoRoot

	env := append(os.Environ(),
		"BEADS_DB="+dbPath,
		"BD_UI_SHUTDOWN_TOKEN="+shutdownToken,
	)
	for k, v := range extraEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("capture stdout: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("capture stderr: %v", err)
	}

	var stderrBuf bytes.Buffer
	go func() {
		_, _ = io.Copy(&stderrBuf, stderrPipe)
	}()

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start bd ui: %v (stderr=%s)", err, stderrBuf.String())
	}

	cmdErrCh := make(chan error, 1)
	go func() {
		cmdErrCh <- cmd.Wait()
	}()

	urlRegex := regexp.MustCompile(`http://[^\s]+`)
	lines := &logBuffer{}
	urlCh := make(chan string, 1)
	readErrCh := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			lines.Append(line)
			if match := urlRegex.FindString(line); match != "" {
				select {
				case urlCh <- strings.TrimSpace(match):
				default:
				}
			}
		}
		if err := scanner.Err(); err != nil {
			readErrCh <- err
		}
	}()

	var baseURL string

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			if baseURL != "" {
				shutdownURL := fmt.Sprintf("%s/__shutdown?token=%s", baseURL, url.QueryEscape(shutdownToken))
				reqCtx, reqCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer reqCancel()

				req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, shutdownURL, nil)
				if err == nil {
					resp, postErr := http.DefaultClient.Do(req)
					if postErr == nil {
						io.Copy(io.Discard, resp.Body) // nolint:errcheck
						resp.Body.Close()
					} else {
						lines.Append(fmt.Sprintf("shutdown post failed: %v", postErr))
					}
				}
			}

			cancel()

			select {
			case <-cmdErrCh:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-cmdErrCh
			}

			serverStopErr := server.Stop()
			select {
			case err := <-serverErrCh:
				if err != nil && serverStopErr == nil {
					lines.Append(fmt.Sprintf("rpc server stop error: %v", err))
				}
			case <-time.After(2 * time.Second):
				lines.Append("rpc server stop timeout")
			}
		})
	}

	t.Cleanup(shutdown)

	select {
	case baseURL = <-urlCh:
	case err := <-readErrCh:
		cancel()
		t.Fatalf("reading stdout failed: %v (stdout=%s stderr=%s)", err, lines.String(), stderrBuf.String())
	case err := <-cmdErrCh:
		cancel()
		t.Fatalf("ui command exited early: %v (stdout=%s stderr=%s)", err, lines.String(), stderrBuf.String())
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatalf("timeout waiting for ui listen address (stdout=%s stderr=%s)", lines.String(), stderrBuf.String())
	}

	return baseURL, shutdown, lines, &stderrBuf
}

func TestUIServerRefreshesOnSSEEvent(t *testing.T) {
	t.Parallel()

	source := newSSEStubSource(8)

	indexHTML := renderBasePage(t, "Beads")

	renderListFragment := func(title string, status types.Status, priority int, issueType types.IssueType) string {
		statusClass := strings.ToLower(string(issueType))
		if statusClass == "" {
			statusClass = "task"
		}
		typeLabel := strings.ToUpper(string(issueType))
		if typeLabel == "" {
			typeLabel = "TASK"
		}
		priorityLabel := fmt.Sprintf("P%d", priority)
		if priority < 0 || priority > 4 {
			priorityLabel = "P?"
		}
		priorityClass := fmt.Sprintf("ui-badge--priority-p%d", priority)
		if priority < 0 || priority > 4 {
			priorityClass = "ui-badge--priority-p?"
		}
		return fmt.Sprintf(`
			<div class="ui-issue-shell" data-role="issue-shell">
				<div class="ui-issue-list" data-role="issue-list-rows">
					<ul class="ui-issue-list-items" role="listbox" aria-label="Filtered issues">
						<li class="ui-issue-list-item" data-issue-id="ui-06" data-index="0" data-selected="false">
							<label class="ui-issue-select">
								<input type="checkbox" data-role="issue-select" aria-label="Select ui-06" />
								<span aria-hidden="true"></span>
							</label>
							<button
								type="button"
								class="ui-issue-row"
								data-role="issue-row"
								data-issue-id="ui-06"
								data-status="%s"
								data-priority="%d"
								data-type="%s"
								aria-selected="false"
								tabindex="-1"
							>
								<span class="ui-issue-row-heading">
									<span class="ui-issue-row-title">%s</span>
								</span>
								<span class="ui-issue-row-meta">
									<span class="ui-badge ui-badge--id" data-testid="issue-id-pill" aria-hidden="true">ui-06</span>
									<span class="ui-badge ui-badge--priority %s">%s</span>
									<span class="ui-badge ui-badge--type ui-badge--type-%s">%s</span>
									<time class="ui-issue-row-updated" datetime="%s">just now</time>
								</span>
							</button>
						</li>
					</ul>
				</div>
			</div>
		`,
			status,
			priority,
			strings.ToLower(string(issueType)),
			title,
			priorityClass,
			priorityLabel,
			statusClass,
			typeLabel,
			time.Now().UTC().Format(time.RFC3339),
		)
	}

	var listHTML atomic.Value
	listHTML.Store(renderListFragment("Awaiting update", types.StatusOpen, 2, types.TypeFeature))

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/api/issues", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				payload := map[string]any{
					"issues": []api.IssueSummary{
						{
							ID:        "ui-06",
							Title:     "Awaiting update",
							Status:    string(types.StatusOpen),
							IssueType: string(types.TypeFeature),
							Priority:  2,
							UpdatedAt: time.Now().UTC().Format(time.RFC3339),
						},
					},
				}
				if err := json.NewEncoder(w).Encode(payload); err != nil {
					http.Error(w, fmt.Sprintf("encode list: %v", err), http.StatusInternalServerError)
				}
			}))
			mux.Handle("/fragments/issues", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				html, _ := listHTML.Load().(string)
				fmt.Fprint(w, html)
			}))
			mux.Handle("/events", api.NewEventStreamHandler(source,
				api.WithHeartbeatInterval(5*time.Second),
			))
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

	select {
	case <-source.subscribed:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for SSE subscription")
	}

	listHTML.Store(renderListFragment("Live update ready", types.StatusInProgress, 1, types.TypeFeature))
	source.Publish(api.IssueEvent{
		Type: api.EventTypeUpdated,
		Issue: api.IssueSummary{
			ID:        "ui-06",
			Title:     "Live update ready",
			Status:    string(types.StatusInProgress),
			IssueType: string(types.TypeFeature),
			Priority:  1,
			Assignee:  "codex",
			UpdatedAt: time.Now().UTC().Add(30 * time.Second).Format(time.RFC3339),
		},
	})
	source.Close()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		text, err := h.Page().InnerText("[data-role='issue-row']")
		if err != nil {
			return err
		}
		if !strings.Contains(text, "Live update ready") {
			return fmt.Errorf("event feed missing update marker: %q", text)
		}
		return nil
	})
}

type sseStubSource struct {
	ch             chan api.IssueEvent
	subscribed     chan struct{}
	subscribedOnce sync.Once
}

func newSSEStubSource(buffer int) *sseStubSource {
	if buffer <= 0 {
		buffer = 1
	}
	return &sseStubSource{
		ch:         make(chan api.IssueEvent, buffer),
		subscribed: make(chan struct{}),
	}
}

func (s *sseStubSource) Subscribe(ctx context.Context) (<-chan api.IssueEvent, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.subscribedOnce.Do(func() {
		close(s.subscribed)
	})

	out := make(chan api.IssueEvent)
	go func() {
		defer close(out)
		for {
			select {
			case evt, ok := <-s.ch:
				if !ok {
					return
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

func (s *sseStubSource) Publish(evt api.IssueEvent) {
	s.ch <- evt
}

func (s *sseStubSource) Close() {
	close(s.ch)
}

func findRepoRoot(t testing.TB) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found starting from %s", dir)
		}
		dir = parent
	}
}
