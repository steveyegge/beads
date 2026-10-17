package dolt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCLIExecTimeoutDuration verifies BEADS_CLI_TRANSFER_TIMEOUT parsing:
// valid durations and bare seconds are honored, unset/invalid/non-positive
// values fall back to the compiled-in default.
func TestCLIExecTimeoutDuration(t *testing.T) {
	t.Run("duration string", func(t *testing.T) {
		t.Setenv(cliExecTimeoutEnv, "20m")
		if got := cliExecTimeoutDuration(); got != 20*time.Minute {
			t.Fatalf("cliExecTimeoutDuration() = %v, want 20m", got)
		}
	})
	t.Run("bare seconds", func(t *testing.T) {
		t.Setenv(cliExecTimeoutEnv, "90")
		if got := cliExecTimeoutDuration(); got != 90*time.Second {
			t.Fatalf("cliExecTimeoutDuration() = %v, want 90s", got)
		}
	})
	t.Run("unset falls back", func(t *testing.T) {
		t.Setenv(cliExecTimeoutEnv, "")
		if got := cliExecTimeoutDuration(); got != cliExecTimeout {
			t.Fatalf("cliExecTimeoutDuration() = %v, want default %v", got, cliExecTimeout)
		}
	})
	t.Run("invalid falls back", func(t *testing.T) {
		t.Setenv(cliExecTimeoutEnv, "soon")
		if got := cliExecTimeoutDuration(); got != cliExecTimeout {
			t.Fatalf("cliExecTimeoutDuration() = %v, want default %v", got, cliExecTimeout)
		}
	})
	t.Run("non-positive falls back", func(t *testing.T) {
		t.Setenv(cliExecTimeoutEnv, "-5s")
		if got := cliExecTimeoutDuration(); got != cliExecTimeout {
			t.Fatalf("cliExecTimeoutDuration() = %v, want default %v", got, cliExecTimeout)
		}
	})
}

// TestPrepareDoltCLITransferCommandSetsDeadlineAndWaitDelay pins the two
// guards against indefinite transfer hangs: the returned context carries the
// configured deadline, and the command has a non-zero WaitDelay so Wait cannot
// block forever on inherited pipes after a context kill.
func TestPrepareDoltCLITransferCommandSetsDeadlineAndWaitDelay(t *testing.T) {
	cmd, ctx, cancel := prepareDoltCLITransferCommand(context.Background(), t.TempDir(), nil, false, "push", "origin", "main")
	defer cancel()
	if cmd.WaitDelay == 0 {
		t.Fatal("cmd.WaitDelay = 0; a grandchild holding the output pipes would block Wait forever after a context kill")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("returned context has no deadline; transfer would be unbounded")
	}
	if remaining := time.Until(deadline); remaining > cliExecTimeout {
		t.Fatalf("deadline %v from now exceeds cliExecTimeout %v", remaining, cliExecTimeout)
	}
}

// TestCLITransferTimeoutUnblocksDespiteGrandchildPipes reproduces the wy-5f2u7
// hang shape: the direct child is killed at the context deadline, but a
// grandchild (stand-in for a cloud credential helper) still holds the
// inherited stdout/stderr pipes. Without WaitDelay, CombinedOutput blocks
// until the grandchild exits (60s here); with it, the call returns promptly.
func TestCLITransferTimeoutUnblocksDespiteGrandchildPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fake")
	}
	binDir := t.TempDir()
	// Fake dolt: background a pipe-holding grandchild, then hang forever.
	script := "#!/bin/sh\nsleep 60 &\nexec sleep 60\n"
	if err := os.WriteFile(filepath.Join(binDir, "dolt"), []byte(script), 0o755); err != nil { // #nosec G306 -- test fixture must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(cliExecTimeoutEnv, "1s")

	cmd, ctx, cancel := prepareDoltCLITransferCommand(context.Background(), binDir, nil, false, "push", "gcs", "main")
	defer cancel()
	cmd.WaitDelay = 500 * time.Millisecond // keep the test fast; default is cliExecWaitDelay

	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("CombinedOutput succeeded; want kill after timeout (out=%q)", out)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("CombinedOutput blocked %v despite 1s timeout + WaitDelay; pipe-holding grandchild kept Wait stuck", elapsed)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("ctx.Err() = %v, want DeadlineExceeded", ctx.Err())
	}
	msg := cliTransferError("dolt push", "gcs", ctx, out, err).Error()
	if !strings.Contains(msg, "timed out after 1s") || !strings.Contains(msg, cliExecTimeoutEnv) {
		t.Fatalf("timeout error not actionable: %q", msg)
	}
}

