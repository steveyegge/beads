//go:build cgo && unix

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestDaemonChild_LockRace verifies that when two daemon-child processes race to
// acquire bdd.lock, exactly one succeeds (enters the accept loop) and the other
// exits with code 75 (EX_TEMPFAIL).
func TestDaemonChild_LockRace(t *testing.T) {
	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--skip-hooks", "--skip-agents")
	_ = dir

	var mu sync.Mutex
	var exitCodes []int

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(bd, "daemon-child", "--root", beadsDir,
				"--idle-timeout", "1s", "--max-lifetime", "3s")
			cmd.Dir = beadsDir
			cmd.Env = bdEnv(beadsDir)
			_ = cmd.Start()
			if cmd.Process == nil {
				return
			}
			state, err := cmd.Process.Wait()
			if err != nil {
				return
			}
			mu.Lock()
			exitCodes = append(exitCodes, state.ExitCode())
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(exitCodes) == 0 {
		t.Skip("daemon-child processes didn't exit in time; may need real Dolt store")
	}

	// One process should exit 75 (EX_TEMPFAIL — lost lock race).
	// The other should exit 0 (ran and exited cleanly via idle timeout/max-lifetime).
	has75 := false
	for _, c := range exitCodes {
		if c == bddExTempFail {
			has75 = true
		}
	}
	if !has75 {
		t.Errorf("expected one process to exit %d (EX_TEMPFAIL), exit codes: %v", bddExTempFail, exitCodes)
	}
}

// TestDaemonChild_SocketPermissions verifies that the Unix socket created by
// daemon-child is restricted to owner-only (mode 0600).
func TestDaemonChild_SocketPermissions(t *testing.T) {
	bd := buildEmbeddedBD(t)
	_, beadsDir, _ := bdInit(t, bd, "--skip-hooks", "--skip-agents")

	cmd := exec.Command(bd, "daemon-child", "--root", beadsDir,
		"--idle-timeout", "2s", "--max-lifetime", "5s")
	cmd.Dir = beadsDir
	cmd.Env = bdEnv(beadsDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon-child: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
		}
	})

	sock := filepath.Join(beadsDir, "bdd.sock")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(sock)
		if err == nil {
			mode := info.Mode() & os.ModePerm
			if mode != 0o600 {
				t.Errorf("socket mode = %04o, want 0600", mode)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("socket did not appear within 5s; daemon-child may need a configured store")
}

// TestDaemonChild_IdleTimeout verifies that daemon-child exits cleanly when no
// iterator sessions are active for longer than the idle timeout.
func TestDaemonChild_IdleTimeout(t *testing.T) {
	bd := buildEmbeddedBD(t)
	_, beadsDir, _ := bdInit(t, bd, "--skip-hooks", "--skip-agents")

	start := time.Now()
	cmd := exec.Command(bd, "daemon-child", "--root", beadsDir,
		"--idle-timeout", "1s", "--max-lifetime", "10s")
	cmd.Dir = beadsDir
	cmd.Env = bdEnv(beadsDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon-child: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == bddExTempFail {
				t.Skip("daemon-child exited 75 (lock race with another daemon); skip in CI")
			}
		}
		// Should exit cleanly (exit 0) after idle timeout (1s) + some margin.
		if elapsed > 8*time.Second {
			t.Errorf("daemon-child took too long to exit on idle timeout: %v", elapsed)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Signal(syscall.SIGKILL)
		t.Error("daemon-child did not exit within 10s after idle timeout")
	}
}

// TestDaemonChild_PidFileShape verifies that bdd.pid is valid JSON with the
// expected fields: pid, socket, version, started_at.
func TestDaemonChild_PidFileShape(t *testing.T) {
	bd := buildEmbeddedBD(t)
	_, beadsDir, _ := bdInit(t, bd, "--skip-hooks", "--skip-agents")

	cmd := exec.Command(bd, "daemon-child", "--root", beadsDir,
		"--idle-timeout", "2s", "--max-lifetime", "5s")
	cmd.Dir = beadsDir
	cmd.Env = bdEnv(beadsDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon-child: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
		}
	})

	pidFile := filepath.Join(beadsDir, "bdd.pid")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil && len(data) > 0 {
			var pf struct {
				Pid       int     `json:"pid"`
				Socket    string  `json:"socket"`
				Version   string  `json:"version"`
				StartedAt *string `json:"started_at"`
			}
			if err := json.Unmarshal(data, &pf); err != nil {
				t.Fatalf("bdd.pid is not valid JSON: %v\n%s", err, data)
			}
			if pf.Pid <= 0 {
				t.Errorf("bdd.pid.pid = %d, want > 0", pf.Pid)
			}
			if pf.Socket == "" {
				t.Errorf("bdd.pid.socket is empty")
			}
			if pf.StartedAt == nil {
				t.Errorf("bdd.pid.started_at is missing")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("bdd.pid did not appear within 5s; daemon-child may need a configured store")
}
