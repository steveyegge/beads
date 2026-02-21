//go:build cgo

package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/lockfile"
)

// TestRunDoltHealthChecks_NonDoltBackend was removed: SQLite backend no longer
// exists. GetBackend() always returns "dolt" after the dolt-native cleanup.
// (bd-yqpwy)

func TestRunDoltHealthChecks_DoltBackendNoDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	// Write metadata.json marking this as dolt backend
	configContent := []byte(`{"backend":"dolt"}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), configContent, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Create the dolt directory
	doltDir := filepath.Join(beadsDir, "dolt")
	if err := os.MkdirAll(doltDir, 0o755); err != nil {
		t.Fatalf("failed to create dolt dir: %v", err)
	}

	checks := RunDoltHealthChecks(tmpDir)
	if len(checks) < 1 {
		t.Fatalf("expected at least 1 check, got %d", len(checks))
	}

	// Verify the first check is the connection check.
	if checks[0].Name != "Dolt Connection" {
		t.Errorf("expected first check to be 'Dolt Connection', got %q", checks[0].Name)
	}
}

func TestRunDoltHealthChecks_CheckNameAndCategory(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	checks := RunDoltHealthChecks(tmpDir)
	if len(checks) == 0 {
		t.Fatal("expected at least 1 check")
	}

	check := checks[0]
	if check.Category != CategoryCore {
		t.Errorf("expected CategoryCore, got %q", check.Category)
	}
}

func TestServerMode_NoLockAcquired(t *testing.T) {
	// Verifies that server mode connection errors are reported correctly.
	// BEADS_DOLT_SERVER_MODE=1 triggers server mode, which attempts MySQL
	// connection instead of embedded Dolt. We force a non-listening port
	// so the connection always fails.
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	doltDir := filepath.Join(beadsDir, "dolt")
	if err := os.MkdirAll(doltDir, 0o755); err != nil {
		t.Fatalf("failed to create dolt dir: %v", err)
	}

	configContent := []byte(`{"backend":"dolt"}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), configContent, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Enable server mode via env var, pointing at a port nothing listens on
	t.Setenv("BEADS_DOLT_SERVER_MODE", "1")
	t.Setenv("BEADS_DOLT_SERVER_PORT", "59999")

	checks := RunDoltHealthChecks(tmpDir)
	if len(checks) < 1 {
		t.Fatalf("expected at least 1 check (connection error + lock health), got %d", len(checks))
	}

	check := checks[0]

	// Server mode should fail with a connection error
	if check.Status != StatusError {
		t.Errorf("expected StatusError (server unreachable), got %s", check.Status)
	}

	// The error should NOT be about lock acquisition
	if strings.Contains(check.Detail, "access lock") {
		t.Errorf("server mode should not attempt lock acquisition, but Detail contains %q: %s",
			"access lock", check.Detail)
	}
}

func TestCheckLockHealth_NoIssues(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	doltDir := filepath.Join(beadsDir, "dolt")
	if err := os.MkdirAll(doltDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := []byte(`{"backend":"dolt"}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), configContent, 0o644); err != nil {
		t.Fatal(err)
	}

	check := CheckLockHealth(tmpDir)
	if check.Status != StatusOK {
		t.Errorf("expected OK status, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckLockHealth_UnlockedNomsLOCK(t *testing.T) {
	// A noms LOCK file that is NOT flock'd should not produce a warning.
	// This is the common case: a previous bd process created the file,
	// released the flock on close, but the file persists on disk.
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	nomsDir := filepath.Join(beadsDir, "dolt", "beads", ".dolt", "noms")
	if err := os.MkdirAll(nomsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := []byte(`{"backend":"dolt"}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), configContent, 0o644); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(nomsDir, "LOCK")
	if err := os.WriteFile(lockPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	check := CheckLockHealth(tmpDir)
	if check.Status != StatusOK {
		t.Errorf("expected OK status with unlocked noms LOCK file, got %s (detail: %s)", check.Status, check.Detail)
	}
}

func TestCheckLockHealth_HeldNomsLOCK(t *testing.T) {
	// A noms LOCK file that IS flock'd should produce a warning —
	// another process is actively using the database.
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	nomsDir := filepath.Join(beadsDir, "dolt", "beads", ".dolt", "noms")
	if err := os.MkdirAll(nomsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := []byte(`{"backend":"dolt"}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), configContent, 0o644); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(nomsDir, "LOCK")
	if err := os.WriteFile(lockPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	// Hold an exclusive flock on the LOCK file to simulate an active dolt process
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := lockfile.FlockExclusiveNonBlocking(f); err != nil {
		t.Fatalf("failed to acquire flock: %v", err)
	}
	defer func() { _ = lockfile.FlockUnlock(f) }()

	check := CheckLockHealth(tmpDir)
	if check.Status != StatusWarning {
		t.Errorf("expected Warning status with held noms LOCK, got %s", check.Status)
	}
	if !strings.Contains(check.Detail, "noms LOCK") {
		t.Errorf("expected detail to mention noms LOCK, got: %s", check.Detail)
	}
}

func TestCheckLockHealth_NonDoltBackend(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write SQLite backend config
	configContent := []byte(`{"backend":"sqlite"}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), configContent, 0o644); err != nil {
		t.Fatal(err)
	}

	check := CheckLockHealth(tmpDir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for non-Dolt backend, got %s", check.Status)
	}
}
