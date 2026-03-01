//go:build !windows

package testutil

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql" // required by testcontainers Dolt module
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/dolt"
)

// DoltDockerImage is the Docker image used for Dolt test containers.
// Pinned to 1.43.0 because Dolt >= 1.44 has a broken auth handshake:
// root@localhost vs root@% — the go-sql-driver connects via TCP mapped port
// which maps to root@%, but only root@localhost exists. The Docker image
// does not process /docker-entrypoint-initdb.d/ scripts, so WithScripts
// can't work around it. See testdata/dolt-init.sql for the workaround that
// would work if the image supported init scripts.
// Tracked upstream with DoltHub; bump when fixed.
const DoltDockerImage = "dolthub/dolt-sql-server:1.43.0"

// TestDoltServer represents a running test Dolt server instance.
type TestDoltServer struct {
	Port      int
	container *dolt.DoltContainer
}

// serverStartTimeout is the max time to wait for the test Dolt server to accept connections.
const serverStartTimeout = 60 * time.Second

// Module-level singleton state.
// Note: doltServerOnce is shared between StartTestDoltServer,
// EnsureDoltContainerForTestMain, and RequireDoltContainer.
// Callers should use one entry point per binary, not mix them.
var (
	doltServerOnce    sync.Once
	doltServerErr     error
	doltTestPort      string
	doltSingletonSrv  *TestDoltServer
	doltTerminateOnce sync.Once
	dockerOnce        sync.Once
	dockerAvail       bool
)

// isDockerAvailable returns true if the Docker daemon is reachable.
// The result is cached after the first call.
func isDockerAvailable() bool {
	dockerOnce.Do(func() {
		dockerAvail = exec.Command("docker", "info").Run() == nil
	})
	return dockerAvail
}

// StartTestDoltServer starts a Dolt SQL server in a Docker container on a
// dynamic port. Uses testcontainers-go for clean lifecycle management.
//
// If BEADS_DOLT_PORT is already set in the environment (e.g. by an outer test
// runner or scripts/test.sh with BEADS_TEST_SHARED_SERVER=1), the existing
// server is reused and cleanup is a no-op.
//
// tmpDirPrefix is kept for API compatibility but is unused (containers manage
// their own storage).
// Returns the server (nil if Docker not available) and a cleanup function.
func StartTestDoltServer(tmpDirPrefix string) (*TestDoltServer, func()) {
	// Reuse existing server if BEADS_DOLT_PORT is already set by an outer runner.
	//
	// FIREWALL: Never reuse the production Dolt server (port 3307) for tests.
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

	// Singleton: start at most one container per test binary.
	doltServerOnce.Do(func() {
		if !isDockerAvailable() {
			fmt.Fprintf(os.Stderr, "WARN: Docker not available, skipping test server\n")
			return
		}

		doltServerErr = startDoltContainer()
	})

	if doltServerErr != nil {
		fmt.Fprintf(os.Stderr, "WARN: test Dolt container failed to start: %v\n", doltServerErr)
		return nil, func() {}
	}
	if doltSingletonSrv == nil {
		return nil, func() {}
	}

	return doltSingletonSrv, func() {
		terminateSharedContainer()
	}
}

// startDoltContainer starts the singleton Dolt container.
func startDoltContainer() error {
	ctx, cancel := context.WithTimeout(context.Background(), serverStartTimeout)
	defer cancel()

	ctr, err := dolt.Run(ctx, DoltDockerImage,
		dolt.WithDatabase("beads_test"),
	)
	if err != nil {
		return fmt.Errorf("starting Dolt container: %w", err)
	}

	p, err := ctr.MappedPort(ctx, "3306/tcp")
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return fmt.Errorf("getting mapped port: %w", err)
	}

	port, err := strconv.Atoi(p.Port())
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return fmt.Errorf("parsing port %q: %w", p.Port(), err)
	}

	doltTestPort = p.Port()
	doltSingletonSrv = &TestDoltServer{
		Port:      port,
		container: ctr,
	}

	return nil
}

