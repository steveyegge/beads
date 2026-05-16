//go:build unix

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
)

const (
	bddSpawnHardTimeout  = 2 * time.Minute
	bddSpawnPollInterval = 100 * time.Millisecond
)

// GetCreateDaemonEndpoint returns the Unix socket path of the running bdd
// daemon, spawning one if none is running. It is analogous to
// GetCreateDatabaseProxyServerEndpoint for TCP proxies.
func GetCreateDaemonEndpoint(beadsDir string, cfg *configfile.Config) (string, error) {
	timeout := time.NewTimer(bddSpawnHardTimeout)
	defer timeout.Stop()
	poll := time.NewTicker(bddSpawnPollInterval)
	defer poll.Stop()

	deadline := time.Now().Add(bddSpawnHardTimeout)
	var lastSpawnErr error

	for {
		if sock, ok := probeDaemonSocket(beadsDir); ok {
			return sock, nil
		}

		lock, err := util.TryLock(filepath.Join(beadsDir, bddLockFile))
		switch {
		case err == nil:
			sock, spawnErr := spawnAndWaitDaemon(beadsDir, cfg, deadline, lock)
			if spawnErr != nil {
				lastSpawnErr = spawnErr
			} else {
				return sock, nil
			}
		case !lockfile.IsLocked(err):
			return "", fmt.Errorf("bdd: probe lock: %w", err)
			// else: lock held by a spawning child; fall through to poll
		}

		select {
		case <-timeout.C:
			if lastSpawnErr != nil {
				return "", lastSpawnErr
			}
			return "", fmt.Errorf("bdd: timeout waiting for daemon socket in %s", beadsDir)
		case <-poll.C:
		}
	}
}

// probeDaemonSocket reads bdd.pid and dials the socket; returns ("", false) if
// the daemon is not running or the socket is not dialable.
func probeDaemonSocket(beadsDir string) (string, bool) {
	pf, err := pidfile.Read(beadsDir, bddPIDFile)
	if err != nil || pf == nil || pf.SocketPath == "" {
		return "", false
	}
	conn, err := net.DialTimeout("unix", pf.SocketPath, 500*time.Millisecond)
	if err != nil {
		return "", false
	}
	_ = conn.Close()
	return pf.SocketPath, true
}

// spawnAndWaitDaemon forks a daemon-child process and waits until its socket
// is dialable. lock must be the caller's bdd.lock; it is handed off to
// forkExecDaemonChild (which releases it before cmd.Start).
func spawnAndWaitDaemon(beadsDir string, cfg *configfile.Config, deadline time.Time, lock *util.Lock) (string, error) {
	handed := false
	defer func() {
		if !handed {
			lock.Unlock()
		}
	}()

	// Remove stale pidfile so racing readers don't dial a dead socket.
	_ = pidfile.Remove(beadsDir, bddPIDFile)

	cmd, done, err := forkExecDaemonChild(beadsDir, cfg, lock)
	if err != nil {
		return "", fmt.Errorf("bdd: fork daemon child: %w", err)
	}
	handed = true

	hard := time.NewTimer(bddSpawnHardTimeout)
	defer hard.Stop()
	poll := time.NewTicker(bddSpawnPollInterval)
	defer poll.Stop()

	for {
		if sock, ok := probeDaemonSocket(beadsDir); ok {
			return sock, nil
		}
		select {
		case <-done:
			return "", fmt.Errorf("bdd: daemon child exited before socket became ready")
		case <-hard.C:
			_ = cmd.Process.Kill()
			return "", fmt.Errorf("bdd: hard timeout (%s) waiting for daemon socket", bddSpawnHardTimeout)
		case <-poll.C:
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				return "", fmt.Errorf("bdd: deadline exceeded waiting for daemon socket")
			}
		}
	}
}

// forkExecDaemonChild starts a detached daemon-child process and releases lock
// before cmd.Start so the child can acquire it. Returns the running cmd and a
// channel closed when the child exits.
func forkExecDaemonChild(beadsDir string, cfg *configfile.Config, lock *util.Lock) (*exec.Cmd, <-chan struct{}, error) {
	released := false
	defer func() {
		if !released {
			lock.Unlock()
		}
	}()

	self, err := proxy.ResolveExecutable()
	if err != nil {
		return nil, nil, fmt.Errorf("bdd: locate bd executable: %w", err)
	}

	idleTimeout := time.Duration(cfg.GetDaemonIdleSeconds()) * time.Second
	maxLifetime := time.Duration(cfg.GetDaemonMaxLifetimeSeconds()) * time.Second
	logPath := filepath.Join(beadsDir, bddLogFile)

	args := []string{
		"daemon-child",
		"--root", beadsDir,
		"--idle-timeout", idleTimeout.String(),
		"--max-lifetime", maxLifetime.String(),
	}

	// Parent opens the log with O_TRUNC (rotation on each daemon start).
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // logPath is workspace-derived, not user input
	if err != nil {
		return nil, nil, fmt.Errorf("bdd: open log %q: %w", logPath, err)
	}

	cmd := exec.Command(self, args...)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Release lock before starting child so the child can acquire it.
	released = true
	lock.Unlock()

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, nil, fmt.Errorf("bdd: start daemon child: %w", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
		_ = logFile.Close()
	}()

	return cmd, done, nil
}
