package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func makeSocketTempDir(t testing.TB) string {
	t.Helper()

	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = os.TempDir()
	} else if _, err := os.Stat(base); err != nil {
		base = os.TempDir()
	}

	tmpDir, err := os.MkdirTemp(base, "bd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return tmpDir
}

func TestGetPIDFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	oldDBPath := dbPath
	defer func() { dbPath = oldDBPath }()

	dbPath = filepath.Join(tmpDir, ".beads", "test.db")
	pidFile, err := getPIDFilePath(false) // test local daemon
	if err != nil {
		t.Fatalf("getPIDFilePath failed: %v", err)
	}

	expected := filepath.Join(tmpDir, ".beads", "daemon.pid")
	if pidFile != expected {
		t.Errorf("Expected PID file %s, got %s", expected, pidFile)
	}

	if _, err := os.Stat(filepath.Dir(pidFile)); os.IsNotExist(err) {
		t.Error("Expected beads directory to be created")
	}
}

func TestGetLogFilePath(t *testing.T) {
	tests := []struct {
		name string
		set  func(t *testing.T) (userPath, dbFile, expected string)
	}{
		{
			name: "user specified path",
			set: func(t *testing.T) (string, string, string) {
				userDir := t.TempDir()
				dbDir := t.TempDir()
				userPath := filepath.Join(userDir, "bd.log")
				dbFile := filepath.Join(dbDir, ".beads", "test.db")
				return userPath, dbFile, userPath
			},
		},
		{
			name: "default with dbPath",
			set: func(t *testing.T) (string, string, string) {
				dbDir := t.TempDir()
				dbFile := filepath.Join(dbDir, ".beads", "test.db")
				return "", dbFile, filepath.Join(dbDir, ".beads", "daemon.log")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userPath, dbFile, expected := tt.set(t)

			oldDBPath := dbPath
			defer func() { dbPath = oldDBPath }()
			dbPath = dbFile

			result, err := getLogFilePath(userPath, false) // test local daemon
			if err != nil {
				t.Fatalf("getLogFilePath failed: %v", err)
			}
			if result != expected {
				t.Errorf("Expected %s, got %s", expected, result)
			}
		})
	}
}

func TestIsDaemonRunning_NotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	isRunning, pid := isDaemonRunning(pidFile)
	if isRunning {
		t.Errorf("Expected daemon not running, got running with PID %d", pid)
	}
}

func TestIsDaemonRunning_StalePIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	if err := os.WriteFile(pidFile, []byte("99999"), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	isRunning, pid := isDaemonRunning(pidFile)
	if isRunning {
		t.Errorf("Expected daemon not running (stale PID), got running with PID %d", pid)
	}
}

func TestIsDaemonRunning_CurrentProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	currentPID := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(currentPID)), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	isRunning, pid := isDaemonRunning(pidFile)
	if !isRunning {
		t.Error("Expected daemon running (current process PID)")
	}
	if pid != currentPID {
		t.Errorf("Expected PID %d, got %d", currentPID, pid)
	}
}

func TestDaemonIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	dbDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(dbDir, "test.db")
	testStore, err := sqlite.New(testDBPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	oldStore := store
	oldDBPath := dbPath
	defer func() {
		testStore.Close()
		store = oldStore
		dbPath = oldDBPath
	}()
	store = testStore
	dbPath = testDBPath

	ctx := context.Background()
	testIssue := &types.Issue{
		Title:       "Test daemon issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	if err := testStore.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	pidFile := filepath.Join(dbDir, "daemon.pid")
	_ = pidFile

	if isRunning, _ := isDaemonRunning(pidFile); isRunning {
		t.Fatal("Daemon should not be running at start of test")
	}

	t.Run("start requires git repo", func(t *testing.T) {
		if isGitRepo() {
			t.Skip("Already in a git repo, skipping this test")
		}
	})

	t.Run("status shows not running", func(t *testing.T) {
		if isRunning, _ := isDaemonRunning(pidFile); isRunning {
			t.Error("Daemon should not be running")
		}
	})
}

func TestDaemonPIDFileManagement(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "daemon.pid")

	testPID := 12345
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(testPID)), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}

	readPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("Failed to parse PID: %v", err)
	}

	if readPID != testPID {
		t.Errorf("Expected PID %d, got %d", testPID, readPID)
	}

	if err := os.Remove(pidFile); err != nil {
		t.Fatalf("Failed to remove PID file: %v", err)
	}

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("PID file should be removed")
	}
}

func TestDaemonLogFileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	logF, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open log file: %v", err)
	}
	defer logF.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := "Test log message"
	_, err = logF.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, msg))
	if err != nil {
		t.Fatalf("Failed to write to log file: %v", err)
	}

	logF.Sync()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), msg) {
		t.Errorf("Log file should contain message: %s", msg)
	}
}

func TestDaemonIntervalParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"1h", 1 * time.Hour},
		{"30s", 30 * time.Second},
		{"2m30s", 2*time.Minute + 30*time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := time.ParseDuration(tt.input)
			if err != nil {
				t.Errorf("Failed to parse duration %s: %v", tt.input, err)
			}
			if d != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, d)
			}
		})
	}
}

func TestDaemonRPCServerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	dbDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(dbDir, "test.db")
	testStore, err := sqlite.New(testDBPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testStore.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testIssue := &types.Issue{
		Title:       "Test RPC issue",
		Description: "Test RPC integration",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	if err := testStore.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	if testIssue.ID == "" {
		t.Fatal("Issue ID should be set after creation")
	}
}

func TestDaemonConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	dbDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(dbDir, "test.db")
	testStore, err := sqlite.New(testDBPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()

	numGoroutines := 10
	errChan := make(chan error, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			issue := &types.Issue{
				Title:       fmt.Sprintf("Concurrent issue %d", n),
				Description: "Test concurrent operations",
				Status:      types.StatusOpen,
				Priority:    1,
				IssueType:   types.TypeTask,
			}

			if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
				errChan <- fmt.Errorf("goroutine %d create failed: %w", n, err)
				return
			}

			updates := map[string]interface{}{
				"status": types.StatusInProgress,
			}
			if err := testStore.UpdateIssue(ctx, issue.ID, updates, "test"); err != nil {
				errChan <- fmt.Errorf("goroutine %d update failed: %w", n, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}

	issues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	if len(issues) != numGoroutines {
		t.Errorf("Expected %d issues, got %d", numGoroutines, len(issues))
	}
}

func TestDaemonSocketCleanupOnShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "test.sock")
	testDBPath := filepath.Join(tmpDir, "test.db")

	testStore, err := sqlite.New(testDBPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	server := newMockDaemonServer(socketPath, testStore)

	ctx, cancel := context.WithCancel(context.Background())

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Start(ctx)
	}()

	// Wait for server to be ready
	if err := server.WaitReady(2 * time.Second); err != nil {
		t.Fatal(err)
	}

	// Verify socket exists (with retry for filesystem sync)
	var socketFound bool
	var lastErr error
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			socketFound = true
			break
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !socketFound {
		t.Fatalf("Socket should exist after server is ready (path=%s, err=%v)", socketPath, lastErr)
	}

	cancel()

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Error("Server did not shut down within timeout")
	}

	testStore.Close()

	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("Socket should be cleaned up after shutdown")
	}
}

func TestDaemonServerStartFailureSocketExists(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "test.sock")
	testDBPath := filepath.Join(tmpDir, "test.db")

	testStore1, err := sqlite.New(testDBPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testStore1.Close()

	server1 := newMockDaemonServer(socketPath, testStore1)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	go server1.Start(ctx1)

	// Wait for server to be ready
	if err := server1.WaitReady(2 * time.Second); err != nil {
		t.Fatal(err)
	}

	// Verify socket exists (with retry for filesystem sync)
	var socketFound bool
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			socketFound = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !socketFound {
		t.Fatal("Socket should exist for first server")
	}

	testStore2, err := sqlite.New(filepath.Join(tmpDir, "test2.db"))
	if err != nil {
		t.Fatalf("Failed to create second test database: %v", err)
	}
	defer testStore2.Close()

	server2 := newMockDaemonServer(socketPath, testStore2)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()

	startErr := make(chan error, 1)
	go func() {
		startErr <- server2.Start(ctx2)
	}()

	select {
	case err := <-startErr:
		if err == nil {
			t.Error("Expected second server to fail to start, but it succeeded")
		}
	case <-time.After(1 * time.Second):
	}

	cancel1()
	time.Sleep(200 * time.Millisecond)
}

func TestDaemonGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "test.sock")
	testDBPath := filepath.Join(tmpDir, "test.db")

	testStore, err := sqlite.New(testDBPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	server := newMockDaemonServer(socketPath, testStore)

	ctx, cancel := context.WithCancel(context.Background())

	serverDone := make(chan error, 1)
	startTime := time.Now()

	go func() {
		serverDone <- server.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)

	cancel()

	select {
	case err := <-serverDone:
		shutdownDuration := time.Since(startTime)

		if err != nil && err != context.Canceled {
			t.Errorf("Server returned unexpected error: %v", err)
		}

		if shutdownDuration > 3*time.Second {
			t.Errorf("Shutdown took too long: %v", shutdownDuration)
		}

		testStore.Close()

		if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
			t.Error("Socket should be cleaned up after graceful shutdown")
		}

	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down gracefully within timeout")
	}
}

type mockDaemonServer struct {
	socketPath string
	store      storage.Storage
	listener   net.Listener
	mu         sync.Mutex
	shutdown   bool
	ready      chan error
}

func newMockDaemonServer(socketPath string, store storage.Storage) *mockDaemonServer {
	return &mockDaemonServer{
		socketPath: socketPath,
		store:      store,
		ready:      make(chan error, 1),
	}
}

func (s *mockDaemonServer) WaitReady(timeout time.Duration) error {
	select {
	case err := <-s.ready:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("server did not become ready within %v", timeout)
	}
}

func (s *mockDaemonServer) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Check if socket already exists
	if _, err := os.Stat(s.socketPath); err == nil {
		// Socket exists - try to connect to see if server is running
		conn, err := net.Dial("unix", s.socketPath)
		if err == nil {
			conn.Close()
			startErr := fmt.Errorf("socket already in use: %s", s.socketPath)
			s.ready <- startErr
			return startErr
		}
		// Socket is stale, remove it
		_ = os.Remove(s.socketPath)
	}

	var err error
	s.listener, err = net.Listen("unix", s.socketPath)
	if err != nil {
		startErr := fmt.Errorf("failed to listen on socket: %w", err)
		s.ready <- startErr
		return startErr
	}

	// Signal that server is ready
	s.ready <- nil

	// Set up cleanup before accepting connections
	defer func() {
		s.listener.Close()
		os.Remove(s.socketPath)
	}()

	doneChan := make(chan struct{})
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		s.listener.Close()
		close(doneChan)
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			shutdown := s.shutdown
			s.mu.Unlock()
			if shutdown {
				<-doneChan
				return ctx.Err()
			}
			return fmt.Errorf("failed to accept connection: %w", err)
		}
		conn.Close()
	}
}
