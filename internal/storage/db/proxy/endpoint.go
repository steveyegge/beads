package proxy

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage/db/util"
)

const lockFileName = "proxy.lock"

type Endpoint struct {
	Host string
	Port int
}

func (e Endpoint) Address() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

type OpenOpts struct {
	IdleTimeout    time.Duration
	Backend        Backend
	ConfigFilePath string
	LogFilePath    string
	DoltBinPath    string
}

const (
	openDeadline          = 15 * time.Second
	spawnReadyHardTimeout = 2 * time.Minute
	openPollInterval      = 100 * time.Millisecond
)

func PickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

func GetCreateDatabaseProxyServerEndpoint(rootDir string, opts OpenOpts) (Endpoint, error) {
	if err := opts.Backend.Validate(); err != nil {
		return Endpoint{}, fmt.Errorf("OpenOpts.Backend: %w", err)
	}
	if opts.Backend == BackendLocalServer {
		if opts.ConfigFilePath == "" {
			return Endpoint{}, fmt.Errorf("OpenOpts.ConfigFilePath is required for backend %q", opts.Backend)
		}
		if opts.LogFilePath == "" {
			return Endpoint{}, fmt.Errorf("OpenOpts.LogFilePath is required for backend %q", opts.Backend)
		}
		if opts.DoltBinPath == "" {
			return Endpoint{}, fmt.Errorf("OpenOpts.DoltBinPath is required for backend %q", opts.Backend)
		}
	}
	deadline := time.Now().Add(openDeadline)

	timeout := time.NewTimer(openDeadline)
	defer timeout.Stop()
	poll := time.NewTicker(openPollInterval)
	defer poll.Stop()

	var lastSpawnErr error
	for {
		if ep, ok := readAndDial(rootDir); ok {
			return ep, nil
		}

		// unlocked prior to spawn of child process
		lock, err := util.TryLock(filepath.Join(rootDir, lockFileName))
		switch {
		case err == nil:
			var ep Endpoint
			if ep, lastSpawnErr = spawnAndHandoff(rootDir, opts, deadline, lock); lastSpawnErr == nil {
				return ep, nil
			}
		case !lockfile.IsLocked(err):
			return Endpoint{}, fmt.Errorf("probe proxy lock: %w", err)
		}

		select {
		case <-timeout.C:
			if lastSpawnErr != nil {
				return Endpoint{}, lastSpawnErr
			}
			return Endpoint{}, fmt.Errorf("timeout waiting for proxy on %s", rootDir)
		case <-poll.C:
		}
	}
}

func spawnAndHandoff(rootDir string, opts OpenOpts, deadline time.Time, lock *util.Lock) (Endpoint, error) {
	handedOff := false
	defer func() {
		if !handedOff {
			lock.Unlock()
		}
	}()

	// Stale pidfile from a previous (now-dead) proxy must not mislead racing
	// readers into dialing a port that nobody is listening on.
	_ = RemoveDatabaseProxyPidFile(rootDir)

	port, err := PickFreePort()
	if err != nil {
		return Endpoint{}, fmt.Errorf("pick port: %w", err)
	}

	handedOff = true
	cmd, done, err := forkExecChild(rootDir, opts.ConfigFilePath, opts.LogFilePath, opts.DoltBinPath, port, opts.IdleTimeout, opts.Backend, lock)
	if err != nil {
		return Endpoint{}, fmt.Errorf("fork child: %w", err)
	}

	hard := time.NewTimer(spawnReadyHardTimeout)
	defer hard.Stop()
	poll := time.NewTicker(openPollInterval)
	defer poll.Stop()

	for {
		if ep, ok := readAndDial(rootDir); ok {
			return ep, nil
		}
		select {
		case <-done:
			return Endpoint{}, fmt.Errorf("proxy child on port %d exited before becoming ready (likely lost lock race)", port)
		case <-hard.C:
			_ = cmd.Process.Kill()
			return Endpoint{}, fmt.Errorf("hard timeout (%s) waiting for proxy on port %d", spawnReadyHardTimeout, port)
		case <-poll.C:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return Endpoint{}, fmt.Errorf("timeout waiting for proxy to become ready on port %d", port)
		}
	}
}

func forkExecChild(rootDir, configFilePath, logFilePath, doltBinPath string, port int, idleTimeout time.Duration, backend Backend, lock *util.Lock) (*exec.Cmd, <-chan struct{}, error) {
	released := false
	defer func() {
		if !released {
			lock.Unlock()
		}
	}()

	self, err := os.Executable()
	if err != nil {
		return nil, nil, fmt.Errorf("locate bd executable: %w", err)
	}

	if idleTimeout < 0 {
		idleTimeout = 0
	}

	args := []string{
		"db-proxy-child",
		"--root", rootDir,
		"--port", strconv.Itoa(port),
		"--idle-timeout", idleTimeout.String(),
		"--backend", string(backend),
	}
	if configFilePath != "" {
		args = append(args, "--config", configFilePath)
	}
	if logFilePath != "" {
		args = append(args, "--logpath", logFilePath)
	}
	if doltBinPath != "" {
		args = append(args, "--dolt-bin", doltBinPath)
	}

	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: logFilePath is caller-derived (workspace path), not user-request input
	if err != nil {
		return nil, nil, fmt.Errorf("open log file %q: %w", logFilePath, err)
	}

	cmd := exec.Command(self, args...)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = procAttrDetached()

	released = true
	lock.Unlock()

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, nil, fmt.Errorf("start proxy child: %w", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
		_ = logFile.Close()
	}()

	return cmd, done, nil
}

func readAndDial(rootDir string) (Endpoint, bool) {
	pf, err := ReadDatabaseProxyPidFile(rootDir)
	if err != nil || pf == nil {
		return Endpoint{}, false
	}
	ep := Endpoint{Host: "127.0.0.1", Port: pf.Port}
	if !probePort(ep, 500*time.Millisecond) {
		return Endpoint{}, false
	}
	return ep, true
}

func probePort(ep Endpoint, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", ep.Address(), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
