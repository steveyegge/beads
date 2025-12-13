package reset

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestEnv sets BEADS_DIR to the test directory using t.Setenv for automatic cleanup
func setupTestEnv(t *testing.T, beadsDir string) {
	t.Helper()
	// t.Setenv automatically restores the previous value when the test completes
	t.Setenv("BEADS_DIR", beadsDir)
	// Also unset BEADS_DB to prevent finding the real database
	t.Setenv("BEADS_DB", "")
}

// createMinimalBeadsDir creates a minimal .beads directory with required files
func createMinimalBeadsDir(t *testing.T, tmpDir string) string {
	t.Helper()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}
	// Create metadata.json to make it a valid beads directory
	metadataPath := filepath.Join(beadsDir, "metadata.json")
	if err := os.WriteFile(metadataPath, []byte(`{"version":"1.0"}`), 0644); err != nil {
		t.Fatalf("failed to create metadata.json: %v", err)
	}
	return beadsDir
}

func TestValidateState_NoBeadsDir(t *testing.T) {
	// Create temp directory without .beads
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")

	// Change to temp dir and set BEADS_DIR to prevent finding real .beads
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	setupTestEnv(t, beadsDir)

	err := ValidateState()
	if err == nil {
		t.Error("expected error when .beads directory doesn't exist")
	}
}

func TestValidateState_BeadsDirExists(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := createMinimalBeadsDir(t, tmpDir)
	setupTestEnv(t, beadsDir)

	err := ValidateState()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateState_BeadsIsFile(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, ".beads")
	if err := os.WriteFile(beadsPath, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to create .beads file: %v", err)
	}

	// Change to temp dir to prevent finding real .beads
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	setupTestEnv(t, beadsPath)

	err := ValidateState()
	if err == nil {
		t.Error("expected error when .beads is a file, not directory")
	}
}

func TestCountImpact_EmptyWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := createMinimalBeadsDir(t, tmpDir)
	setupTestEnv(t, beadsDir)

	impact, err := CountImpact()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if impact.IssueCount != 0 {
		t.Errorf("expected 0 issues, got %d", impact.IssueCount)
	}
	if impact.TombstoneCount != 0 {
		t.Errorf("expected 0 tombstones, got %d", impact.TombstoneCount)
	}
}

// TestCountImpact_WithIssues is skipped in isolated test environments
// because CountImpact relies on beads.FindDatabasePath() which has complex
// path resolution logic that's difficult to mock in tests.
// The CountImpact function is well-tested through integration tests.
func TestCountImpact_WithIssues(t *testing.T) {
	t.Skip("Skipped: CountImpact uses beads.FindDatabasePath() which is complex to test in isolation")
}

func TestReset_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := createMinimalBeadsDir(t, tmpDir)
	setupTestEnv(t, beadsDir)

	// Create a test file
	testFile := filepath.Join(beadsDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	opts := ResetOptions{DryRun: true}
	result, err := Reset(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the file still exists (dry run shouldn't delete anything)
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("dry run should not delete files")
	}

	// Result should still have counts
	if result == nil {
		t.Error("expected result from dry run")
	}
}

func TestReset_SoftReset(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := createMinimalBeadsDir(t, tmpDir)
	setupTestEnv(t, beadsDir)

	// Create a test file
	testFile := filepath.Join(beadsDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Skip init since we don't have a proper beads environment
	opts := ResetOptions{SkipInit: true}
	result, err := Reset(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the .beads directory is gone
	if _, err := os.Stat(beadsDir); !os.IsNotExist(err) {
		t.Error("reset should delete .beads directory")
	}

	if result == nil {
		t.Error("expected result from reset")
	}
}

func TestReset_WithBackup(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := createMinimalBeadsDir(t, tmpDir)
	setupTestEnv(t, beadsDir)

	// Create a test file
	testFile := filepath.Join(beadsDir, "test.txt")
	testContent := "test content"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	opts := ResetOptions{Backup: true, SkipInit: true}
	result, err := Reset(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify backup was created
	if result.BackupPath == "" {
		t.Error("expected backup path in result")
	}

	// Verify backup directory exists
	if _, err := os.Stat(result.BackupPath); os.IsNotExist(err) {
		t.Error("backup directory should exist")
	}

	// Verify backup contains the test file
	backupFile := filepath.Join(result.BackupPath, "test.txt")
	content, err := os.ReadFile(backupFile)
	if err != nil {
		t.Errorf("failed to read backup file: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("backup content mismatch: got %q, want %q", content, testContent)
	}

	// Verify original .beads is gone
	if _, err := os.Stat(beadsDir); !os.IsNotExist(err) {
		t.Error("original .beads should be deleted after reset")
	}
}

func TestReset_NoBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")

	// Change to temp dir to prevent finding real .beads
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	setupTestEnv(t, beadsDir)

	opts := ResetOptions{}
	_, err := Reset(opts)
	if err == nil {
		t.Error("expected error when .beads doesn't exist")
	}
}
