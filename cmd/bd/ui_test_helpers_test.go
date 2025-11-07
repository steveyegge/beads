package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	internalbeads "github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/daemonrunner"
	"github.com/steveyegge/beads/internal/storage/sqlite"
)

func makeUITestWorkspace(t testing.TB) (string, string) {
	t.Helper()

	workspace := t.TempDir()
	initTestGitRepo(t, workspace)

	beadsDir := filepath.Join(workspace, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("create beads dir: %v", err)
	}

	dbFile := filepath.Join(beadsDir, internalbeads.CanonicalDatabaseName)
	store, err := sqlite.New(dbFile)
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "ui"); err != nil {
		store.Close()
		t.Fatalf("set issue prefix: %v", err)
	}
	store.Close()

	return workspace, dbFile
}

func startTestDaemon(t testing.TB, workspace, dbFile string) func() {
	t.Helper()

	// bd expects databases to be tied to the current repository fingerprint.
	// Tests spin up a synthetic repository under a temp directory, so allow the
	// daemon to ignore repo mismatches for the duration of each test run.
	previousValue, hadValue := os.LookupEnv("BEADS_IGNORE_REPO_MISMATCH")
	if err := os.Setenv("BEADS_IGNORE_REPO_MISMATCH", "1"); err != nil {
		t.Fatalf("set BEADS_IGNORE_REPO_MISMATCH: %v", err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv("BEADS_IGNORE_REPO_MISMATCH", previousValue)
		} else {
			_ = os.Unsetenv("BEADS_IGNORE_REPO_MISMATCH")
		}
	})

	beadsDir := filepath.Dir(dbFile)
	socketPath := filepath.Join(beadsDir, "bd.sock")
	_ = os.Remove(socketPath)

	cfg := daemonrunner.Config{
		Interval:      200 * time.Millisecond,
		AutoCommit:    false,
		AutoPush:      false,
		Global:        false,
		LogFile:       filepath.Join(beadsDir, "daemon-test.log"),
		PIDFile:       filepath.Join(beadsDir, "daemon.pid"),
		DBPath:        dbFile,
		BeadsDir:      beadsDir,
		SocketPath:    socketPath,
		WorkspacePath: workspace,
	}

	daemon := daemonrunner.New(cfg, Version)
	errCh := make(chan error, 1)

	go func() {
		errCh <- daemon.Start()
	}()

	readyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for {
		if waitForSocketReadiness(socketPath, 250*time.Millisecond) {
			break
		}
		select {
		case <-readyCtx.Done():
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("daemon failed to start: %v", err)
				}
			default:
			}
			t.Fatalf("daemon did not become ready before timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}

	return func() {
		_ = daemon.Stop()

		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("daemon exited with error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Log("daemon stop timed out")
		}
	}
}


var (
	bdBinaryOnce sync.Once
	bdBinaryPath string
	bdBinaryErr  error
)

type uiTestServer struct {
	baseURL     string
	sessionPath string
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	errCh       chan error

	stdout *concurrentBuffer
	stderr *concurrentBuffer

	stopOnce sync.Once
}

func (s *uiTestServer) BaseURL() string {
	return s.baseURL
}

func (s *uiTestServer) SessionPath() string {
	return s.sessionPath
}

func (s *uiTestServer) pollErr() error {
	if s == nil || s.errCh == nil {
		return nil
	}

	select {
	case err, ok := <-s.errCh:
		if !ok {
			err = nil
		}
		s.errCh = nil
		return err
	default:
		return nil
	}
}

func (s *uiTestServer) Stop(t testing.TB) {
	t.Helper()

	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}

		waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if s.errCh != nil {
			select {
			case err := <-s.errCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) {
						t.Fatalf("ui command exited with error: %v (stdout=%q stderr=%q)", err, s.stdout.String(), s.stderr.String())
					}
				}
				s.errCh = nil
			case <-waitCtx.Done():
				if s.cmd != nil && s.cmd.Process != nil {
					_ = s.cmd.Process.Kill()
				}
				t.Fatalf("timeout waiting for ui command to stop (stdout=%q stderr=%q)", s.stdout.String(), s.stderr.String())
			}
		}
	})
}

type uiSessionSnapshot struct {
	SocketPath       string `json:"socket_path"`
	WorkspacePath    string `json:"workspace_path"`
	DatabasePath     string `json:"database_path"`
	ListenURL        string `json:"listen_url"`
	EndpointNetwork  string `json:"endpoint_network"`
	EndpointAddress  string `json:"endpoint_address"`
	AutoStartAttempt bool   `json:"auto_start_attempted"`
	AutoStartSuccess bool   `json:"auto_start_succeeded"`
	PID              int    `json:"pid"`
	AuthTokenSHA256  string `json:"auth_token_sha256"`
	UpdatedAt        string `json:"updated_at"`
}

