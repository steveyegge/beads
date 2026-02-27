//go:build !windows

package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// reapStaleDoltServers finds and kills dolt sql-server processes that:
//   - Have a --data-dir containing "dolt-test-server" or "beads-test-dolt" (test servers, not production)
//   - Have been running for longer than maxAge
//
// This prevents zombie test servers from accumulating when test processes
// are SIGKILL'd (e.g., go test -timeout expiration) and cleanup never runs.
func reapStaleDoltServers(maxAge time.Duration) {
	// Use ps to find dolt sql-server processes with test data dirs.
	// Format: PID ELAPSED ARGS
	out, err := exec.Command("ps", "-eo", "pid,etime,args").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "dolt sql-server") {
			continue
		}
		// Only target test servers, not production
		if !strings.Contains(line, "dolt-test-server") && !strings.Contains(line, "beads-test-dolt") {
			continue
		}
		// Don't kill production (port 3307)
		if strings.Contains(line, "--port 3307") || strings.Contains(line, "-P 3307") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		elapsed := parseElapsed(fields[1])
		if elapsed < maxAge {
			continue
		}
		// Kill the stale test server
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}
}

// parseElapsed converts ps etime format (HH:MM:SS or MM:SS or DD-HH:MM:SS) to duration.
func parseElapsed(s string) time.Duration {
	var days, hours, mins, secs int

	// Handle DD-HH:MM:SS format
	if idx := strings.Index(s, "-"); idx >= 0 {
		fmt.Sscanf(s[:idx], "%d", &days)
		s = s[idx+1:]
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 3:
		fmt.Sscanf(parts[0], "%d", &hours)
		fmt.Sscanf(parts[1], "%d", &mins)
		fmt.Sscanf(parts[2], "%d", &secs)
	case 2:
		fmt.Sscanf(parts[0], "%d", &mins)
		fmt.Sscanf(parts[1], "%d", &secs)
	}

	return time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(mins)*time.Minute +
		time.Duration(secs)*time.Second
}

