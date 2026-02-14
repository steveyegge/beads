//go:build cgo && integration

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestE2E_InitDoltMetadataRoundtrip verifies that bd init --backend dolt writes
// metadata that bd doctor can validate without warnings.
// Covers FR-018 (e2e init->doctor roundtrip).
func TestE2E_InitDoltMetadataRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("dolt metadata e2e test not supported on windows")
	}

	tmpDir := createTempDirWithCleanup(t)

	// Set up a real git repo so repo_id can be computed
	if err := runCommandInDir(tmpDir, "git", "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	_ = runCommandInDir(tmpDir, "git", "config", "user.email", "test@example.com")
	_ = runCommandInDir(tmpDir, "git", "config", "user.name", "Test User")
	_ = runCommandInDir(tmpDir, "git", "config", "remote.origin.url", "https://github.com/test/repo.git")

	socketPath := filepath.Join(tmpDir, ".beads", "bd.sock")
	env := append(os.Environ(),
		"BEADS_TEST_MODE=1",
		"BEADS_AUTO_START_DAEMON=true",
		"BEADS_NO_DAEMON=0",
		"BD_SOCKET="+socketPath,
	)

	// Init dolt backend
	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		lower := strings.ToLower(initOut)
		if strings.Contains(lower, "dolt") && (strings.Contains(lower, "not supported") || strings.Contains(lower, "not available") || strings.Contains(lower, "unknown")) {
			t.Skipf("dolt backend not available: %s", initOut)
		}
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	// Ensure daemon cleanup
	t.Cleanup(func() {
		_, _ = runBDExecAllowErrorWithEnv(t, tmpDir, env, "daemon", "stop")
		time.Sleep(200 * time.Millisecond)
	})

	// Run doctor and verify no metadata warnings
	doctorOut, _ := runBDExecAllowErrorWithEnv(t, tmpDir, env, "doctor")

	// Doctor should NOT report missing metadata
	metadataWarnings := []string{
		"Missing metadata",
		"bd_version",
		"repo_id not set",
		"clone_id not set",
	}
	for _, warning := range metadataWarnings {
		if strings.Contains(doctorOut, warning) {
			t.Errorf("bd doctor reported metadata warning %q after init; output:\n%s", warning, doctorOut)
		}
	}

	// Sanity check: doctor should mention dolt
	if !strings.Contains(strings.ToLower(doctorOut), "dolt") {
		t.Logf("Note: doctor output did not mention dolt; output:\n%s", doctorOut)
	}

	// Verify no SQLite database was created (regression check)
	if _, err := os.Stat(filepath.Join(tmpDir, ".beads", "beads.db")); err == nil {
		t.Errorf("unexpected sqlite database created in dolt mode")
	}
}

// TestE2E_DoctorFixMetadataRoundtrip verifies that bd doctor --fix repairs
// missing metadata on an existing Dolt database, and a subsequent bd doctor
// reports a clean state.
// Covers FR-019 (e2e doctor fix cycle).
func TestE2E_DoctorFixMetadataRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("dolt metadata e2e test not supported on windows")
	}

	tmpDir := createTempDirWithCleanup(t)

	// Set up a real git repo so repo_id can be computed
	if err := runCommandInDir(tmpDir, "git", "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	_ = runCommandInDir(tmpDir, "git", "config", "user.email", "test@example.com")
	_ = runCommandInDir(tmpDir, "git", "config", "user.name", "Test User")
	_ = runCommandInDir(tmpDir, "git", "config", "remote.origin.url", "https://github.com/test/repo.git")

	socketPath := filepath.Join(tmpDir, ".beads", "bd.sock")
	env := append(os.Environ(),
		"BEADS_TEST_MODE=1",
		"BEADS_AUTO_START_DAEMON=true",
		"BEADS_NO_DAEMON=0",
		"BD_SOCKET="+socketPath,
	)

	// Init dolt backend (which now writes metadata via Phase 1)
	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		lower := strings.ToLower(initOut)
		if strings.Contains(lower, "dolt") && (strings.Contains(lower, "not supported") || strings.Contains(lower, "not available") || strings.Contains(lower, "unknown")) {
			t.Skipf("dolt backend not available: %s", initOut)
		}
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	// Ensure daemon cleanup
	t.Cleanup(func() {
		_, _ = runBDExecAllowErrorWithEnv(t, tmpDir, env, "daemon", "stop")
		time.Sleep(200 * time.Millisecond)
	})

	// Delete metadata to simulate a pre-Phase-1 database
	sqlOut, sqlErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "sql",
		"DELETE FROM metadata WHERE key IN ('bd_version', 'repo_id', 'clone_id')")
	if sqlErr != nil {
		t.Fatalf("bd sql DELETE failed: %v\n%s", sqlErr, sqlOut)
	}

	// Verify doctor detects the missing metadata
	doctorOut1, _ := runBDExecAllowErrorWithEnv(t, tmpDir, env, "doctor")
	if !strings.Contains(doctorOut1, "doctor --fix") {
		t.Logf("Note: first doctor did not suggest 'doctor --fix'; output:\n%s", doctorOut1)
	}

	// Run doctor --fix to repair
	fixOut, fixErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "doctor", "--fix", "--yes")
	if fixErr != nil {
		t.Fatalf("bd doctor --fix failed: %v\n%s", fixErr, fixOut)
	}

	// Run doctor again and verify no metadata warnings
	doctorOut2, _ := runBDExecAllowErrorWithEnv(t, tmpDir, env, "doctor")
	metadataWarnings := []string{
		"Missing metadata",
		"missing version metadata",
		"Missing repo fingerprint",
	}
	for _, warning := range metadataWarnings {
		if strings.Contains(doctorOut2, warning) {
			t.Errorf("bd doctor still reports metadata warning %q after fix; output:\n%s", warning, doctorOut2)
		}
	}
}
