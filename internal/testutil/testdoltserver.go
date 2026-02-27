package testutil

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const testPidDir = "/tmp"
const testPidPrefix = "beads-test-dolt-"

// TestDoltServer represents a running test dolt server instance.
type TestDoltServer struct {
	Port     int
	cmd      *exec.Cmd
	tmpDir   string
	pidFile  string
	crashed  chan struct{} // closed when server exits unexpectedly
	exitErr  error        // set before crashed is closed
	exitOnce sync.Once
}

// serverStartTimeout is the max time to wait for the test dolt server to accept connections.
const serverStartTimeout = 30 * time.Second

// maxPortRetries is how many times to retry port allocation + server start on port conflict.
const maxPortRetries = 3

// Singleton state: sync.Once ensures only one goroutine in the process starts
// the server. Cross-process coordination (flock on unix) is handled in the
// platform-specific startDoltServerInner / cleanupSharedServer functions.
var (
	doltServerOnce sync.Once
	doltServerErr  error
	sharedServer   *TestDoltServer
	// doltLockFile is held with LOCK_SH (unix) for the lifetime of the test
	// binary. This prevents other test processes from killing the server while
	// we're still using it. See cleanupSharedServer for the shutdown protocol.
	doltLockFile *os.File
	// doltWeStarted tracks whether this process started the server (vs reusing).
	doltWeStarted bool
	// doltPortSetByUs tracks whether we set BEADS_DOLT_PORT (vs it being set externally).
	doltPortSetByUs bool
)

// StartTestDoltServer starts a dedicated Dolt SQL server in a temp directory
// on a dynamic port. Uses a process-level singleton (sync.Once) to ensure only
// one server per test binary, and cross-process file locking (flock on unix) to
// coordinate across concurrent test binaries.
//
// If BEADS_DOLT_PORT is already set in the environment (e.g. by an outer test
// runner or scripts/test.sh with BEADS_TEST_SHARED_SERVER=1), the existing
// server is reused and cleanup is a no-op.
//
// tmpDirPrefix is the os.MkdirTemp prefix (e.g. "beads-test-dolt-*").
// Returns the server (nil if dolt not installed) and a cleanup function.
func StartTestDoltServer(tmpDirPrefix string) (*TestDoltServer, func()) {
	// Reuse existing server if BEADS_DOLT_PORT is already set by an outer runner.
	// This avoids spawning redundant dolt processes when running go test ./...
	// with a pre-started shared server.
	//
	// FIREWALL: Never reuse the production Dolt server (port 3307) for tests.
	// This guard does NOT depend on BEADS_TEST_MODE — that env var may not be set yet
	// when TestMain calls StartTestDoltServer (it's set AFTER this returns).
	// Clown Shows #12-#18: every time this guard had a hole, production got polluted.
	if port := os.Getenv("BEADS_DOLT_PORT"); port != "" {
		p, err := strconv.Atoi(port)
		if err == nil && p == 3307 {
			// Port 3307 is ALWAYS production. Never reuse it, regardless of BEADS_TEST_MODE.
			fmt.Fprintf(os.Stderr, "WARN: BEADS_DOLT_PORT=%d is production — starting isolated test server\n", p)
		} else if err == nil && WaitForServer(p, 2*time.Second) {
			return &TestDoltServer{Port: p}, func() {}
		} else {
			fmt.Fprintf(os.Stderr, "WARN: BEADS_DOLT_PORT=%s set but server not reachable, starting new server\n", port)
		}
	}

	if _, err := exec.LookPath("dolt"); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: dolt not found in PATH, skipping test server\n")
		return nil, func() {}
	}

	// sync.Once: only one goroutine in this process attempts startup.
	// startDoltServerInner handles cross-process coordination (flock on unix).
	doltServerOnce.Do(func() {
		doltServerErr = startDoltServerInner(tmpDirPrefix)
	})

	if doltServerErr != nil {
		fmt.Fprintf(os.Stderr, "WARN: dolt server setup failed: %v\n", doltServerErr)
		return nil, func() {}
	}

	if sharedServer == nil {
		return nil, func() {}
	}

	// Return a reference to the shared server. The cleanup function calls
	// the platform-specific cleanupSharedServer which uses flock (on unix)
	// to determine if we're the last user before killing the server.
	return sharedServer, func() {
		cleanupSharedServer()
	}
}

// IsCrashed returns true if the server process has exited unexpectedly.
// Returns false for reused servers (BEADS_DOLT_PORT) where we don't own the process.
func (s *TestDoltServer) IsCrashed() bool {
	if s == nil || s.crashed == nil {
		return false
	}
	select {
	case <-s.crashed:
		return true
	default:
		return false
	}
}

// CrashError returns the server's exit error if it crashed, nil otherwise.
func (s *TestDoltServer) CrashError() error {
	if s == nil || s.crashed == nil {
		return nil
	}
	select {
	case <-s.crashed:
		return s.exitErr
	default:
		return nil
	}
}

// CleanStaleTestServers kills orphaned test dolt servers from previous
// interrupted test runs by scanning PID files in /tmp, and removes
// orphaned temp directories left behind by crashed tests.
func CleanStaleTestServers() {
	cleanStalePIDFiles()
	cleanOrphanedTempDirs()
}

// lockFilePathForPort returns the lock file path for a given port.
// Port-specific paths prevent contention between test binaries using different ports.
func lockFilePathForPort(port string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("beads-test-dolt-%s.lock", port))
}

// pidFilePathForPort returns the PID file path for a given port.
func pidFilePathForPort(port string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("beads-test-dolt-%s.pid", port))
}

// cleanOrphanedTempDirs removes test temp directories whose owning server
// process is no longer running. Covers all prefixes used by StartTestDoltServer
// callers and test working dirs in the system temp directory.
func cleanOrphanedTempDirs() {
	tmpDir := os.TempDir()
	for _, prefix := range []string{
		// Server data dirs (one per StartTestDoltServer caller)
		"beads-test-dolt-",
		"beads-root-test-",
		"beads-integration-test-",
		"bd-regression-dolt-",
		"tracker-pkg-test-",
		"dolt-pkg-test-",
		"molecules-pkg-test-",
		"doctor-test-dolt-",
		"fix-test-dolt-",
		"protocol-test-dolt-",
		// Test working dirs
		"beads-bd-tests-",
		// Legacy prefix (no longer created)
		"dolt-test-server-",
	} {
		pattern := filepath.Join(tmpDir, prefix+"*")
		entries, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			info, err := os.Stat(entry)
			if err != nil || !info.IsDir() {
				continue
			}
			// Skip dirs modified in the last 5 minutes (may be in active use)
			if time.Since(info.ModTime()) < 5*time.Minute {
				continue
			}
			// Go module caches have read-only perms; fix before removal
			_ = filepath.Walk(entry, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if fi.IsDir() && fi.Mode()&0200 == 0 {
					_ = os.Chmod(path, fi.Mode()|0200)
				}
				return nil
			})
			_ = os.RemoveAll(entry)
		}
	}
}

// FindFreePort finds an available TCP port by binding to :0.
func FindFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}

// WaitForServer polls until the server accepts TCP connections on the given port.
func WaitForServer(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		// #nosec G704 -- addr is always loopback (127.0.0.1) with a test-selected local port.
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
