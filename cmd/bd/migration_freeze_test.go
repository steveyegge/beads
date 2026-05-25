package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindTownRootForFreezeEnvVar verifies that findTownRootForFreeze
// returns the root from GT_ROOT/GT_TOWN_ROOT when set and valid.
func TestFindTownRootForFreezeEnvVar(t *testing.T) {
	// Create a temp directory that looks like a town root (has mayor/ dir)
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GT_TOWN_ROOT", tmpDir)
	t.Setenv("GT_ROOT", "")

	got := findTownRootForFreeze()
	if got != tmpDir {
		t.Errorf("findTownRootForFreeze() = %q, want %q", got, tmpDir)
	}
}

// TestFindTownRootForFreezeGTROOT verifies GT_ROOT is used as fallback.
func TestFindTownRootForFreezeGTROOT(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GT_TOWN_ROOT", "")
	t.Setenv("GT_ROOT", tmpDir)

	got := findTownRootForFreeze()
	if got != tmpDir {
		t.Errorf("findTownRootForFreeze() = %q, want %q", got, tmpDir)
	}
}

// TestFindTownRootForFreezeEnvInvalid verifies that an env var pointing to a
// non-town directory is ignored and the CWD walk is used instead.
func TestFindTownRootForFreezeEnvInvalid(t *testing.T) {
	t.Setenv("GT_TOWN_ROOT", "/nonexistent/path")
	t.Setenv("GT_ROOT", "/also/nonexistent")

	// With no valid env var and CWD not in a town, should return ""
	// (We can't easily control CWD in tests, but we can verify the env
	// var path doesn't cause a panic or crash.)
	_ = findTownRootForFreeze()
}

// TestCheckMigrationFreezeNoFreeze verifies CheckMigrationFreeze is a no-op
// when no freeze is active.
func TestCheckMigrationFreezeNoFreeze(t *testing.T) {
	// Point GT_ROOT to a temp dir with no MIGRATION-FREEZE file
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GT_ROOT", tmpDir)
	t.Setenv("GT_TOWN_ROOT", "")

	// Should return without calling os.Exit. If it exits, the test process dies.
	CheckMigrationFreeze("create")
}

// TestCheckMigrationFreezeNoTownRoot verifies CheckMigrationFreeze is a no-op
// when no town root is found.
func TestCheckMigrationFreezeNoTownRoot(t *testing.T) {
	t.Setenv("GT_ROOT", "")
	t.Setenv("GT_TOWN_ROOT", "")

	// With no env vars and CWD not in a Gas Town workspace (in temp dir),
	// CheckMigrationFreeze should be a no-op.
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Skip("cannot change directory:", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Should return without calling os.Exit.
	CheckMigrationFreeze("create")
}

// TestMigrationFreezeFileFormat verifies the sentinel file is parsed correctly.
// The format is: operator\ttimestamp\treason\n
func TestMigrationFreezeFileFormat(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}

	freezeFile := filepath.Join(tmpDir, "MIGRATION-FREEZE")
	content := "mayor\t2026-05-25T10:00:00Z\thq migration\n"
	if err := os.WriteFile(freezeFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify the file exists and can be stat'd
	if _, err := os.Stat(freezeFile); err != nil {
		t.Fatalf("freeze file should exist: %v", err)
	}

	// Verify the parse logic (test the parsing inline since CheckMigrationFreeze
	// calls os.Exit and can't be tested directly when a freeze is active)
	data, err := os.ReadFile(freezeFile)
	if err != nil {
		t.Fatal(err)
	}
	parts := splitFreezeContent(string(data))
	if len(parts) < 1 || parts[0] != "mayor" {
		t.Errorf("expected operator 'mayor', got %v", parts)
	}
	if len(parts) < 3 || parts[2] != "hq migration" {
		t.Errorf("expected reason 'hq migration', got %v", parts)
	}
}

// splitFreezeContent is a test helper that mirrors the parsing logic in
// CheckMigrationFreeze to allow testing without triggering os.Exit.
func splitFreezeContent(content string) []string {
	trimmed := content
	for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == '\n' || trimmed[len(trimmed)-1] == '\r') {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "" {
		return nil
	}
	result := []string{}
	start := 0
	count := 0
	for i, ch := range trimmed {
		if ch == '\t' && count < 2 {
			result = append(result, trimmed[start:i])
			start = i + 1
			count++
		}
	}
	result = append(result, trimmed[start:])
	return result
}
