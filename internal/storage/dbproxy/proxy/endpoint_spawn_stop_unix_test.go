//go:build unix

package proxy

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spawnStopFailAfter only detects a hung start or Shutdown. It is far above
// shutdownSpawnWaitDeadline's components on purpose: a loaded -race runner
// can take seconds to fork/exec the child, and that must not fail the test.
const spawnStopFailAfter = 60 * time.Second

// holdStartBeforeExec makes the next proxy start block in its
// release-lock-before-exec window. entered closes once the start is parked
// there with its spawn marker published; closing release lets it exec.
func holdStartBeforeExec(t *testing.T, root string) (entered <-chan struct{}, release chan<- struct{}) {
	t.Helper()
	childPath := filepath.Join(root, "exit-child.sh")
	require.NoError(t, os.WriteFile(childPath, []byte("#!/bin/sh\nexit 0\n"), 0o700))

	previousResolve := ResolveExecutable
	previousHook := beforeProxyChildStart
	ResolveExecutable = func() (string, error) { return childPath, nil }
	enteredCh := make(chan struct{})
	releaseCh := make(chan struct{})
	var once sync.Once
	beforeProxyChildStart = func() {
		once.Do(func() {
			close(enteredCh)
			<-releaseCh
		})
	}
	t.Cleanup(func() {
		ResolveExecutable = previousResolve
		beforeProxyChildStart = previousHook
	})
	return enteredCh, releaseCh
}

// runShutdownDuringHeldStart parks a start before exec, runs Shutdown
// concurrently, holds the window for hold, then lets the start proceed and
// asserts the start aborts and Shutdown succeeds without leaving a marker.
func runShutdownDuringHeldStart(t *testing.T, hold time.Duration) {
	t.Helper()
	root := t.TempDir()
	entered, release := holdStartBeforeExec(t, root)

	startDone := make(chan error, 1)
	go func() {
		_, err := GetCreateDatabaseProxyServerEndpoint(root, externalOpenOpts(root))
		startDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(spawnStopFailAfter):
		t.Fatal("start did not reach injected pre-exec delay")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- Shutdown(root) }()
	select {
	case err := <-stopDone:
		t.Fatalf("Shutdown returned during delayed start: %v", err)
	case <-time.After(hold):
		// The injected hook, rather than scheduler timing, holds the exact
		// historical release-lock-before-Start window open.
	}

	close(release)
	select {
	case err := <-startDone:
		require.Error(t, err)
		assert.True(t, errors.Is(err, errStartInterrupted), "start error = %v", err)
	case <-time.After(spawnStopFailAfter):
		t.Fatal("start did not terminate after concurrent shutdown")
	}
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(spawnStopFailAfter):
		t.Fatal("Shutdown did not finish after delayed start resolved")
	}

	assert.NoFileExists(t, filepath.Join(root, spawnMarkerFileName))
}

func TestShutdownWaitsForStartDelayedBeforeExec(t *testing.T) {
	runShutdownDuringHeldStart(t, 150*time.Millisecond)
}

// TestForkExecChild_RotatesOversizedLogs pins the spawn-side rotation call:
// while the passed-in proxy.lock is still held and before the child's
// stdout/stderr fd is opened, forkExecChild rotates both the proxied
// server.log and proxy.log past the BEADS_DOLT_LOG_MAX_BYTES ceiling, so
// the rename cannot race an open writer (#7527).
func TestForkExecChild_RotatesOversizedLogs(t *testing.T) {
	// Not parallel: t.Setenv + ResolveExecutable stub.
	t.Setenv(doltserver.EnvMaxLogBytes, "1024")
	root := t.TempDir()
	childPath := filepath.Join(root, "exit-child.sh")
	require.NoError(t, os.WriteFile(childPath, []byte("#!/bin/sh\nexit 0\n"), 0o700))
	previousResolve := ResolveExecutable
	ResolveExecutable = func() (string, error) { return childPath, nil }
	t.Cleanup(func() { ResolveExecutable = previousResolve })

	proxyLog := filepath.Join(root, LogFileName)
	serverLog := filepath.Join(root, "server.log") // distinct from proxy.log
	require.NoError(t, os.WriteFile(proxyLog, bytes.Repeat([]byte("old-proxy\n"), 200), 0o600))
	require.NoError(t, os.WriteFile(serverLog, bytes.Repeat([]byte("old-server\n"), 200), 0o600))

	lock, err := util.TryLock(filepath.Join(root, LockFileName))
	require.NoError(t, err)

	opts := OpenOpts{
		Backend:        BackendLocalServer,
		ConfigFilePath: filepath.Join(root, "config.yaml"),
		LogFilePath:    serverLog,
		DoltBinPath:    "dolt",
	}
	child, err := forkExecChild(root, opts, 0, "", lock)
	require.NoError(t, err)
	defer func() {
		if child.handle != nil {
			_ = child.handle.Close()
		}
	}()

	select {
	case childErr := <-child.done:
		require.NoError(t, childErr)
	case <-time.After(spawnStopFailAfter):
		t.Fatal("spawned child did not exit")
	}

	rotated, err := os.ReadFile(proxyLog + ".1")
	require.NoError(t, err, "oversized proxy.log should have been rotated to proxy.log.1")
	assert.Contains(t, string(rotated), "old-proxy")
	rotated, err = os.ReadFile(serverLog + ".1")
	require.NoError(t, err, "oversized server.log should have been rotated to server.log.1")
	assert.Contains(t, string(rotated), "old-server")

	// The fresh primary is where the child's stdout/stderr fd points.
	if _, err := os.Stat(serverLog); err != nil {
		t.Fatalf("fresh server.log not created after rotation: %v", err)
	}
}

// A live starter that is slow to exec (loaded host, -race) must be waited
// out, not reported as "proxy left running" once shutdownConfirmDeadline
// elapses: the start is still making progress and will abort on the advanced
// stop epoch.
func TestShutdownWaitsForLiveStarterPastConfirmDeadline(t *testing.T) {
	runShutdownDuringHeldStart(t, shutdownConfirmDeadline+time.Second)
}