// startDoltServerInner starts or reuses a shared test Dolt server using
// cross-process file locking (syscall.Flock) to coordinate with concurrent
// test binaries.
//
// Protocol:
//  1. Reap zombie test servers older than 1 hour.
//  2. Pick a port (from GT_DOLT_PORT or FindFreePort).
//  3. Acquire LOCK_EX on /tmp/beads-test-dolt-<port>.lock.
//  4. Under the lock, check if a server is already running on that port.
//     If yes, downgrade to LOCK_SH and reuse it.
//  5. If no server, start one, write PID file, wait for it to accept connections.
//  6. Downgrade to LOCK_SH. The lock file is held for the test binary's lifetime.
func startDoltServerInner(tmpDirPrefix string) error {
	// Reap zombie test servers from previous crashed test runs.
	reapStaleDoltServers(1 * time.Hour)

	// Also clean stale PID files and orphaned temp dirs.
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

	lockPath := lockFilePathForPort(portStr)
	pidPath := pidFilePathForPort(portStr)

	// Open the lock file (kept open for the lifetime of the test binary).
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0666) //nolint:gosec // test infrastructure
	if err != nil {
		return fmt.Errorf("opening lock file %s: %w", lockPath, err)
	}

	// Acquire exclusive lock for the startup phase.
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		_ = lockFile.Close()
		return fmt.Errorf("acquiring startup lock: %w", err)
	}

	port, _ := strconv.Atoi(portStr)

	// Under the exclusive lock: check if a server is already running
	// (started by another process that held the lock before us, or external).
	if WaitForServer(port, 2*time.Second) {
		// Server is already running. Downgrade to shared lock.
		if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_SH); err != nil {
			_ = lockFile.Close()
			return fmt.Errorf("downgrading to shared lock: %w", err)
		}
		doltLockFile = lockFile
		sharedServer = &TestDoltServer{Port: port}

		// Set BEADS_DOLT_PORT so downstream code connects to this server.
		os.Setenv("BEADS_DOLT_PORT", portStr) //nolint:tenv // intentional process-wide env
		return nil
	}

	// No server running — start one.
	tmpDir, err := os.MkdirTemp("", tmpDirPrefix)
	if err != nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return fmt.Errorf("creating dolt data dir: %w", err)
	}

	dbDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		_ = os.RemoveAll(tmpDir)
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
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
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			_ = lockFile.Close()
			return fmt.Errorf("%s failed: %w\n%s", args[1], cfgErr, out)
		}
	}

	initCmd := exec.Command("dolt", "init")
	initCmd.Dir = dbDir
	initCmd.Env = doltEnv
	if out, initErr := initCmd.CombinedOutput(); initErr != nil {
		_ = os.RemoveAll(tmpDir)
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
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
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return fmt.Errorf("starting dolt sql-server: %w", err)
	}

	// Write PID file so any last-exiting process can clean up.
	// Format: "PID\nDATA_DIR\n"
	pidContent := fmt.Sprintf("%d\n%s\n", cmd.Process.Pid, tmpDir)
	if err := os.WriteFile(pidPath, []byte(pidContent), 0666); err != nil { //nolint:gosec // test infrastructure
		_ = cmd.Process.Kill()
		_ = os.RemoveAll(tmpDir)
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return fmt.Errorf("writing PID file: %w", err)
	}

	// Reap the process in the background so ProcessState is populated on exit.
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	// Wait for server to accept connections (up to 30 seconds).
	deadline := time.Now().Add(serverStartTimeout)
	for time.Now().Before(deadline) {
		if WaitForServer(port, time.Second) {
			// Server is ready. Downgrade to shared lock.
			if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_SH); err != nil {
				_ = lockFile.Close()
				return fmt.Errorf("downgrading to shared lock: %w", err)
			}
			doltLockFile = lockFile
			doltWeStarted = true

			srv := &TestDoltServer{
				Port:    port,
				cmd:     cmd,
				tmpDir:  tmpDir,
				pidFile: pidPath,
				crashed: make(chan struct{}),
			}

			// Monitor goroutine: detect unexpected server exits
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

			// Set BEADS_DOLT_PORT so downstream code connects to this server.
			os.Setenv("BEADS_DOLT_PORT", portStr) //nolint:tenv // intentional process-wide env
			return nil
		}
		// Check if process exited (port bind failure, etc).
		select {
		case <-exited:
			_ = os.RemoveAll(tmpDir)
			_ = os.Remove(pidPath)
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			_ = lockFile.Close()
			return fmt.Errorf("dolt sql-server exited prematurely")
		default:
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Timed out — kill and clean up.
	_ = cmd.Process.Kill()
	<-exited
	_ = os.RemoveAll(tmpDir)
	_ = os.Remove(pidPath)
	_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	_ = lockFile.Close()
	return fmt.Errorf("dolt sql-server did not become ready within %s", serverStartTimeout)
}

