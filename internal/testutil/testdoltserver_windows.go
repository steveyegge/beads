//go:build windows

package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// startDoltServerInner starts a test Dolt server on Windows.
// On Windows, file locking (syscall.Flock) is not available, so cross-process
// coordination falls back to a simple port-check: if the port is already
// listening, reuse it; otherwise start a new server.
func startDoltServerInner(tmpDirPrefix string) error {
	// Clean stale PID files and orphaned temp dirs.
	CleanStaleTestServers()

	// Determine port: use GT_DOLT_PORT if set externally, otherwise find a free one.
	var portStr string
	if p := os.Getenv("GT_DOLT_PORT"); p != "" {
		portStr = p
	} else {
		port, err := FindFreePort()
		if err != nil {
			return fmt.Errorf("finding free port: %w", err)
		}
		portStr = strconv.Itoa(port)
		os.Setenv("GT_DOLT_PORT", portStr) //nolint:tenv // intentional process-wide env
		doltPortSetByUs = true
	}

	port, _ := strconv.Atoi(portStr)
	pidPath := pidFilePathForPort(portStr)

	// Check if a server is already running on the port.
	if WaitForServer(port, 2*time.Second) {
		sharedServer = &TestDoltServer{Port: port}
		os.Setenv("BEADS_DOLT_PORT", portStr) //nolint:tenv // intentional process-wide env
		return nil
	}

	// No server running — start one.
	tmpDir, err := os.MkdirTemp("", tmpDirPrefix)
	if err != nil {
		return fmt.Errorf("creating dolt data dir: %w", err)
	}

	dbDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("creating dolt data subdir: %w", err)
	}

	// Configure dolt user identity (required by dolt init).
	doltEnv := append(os.Environ(), "DOLT_ROOT_PATH="+tmpDir)
	for _, args := range [][]string{
		{"dolt", "config", "--global", "--add", "user.name", "beads-test"},
		{"dolt", "config", "--global", "--add", "user.email", "test@beads.local"},
	} {
		cfgCmd := exec.Command(args[0], args[1:]...)
		cfgCmd.Env = doltEnv
		if out, cfgErr := cfgCmd.CombinedOutput(); cfgErr != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("%s failed: %w\n%s", args[1], cfgErr, out)
		}
	}

	initCmd := exec.Command("dolt", "init")
	initCmd.Dir = dbDir
	initCmd.Env = doltEnv
	if out, initErr := initCmd.CombinedOutput(); initErr != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("dolt init failed: %w\n%s", initErr, out)
	}

	verbose := os.Getenv("BEADS_TEST_DOLT_VERBOSE") == "1"

	cmd := exec.Command("dolt", "sql-server",
		"-H", "127.0.0.1",
		"-P", portStr,
		"--no-auto-commit",
	)
	cmd.Dir = dbDir
	cmd.Env = doltEnv
	if !verbose {
		cmd.Stdout = nil
		cmd.Stderr = nil
	}

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("starting dolt sql-server: %w", err)
	}

	// Write PID file so cleanup can find the server.
	pidContent := fmt.Sprintf("%d\n%s\n", cmd.Process.Pid, tmpDir)
	if err := os.WriteFile(pidPath, []byte(pidContent), 0666); err != nil { //nolint:gosec // test infrastructure
		_ = cmd.Process.Kill()
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("writing PID file: %w", err)
	}

	// Reap the process in the background.
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	// Wait for server to accept connections.
	deadline := time.Now().Add(serverStartTimeout)
	for time.Now().Before(deadline) {
		if WaitForServer(port, time.Second) {
			doltWeStarted = true

			srv := &TestDoltServer{
				Port:    port,
				cmd:     cmd,
				tmpDir:  tmpDir,
				pidFile: pidPath,
				crashed: make(chan struct{}),
			}

			go func() {
				select {
				case <-exited:
					srv.exitOnce.Do(func() {
						srv.exitErr = fmt.Errorf("dolt sql-server exited unexpectedly")
						close(srv.crashed)
						fmt.Fprintf(os.Stderr, "WARN: test dolt server (port %d) exited unexpectedly\n", port)
					})
				}
			}()

			sharedServer = srv
			os.Setenv("BEADS_DOLT_PORT", portStr) //nolint:tenv // intentional process-wide env
			return nil
		}
		select {
		case <-exited:
			_ = os.RemoveAll(tmpDir)
			_ = os.Remove(pidPath)
			return fmt.Errorf("dolt sql-server exited prematurely")
		default:
		}
		time.Sleep(500 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	<-exited
	_ = os.RemoveAll(tmpDir)
	_ = os.Remove(pidPath)
	return fmt.Errorf("dolt sql-server did not become ready within %s", serverStartTimeout)
}

// cleanupSharedServer kills the test dolt server on Windows.
// On Windows, file locking is not used, so cleanup simply reads the PID file
// and kills the server process if we started it.
func cleanupSharedServer() {
	defer func() {
		if doltPortSetByUs {
			_ = os.Unsetenv("GT_DOLT_PORT")
			_ = os.Unsetenv("BEADS_DOLT_PORT")
		}
	}()

	if sharedServer == nil {
		return
	}

	portStr := strconv.Itoa(sharedServer.Port)
	pidPath := pidFilePathForPort(portStr)

	data, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}

	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 {
		return
	}

	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return
	}
	dataDir := strings.TrimSpace(lines[1])

	proc, err := os.FindProcess(pid)
	if err == nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
	}

	if dataDir != "" {
		_ = os.RemoveAll(dataDir)
	}
	_ = os.Remove(pidPath)
}

// cleanStalePIDFiles scans PID files in /tmp for dead or orphaned test servers.
// Windows variant without syscall.Signal(0) — just reads PID files and tries to kill.
func cleanStalePIDFiles() {
	prefixes := []string{testPidPrefix, "dolt-test-server-"}
	for _, prefix := range prefixes {
		pattern := filepath.Join(testPidDir, prefix+"*.pid")
		entries, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, pidFile := range entries {
			cleanPIDFileWindows(pidFile)
		}
		// Also clean matching .lock files
		lockPattern := filepath.Join(testPidDir, prefix+"*.lock")
		lockEntries, _ := filepath.Glob(lockPattern)
		for _, lf := range lockEntries {
			_ = os.Remove(lf)
		}
	}
}

// cleanPIDFileWindows handles a single PID file on Windows.
func cleanPIDFileWindows(pidFile string) {
	data, err := os.ReadFile(pidFile) //nolint:gosec // G304: path from test cleanup
	if err != nil {
		_ = os.Remove(pidFile)
		return
	}
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 3)
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		_ = os.Remove(pidFile)
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(pidFile)
		return
	}
	// On Windows, FindProcess always succeeds. Try to kill — if the process
	// is dead, Kill returns an error which we ignore.
	_ = proc.Kill()
	if len(lines) >= 2 {
		dataDir := strings.TrimSpace(lines[1])
		if dataDir != "" {
			_ = os.RemoveAll(dataDir)
		}
	}
	_ = os.Remove(pidFile)
}
