//go:build windows

package testutil

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

// DoltDockerImage is the Docker image used for Dolt test containers.
// Pinned to 1.43.0 — see testdoltserver.go for rationale.
const DoltDockerImage = "dolthub/dolt-sql-server:1.43.0"

// TestDoltServer represents a running test Dolt server instance.
// On Windows CI, Docker Desktop is not reliably available, so all
// container-based test helpers skip gracefully.
type TestDoltServer struct {
	Port int
}

// StartTestDoltServer is not supported on Windows CI.
func StartTestDoltServer(_ string) (*TestDoltServer, func()) {
	fmt.Fprintln(os.Stderr, "WARN: Docker not available on Windows CI, skipping test server")
	return nil, func() {}
}

// IsCrashed always returns false on Windows (no container to monitor).
func (s *TestDoltServer) IsCrashed() bool { return false }

// CrashError always returns nil on Windows (no container to monitor).
func (s *TestDoltServer) CrashError() error { return nil }

// StartIsolatedDoltContainer is not supported on Windows CI.
func StartIsolatedDoltContainer(t *testing.T) string {
	t.Helper()
	t.Skip("Docker not available on Windows CI")
	return ""
}

// EnsureDoltContainerForTestMain is not supported on Windows CI.
func EnsureDoltContainerForTestMain() error {
	return fmt.Errorf("Docker not available on Windows CI")
}

// RequireDoltContainer is not supported on Windows CI.
func RequireDoltContainer(t *testing.T) {
	t.Helper()
	t.Skip("Docker not available on Windows CI")
}

// DoltContainerAddr returns empty string on Windows.
func DoltContainerAddr() string { return "" }

// DoltContainerPort returns empty string on Windows.
func DoltContainerPort() string { return "" }

// TerminateDoltContainer is a no-op on Windows.
func TerminateDoltContainer() {}

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
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
