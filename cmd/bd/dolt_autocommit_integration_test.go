//go:build cgo && integration

package main

import (
	"encoding/json"
	"fmt"
	"os"
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

func doltHeadAuthor(t *testing.T, dir string, env []string) string {
	t.Helper()

	out, err := runBDExecAllowErrorWithEnv(t, dir, env, "sql",
		"SELECT committer, email FROM dolt_log ORDER BY date DESC LIMIT 1")
	if err != nil {
		t.Fatalf("bd sql dolt_log author query failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "|") || strings.Contains(line, "committer") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		return fmt.Sprintf("%s <%s>", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}
	t.Fatalf("missing committer/email row in bd sql output:\n%s", out)
	return ""
}

func TestDoltAutoCommit_On_WritesAdvanceHead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("dolt integration test not supported on windows")
	}

	tmpDir := newCLIIntegrationRepo(t)
	env := cliIntegrationEnv()

	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		skipIfDoltBackendUnavailable(t, initOut)
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	before := doltHeadCommit(t, tmpDir, env)

	// Explicitly enable auto-commit=on (default is now "batch" in embedded mode).
	out, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "--dolt-auto-commit", "on", "create", "Auto-commit test", "--json")
	if err != nil {
		t.Fatalf("bd create failed: %v\n%s", err, out)
	}

	after := doltHeadCommit(t, tmpDir, env)
	if after == before {
		t.Fatalf("expected Dolt HEAD to change after write; before=%s after=%s", before, after)
	}

	// Commit author should be deterministic (not the authenticated SQL user like root@%).
	expectedName := os.Getenv("GIT_AUTHOR_NAME")
	if expectedName == "" {
		expectedName = "beads"
	}
	expectedEmail := os.Getenv("GIT_AUTHOR_EMAIL")
	if expectedEmail == "" {
		expectedEmail = "beads@local"
	}
	expectedAuthor := fmt.Sprintf("%s <%s>", expectedName, expectedEmail)
	if got := doltHeadAuthor(t, tmpDir, env); got != expectedAuthor {
		t.Fatalf("expected Dolt commit author %q, got %q", expectedAuthor, got)
	}

	// A read-only command should not create another commit.
	out, err = runBDExecAllowErrorWithEnv(t, tmpDir, env, "--dolt-auto-commit", "on", "list")
	if err != nil {
		t.Fatalf("bd list failed: %v\n%s", err, out)
	}
	afterList := doltHeadCommit(t, tmpDir, env)
	if afterList != after {
		t.Fatalf("expected Dolt HEAD unchanged after read command; before=%s after=%s", after, afterList)
	}
}

func TestDoltAutoCommit_Batch_DefersCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("dolt integration test not supported on windows")
	}
	t.Skip("TODO: enable with the Dolt write-commit mode runtime fix")

	tmpDir := newCLIIntegrationRepo(t)
	env := cliIntegrationEnv()

	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		skipIfDoltBackendUnavailable(t, initOut)
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	before := doltHeadCommit(t, tmpDir, env)

	// In batch mode (default for embedded), writes should NOT advance HEAD.
	out, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "--dolt-auto-commit", "batch", "create", "Batch test 1", "--json")
	if err != nil {
		t.Fatalf("bd create failed: %v\n%s", err, out)
	}

	afterCreate := doltHeadCommit(t, tmpDir, env)
	if afterCreate != before {
		t.Fatalf("expected Dolt HEAD unchanged in batch mode; before=%s after=%s", before, afterCreate)
	}

	// Create another issue — still deferred.
	out, err = runBDExecAllowErrorWithEnv(t, tmpDir, env, "--dolt-auto-commit", "batch", "create", "Batch test 2", "--json")
	if err != nil {
		t.Fatalf("bd create (2) failed: %v\n%s", err, out)
	}

	afterCreate2 := doltHeadCommit(t, tmpDir, env)
	if afterCreate2 != before {
		t.Fatalf("expected Dolt HEAD still unchanged; before=%s after=%s", before, afterCreate2)
	}

	// An explicit "bd dolt commit" should commit all accumulated changes.
	out, err = runBDExecAllowErrorWithEnv(t, tmpDir, env, "--dolt-auto-commit", "batch", "dolt", "commit")
	if err != nil {
		t.Fatalf("bd dolt commit failed: %v\n%s", err, out)
	}

	afterCommit := doltHeadCommit(t, tmpDir, env)
	if afterCommit == before {
		t.Fatalf("expected Dolt HEAD to advance after explicit commit; before=%s after=%s", before, afterCommit)
	}
}

func TestDoltAutoCommit_Off_DoesNotAdvanceHead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("dolt integration test not supported on windows")
	}
	t.Skip("TODO: enable with the Dolt write-commit mode runtime fix")

	tmpDir := newCLIIntegrationRepo(t)
	env := cliIntegrationEnv()

	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		skipIfDoltBackendUnavailable(t, initOut)
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
