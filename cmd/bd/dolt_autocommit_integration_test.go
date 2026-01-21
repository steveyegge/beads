//go:build integration
// +build integration

package main

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func doltHeadCommit(t *testing.T, dir string, env []string) string {
	t.Helper()
	out, err := runBDExecAllowErrorWithEnv(t, dir, env, "--json", "vc", "status")
	if err != nil {
		t.Fatalf("bd vc status failed: %v\n%s", err, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		// Some commands can emit warnings; try from first '{'
		if idx := strings.Index(out, "{"); idx >= 0 {
			if err2 := json.Unmarshal([]byte(out[idx:]), &m); err2 != nil {
				t.Fatalf("failed to parse vc status JSON: %v\n%s", err2, out)
			}
		} else {
			t.Fatalf("failed to parse vc status JSON: %v\n%s", err, out)
		}
	}
	commit, _ := m["commit"].(string)
	if commit == "" {
		t.Fatalf("missing commit in vc status output:\n%s", out)
	}
	return commit
}

func TestDoltAutoCommit_On_WritesAdvanceHead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("dolt integration test not supported on windows")
	}

	tmpDir := createTempDirWithCleanup(t)
	setupGitRepoForIntegration(t, tmpDir)

	env := []string{
		"BEADS_TEST_MODE=1",
		"BEADS_NO_DAEMON=1",
	}

	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		if isDoltBackendUnavailable(initOut) {
			t.Skipf("dolt backend not available: %s", initOut)
		}
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	before := doltHeadCommit(t, tmpDir, env)

	// A write command should create a new Dolt commit (auto-commit default is on).
	out, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "create", "Auto-commit test", "--json")
	if err != nil {
		t.Fatalf("bd create failed: %v\n%s", err, out)
	}

	after := doltHeadCommit(t, tmpDir, env)
	if after == before {
		t.Fatalf("expected Dolt HEAD to change after write; before=%s after=%s", before, after)
	}

	// A read-only command should not create another commit.
	out, err = runBDExecAllowErrorWithEnv(t, tmpDir, env, "list")
	if err != nil {
		t.Fatalf("bd list failed: %v\n%s", err, out)
	}
	afterList := doltHeadCommit(t, tmpDir, env)
	if afterList != after {
		t.Fatalf("expected Dolt HEAD unchanged after read command; before=%s after=%s", after, afterList)
	}
}

func TestDoltAutoCommit_Off_DoesNotAdvanceHead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("dolt integration test not supported on windows")
	}

	tmpDir := createTempDirWithCleanup(t)
	setupGitRepoForIntegration(t, tmpDir)

	env := []string{
		"BEADS_TEST_MODE=1",
		"BEADS_NO_DAEMON=1",
	}

	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		if isDoltBackendUnavailable(initOut) {
			t.Skipf("dolt backend not available: %s", initOut)
		}
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	before := doltHeadCommit(t, tmpDir, env)

	// Disable auto-commit via persistent flag (must come before subcommand).
	out, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "--dolt-auto-commit", "off", "create", "Auto-commit off", "--json")
	if err != nil {
		t.Fatalf("bd create failed: %v\n%s", err, out)
	}

	after := doltHeadCommit(t, tmpDir, env)
	if after != before {
		t.Fatalf("expected Dolt HEAD unchanged with auto-commit off; before=%s after=%s", before, after)
	}
}