// TestCLITransferErrorPlainFailure pins that a non-timeout failure keeps the
// ordinary error shape and does not claim a timeout.
func TestCLITransferErrorPlainFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	msg := cliTransferError("dolt push", "gcs", ctx, []byte("remote not found"), errors.New("exit status 1")).Error()
	if strings.Contains(msg, "timed out") {
		t.Fatalf("non-timeout failure misreported as timeout: %q", msg)
	}
	if !strings.Contains(msg, "dolt push failed: remote not found") {
		t.Fatalf("unexpected error shape: %q", msg)
	}
}

// TestCLITransferErrorRedactsRemoteCredentials reproduces the leak: a rejected
// push to a credential-bearing git+https remote makes dolt echo the full URL,
// userinfo included, and the wrapped error carried it into terminal and agent
// logs. Host, ref and cause must survive; username and token must not.
func TestCLITransferErrorRedactsRemoteCredentials(t *testing.T) {
	const (
		user  = "x-access-token"
		token = "SYNTHETIC-fixture-token-7f3a9c"
	)
	remote := "git+https://" + user + ":" + token + "@github.com/example/repo"
	out := []byte(" ! [rejected]        main -> main (non-fast-forward)\n" +
		"error: failed to push some refs to '" + remote + "'\n" +
		"hint: Updates were rejected because the tip of your current branch is behind\n")
	runErr := errors.New("exit status 1")

	assertRedacted := func(t *testing.T, err error) {
		t.Helper()
		msg := err.Error()
		for _, secret := range []string{user, token} {
			if strings.Contains(msg, secret) {
				t.Errorf("error leaks %q: %s", secret, msg)
			}
		}
		for _, kept := range []string{"github.com/example/repo", "non-fast-forward", "failed to push some refs", "exit status 1"} {
			if !strings.Contains(msg, kept) {
				t.Errorf("error lost actionable detail %q: %s", kept, msg)
			}
		}
	}

	t.Run("ordinary rejection", func(t *testing.T) {
		assertRedacted(t, cliTransferError("dolt push", remote, context.Background(), out, runErr))
	})
	t.Run("deadline exceeded", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		assertRedacted(t, cliTransferError("dolt pull", remote, ctx, out, runErr))
	})
	t.Run("credential-free output is unchanged", func(t *testing.T) {
		plain := []byte("error: failed to push some refs to 'https://github.com/example/repo'\n")
		err := cliTransferError("dolt push", "https://github.com/example/repo", context.Background(), plain, runErr)
		if !strings.Contains(err.Error(), "'https://github.com/example/repo'") {
			t.Errorf("credential-free URL was altered: %s", err)
		}
	})
}

// TestRedactURLCredentials pins the per-scheme rule: HTTP(S) userinfo is
// transport credentials and goes whole; SSH userinfo names the account and
// loses only a password; text without userinfo is untouched.
func TestRedactURLCredentials(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"git+https token", "git+https://x-access-token:SECRET@github.com/o/r", "git+https://github.com/o/r"},
		{"https user only", "https://SECRET@github.com/o/r.git", "https://github.com/o/r.git"},
		{"ssh keeps account", "ssh://git:SECRET@host.example/o/r", "ssh://git@host.example/o/r"},
		{"git+ssh keeps account", "git+ssh://git@host.example/o/r", "git+ssh://git@host.example/o/r"},
		{"scp-style untouched", "git@github.com:o/r.git", "git@github.com:o/r.git"},
		{"no userinfo untouched", "https://github.com/o/r@v1", "https://github.com/o/r@v1"},
		{"quoted in prose", "failed to push some refs to 'https://u:SECRET@h.example/r'", "failed to push some refs to 'https://h.example/r'"},
		{"two urls", "a https://u:S1@h1/x b https://u:S2@h2/y", "a https://h1/x b https://h2/y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactURLCredentials(tt.in); got != tt.want {
				t.Fatalf("redactURLCredentials(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