func launchUITestServer(t testing.TB, workspace, dbFile string) *uiTestServer {
	t.Helper()

	binaryPath := ensureBdBinary(t)

	sessionPath := filepath.Join(filepath.Dir(dbFile), "ui-session.json")
	_ = os.Remove(sessionPath)
	_ = os.Remove(sessionPath + ".tmp")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Fatalf("restore wd: %v", chdirErr)
		}
	})

	var stdout, stderr concurrentBuffer

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, binaryPath, "ui", "--listen", "127.0.0.1:0", "--no-open")
	cmd.Dir = workspace
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("BEADS_DB=%s", dbFile),
		fmt.Sprintf("BEADS_IGNORE_REPO_MISMATCH=%s", os.Getenv("BEADS_IGNORE_REPO_MISMATCH")),
	)

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start bd ui: %v (stderr=%q)", err, stderr.String())
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Wait()
	}()

	server := &uiTestServer{
		sessionPath: sessionPath,
		cancel:      cancel,
		errCh:       errCh,
		cmd:         cmd,
		stdout:      &stdout,
		stderr:      &stderr,
	}

	url := waitForUILaunch(t, sessionPath, &stdout, &stderr)
	server.baseURL = url

	waitForHealthEndpoint(t, server, url)

	t.Cleanup(func() {
		server.Stop(t)
	})

	return server
}

func waitForUILaunch(t testing.TB, sessionPath string, stdout, stderr *concurrentBuffer) string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	urlPattern := regexp.MustCompile(`https?://[^\s]+`)

	for time.Now().Before(deadline) {
		if url := readListenURL(sessionPath); url != "" {
			return url
		}

		if match := urlPattern.FindString(stdout.String()); match != "" {
			return strings.TrimSpace(match)
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("timeout waiting for ui session (stdout=%q stderr=%q)", stdout.String(), stderr.String())
	return ""
}

func readListenURL(sessionPath string) string {
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return ""
	}
	var payload struct {
		ListenURL string `json:"listen_url"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.ListenURL)
}

func waitForHealthEndpoint(t testing.TB, server *uiTestServer, baseURL string) {
	t.Helper()

	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(15 * time.Second)

	var lastErr error
	lastStatus := 0

	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("%s/healthz", strings.TrimSuffix(baseURL, "/")))
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close() // nolint:errcheck
				return
			}
			resp.Body.Close() // nolint:errcheck
			lastStatus = resp.StatusCode
		} else {
			lastErr = err
		}
		if pollErr := server.pollErr(); pollErr != nil && !errors.Is(pollErr, context.Canceled) {
			t.Fatalf("ui command exited before healthz: %v (stdout=%q stderr=%q)", pollErr, server.stdout.String(), server.stderr.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	if pollErr := server.pollErr(); pollErr != nil && !errors.Is(pollErr, context.Canceled) {
		t.Fatalf("ui command exited before healthz: %v (stdout=%q stderr=%q)", pollErr, server.stdout.String(), server.stderr.String())
	}

	t.Fatalf("ui server did not respond to /healthz (lastStatus=%d lastErr=%v stdout=%q stderr=%q)", lastStatus, lastErr, server.stdout.String(), server.stderr.String())
}

func loadUISessionSnapshot(t testing.TB, sessionPath string) uiSessionSnapshot {
	t.Helper()

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read ui session: %v", err)
	}

	var snapshot uiSessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode ui session: %v", err)
	}
	return snapshot
}

func ensureBdBinary(t testing.TB) string {
	bdBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "bd-ui-binary-*")
		if err != nil {
			bdBinaryErr = err
			return
		}

		name := "bd-ui-test"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		outPath := filepath.Join(dir, name)

		_, file, _, ok := runtime.Caller(0)
		if !ok {
			bdBinaryErr = errors.New("runtime caller unavailable")
			return
		}
		repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

		cmd := exec.Command("go", "build", "-o", outPath, "./cmd/bd")
		cmd.Dir = repoRoot
		cmd.Env = os.Environ()
		if output, err := cmd.CombinedOutput(); err != nil {
			bdBinaryErr = fmt.Errorf("go build bd: %w (output=%s)", err, string(output))
			return
		}

		bdBinaryPath = outPath
	})

	if bdBinaryErr != nil {
		t.Fatalf("build bd binary: %v", bdBinaryErr)
	}
	return bdBinaryPath
}