// cleanupSharedServer conditionally kills the shared test dolt server.
//
// Shutdown protocol: try to upgrade from LOCK_SH to LOCK_EX (non-blocking).
//   - If we get LOCK_EX: no other test processes hold the shared lock, so we're
//     the last user. Read the PID file to find and kill the server.
//   - If LOCK_EX fails (EWOULDBLOCK): another process still holds LOCK_SH,
//     meaning it's actively using the server. Skip cleanup — the last process
//     to exit will handle it.
//
// The PID file enables any last-exiting process to clean up, not just the
// process that originally started the server. This prevents leaked servers
// when the starter exits before other consumers.
func cleanupSharedServer() {
	// Release our shared lock regardless.
	defer func() {
		if doltLockFile != nil {
			_ = syscall.Flock(int(doltLockFile.Fd()), syscall.LOCK_UN)
			_ = doltLockFile.Close()
			doltLockFile = nil
		}
		// Clear BEADS_DOLT_PORT / GT_DOLT_PORT if we set them, so subsequent
		// processes don't inherit a stale port.
		if doltPortSetByUs {
			_ = os.Unsetenv("GT_DOLT_PORT")
			_ = os.Unsetenv("BEADS_DOLT_PORT")
		}
	}()

	if doltLockFile == nil || sharedServer == nil {
		return
	}

	portStr := strconv.Itoa(sharedServer.Port)
	pidPath := pidFilePathForPort(portStr)

	// Try to acquire exclusive lock (non-blocking). If another process
	// holds LOCK_SH, this fails with EWOULDBLOCK — the server is still in use.
	err := syscall.Flock(int(doltLockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Another process is using the server. Don't kill it.
		return
	}
	// We got LOCK_EX — we're the last process. Kill from PID file.

	data, err := os.ReadFile(pidPath)
	if err != nil {
		// No PID file — either external server or already cleaned up.
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

	// Kill the server process.
	proc, err := os.FindProcess(pid)
	if err == nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
	}

	// Clean up data dir, PID file, and lock file.
	if dataDir != "" {
		_ = os.RemoveAll(dataDir)
	}
	_ = os.Remove(pidPath)
	_ = os.Remove(lockFilePathForPort(portStr))
}

// cleanStalePIDFiles scans PID files in /tmp for dead or orphaned test servers.
// Handles both the current prefix (beads-test-dolt-) and the legacy prefix
// (dolt-test-server-) from before the rename.
func cleanStalePIDFiles() {
	prefixes := []string{testPidPrefix, "dolt-test-server-"}
	for _, prefix := range prefixes {
		pattern := filepath.Join(testPidDir, prefix+"*.pid")
		entries, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, pidFile := range entries {
			cleanPIDFile(pidFile)
		}
		// Also clean matching .lock files that have no corresponding live server
		lockPattern := filepath.Join(testPidDir, prefix+"*.lock")
		lockEntries, _ := filepath.Glob(lockPattern)
		for _, lf := range lockEntries {
			// Only remove lock files whose PID file is already gone
			// (otherwise a live server's lock would be removed).
			pidEquiv := strings.TrimSuffix(lf, ".lock") + ".pid"
			if _, err := os.Stat(pidEquiv); os.IsNotExist(err) {
				_ = os.Remove(lf)
			}
		}
	}
}

// cleanPIDFile handles a single PID file: removes if dead, kills if alive and is dolt.
func cleanPIDFile(pidFile string) {
	data, err := os.ReadFile(pidFile) //nolint:gosec // G304: path from test cleanup, not user input
	if err != nil {
		_ = os.Remove(pidFile)
		return
	}

	// PID file may be in old format (just PID) or new format (PID\nDATA_DIR\n).
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 3)
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		_ = os.Remove(pidFile)
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(pidFile)
		return
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		// Process is dead — clean up stale PID file and data dir
		if len(lines) >= 2 {
			dataDir := strings.TrimSpace(lines[1])
			if dataDir != "" {
				_ = os.RemoveAll(dataDir)
			}
		}
		_ = os.Remove(pidFile)
		return
	}
	// Process is alive — verify it's a dolt server before killing
	if isDoltTestProcess(pid) {
		_ = process.Signal(syscall.SIGKILL)
		time.Sleep(100 * time.Millisecond)
		if len(lines) >= 2 {
			dataDir := strings.TrimSpace(lines[1])
			if dataDir != "" {
				_ = os.RemoveAll(dataDir)
			}
		}
	}
	_ = os.Remove(pidFile)
}

// isDoltTestProcess verifies that a PID belongs to a dolt sql-server process.
func isDoltTestProcess(pid int) bool {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	cmdline := strings.TrimSpace(string(output))
	return strings.Contains(cmdline, "dolt") && strings.Contains(cmdline, "sql-server")
}