// terminateSharedContainer stops and removes the shared Dolt container.
// Safe to call concurrently or multiple times (sync.Once).
func terminateSharedContainer() {
	doltTerminateOnce.Do(func() {
		if doltSingletonSrv != nil && doltSingletonSrv.container != nil {
			_ = testcontainers.TerminateContainer(doltSingletonSrv.container)
			doltSingletonSrv.container = nil
		}
	})
}

// IsCrashed returns true if the container has exited unexpectedly.
// Returns false for reused servers (BEADS_DOLT_PORT) where we don't own the container.
func (s *TestDoltServer) IsCrashed() bool {
	if s == nil || s.container == nil {
		return false
	}
	state, err := s.container.State(context.Background())
	if err != nil {
		return true // can't check state — assume crashed
	}
	return !state.Running
}

// CrashError returns an error if the container has exited unexpectedly, nil otherwise.
func (s *TestDoltServer) CrashError() error {
	if s == nil || s.container == nil {
		return nil
	}
	state, err := s.container.State(context.Background())
	if err != nil {
		return fmt.Errorf("failed to check container state: %w", err)
	}
	if !state.Running {
		return fmt.Errorf("Dolt container exited (status=%s, exit=%d)", state.Status, state.ExitCode)
	}
	return nil
}

// --- New container-native API (matches gastown pattern) ---

// StartIsolatedDoltContainer starts a per-test Dolt container and returns the
// mapped host port. The container is terminated automatically when the test finishes.
func StartIsolatedDoltContainer(t *testing.T) string {
	t.Helper()
	if !isDockerAvailable() {
		t.Skip("Docker not available, skipping test")
	}

	ctx := context.Background()
	ctr, err := dolt.Run(ctx, DoltDockerImage,
		dolt.WithDatabase("beads_test"),
	)
	if err != nil {
		t.Fatalf("starting Dolt container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Logf("terminating Dolt container: %v", err)
		}
	})

	port, err := ctr.MappedPort(ctx, "3306/tcp")
	if err != nil {
		t.Fatalf("getting mapped port: %v", err)
	}

	portStr := port.Port()
	t.Setenv("BEADS_DOLT_PORT", portStr)
	return portStr
}

// ensureSharedContainer starts the singleton container and sets BEADS_DOLT_PORT.
func ensureSharedContainer() {
	doltServerOnce.Do(func() {
		doltServerErr = startDoltContainer()
		if doltServerErr == nil && doltTestPort != "" {
			if err := os.Setenv("BEADS_DOLT_PORT", doltTestPort); err != nil {
				doltServerErr = fmt.Errorf("set BEADS_DOLT_PORT: %w", err)
			}
		}
	})
}

// EnsureDoltContainerForTestMain starts a shared Dolt container for use in
// TestMain functions. Call TerminateDoltContainer() after m.Run() to clean up.
// Sets BEADS_DOLT_PORT process-wide.
func EnsureDoltContainerForTestMain() error {
	if !isDockerAvailable() {
		return fmt.Errorf("Docker not available")
	}

	ensureSharedContainer()
	return doltServerErr
}

// RequireDoltContainer ensures a shared Dolt container is running. Skips the
// test if Docker is not available.
func RequireDoltContainer(t *testing.T) {
	t.Helper()
	if !isDockerAvailable() {
		t.Skip("Docker not available, skipping test")
	}

	ensureSharedContainer()
	if doltServerErr != nil {
		t.Fatalf("Dolt container setup failed: %v", doltServerErr)
	}
}

// DoltContainerAddr returns the address (host:port) of the Dolt container.
func DoltContainerAddr() string {
	return "127.0.0.1:" + doltTestPort
}

// DoltContainerPort returns the mapped host port of the Dolt container.
func DoltContainerPort() string {
	return doltTestPort
}

// TerminateDoltContainer stops and removes the shared Dolt container.
// Called from TestMain after m.Run().
func TerminateDoltContainer() {
	terminateSharedContainer()
}

// --- Utilities ---

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
