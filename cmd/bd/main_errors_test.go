package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIsDatabaseNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "database not found error",
			err:      errors.New(`database "myproject" not found on Dolt server at 127.0.0.1:3307`),
			expected: true,
		},
		{
			name:     "different error",
			err:      errors.New("connection refused"),
			expected: false,
		},
		{
			name:     "partial match - no dolt server",
			err:      errors.New("database not found"),
			expected: false,
		},
		{
			name:     "case insensitive match",
			err:      errors.New(`DATABASE "test" NOT FOUND on DOLT SERVER`),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDatabaseNotFoundError(tt.err)
			if result != tt.expected {
				t.Errorf("isDatabaseNotFoundError() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasBackupFiles(t *testing.T) {
	t.Run("no backup directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		origWd, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWd) })

		backupPath, issueCount := hasBackupFiles()
		if backupPath != "" || issueCount != 0 {
			t.Errorf("hasBackupFiles() = (%q, %d), want empty", backupPath, issueCount)
		}
	})

	t.Run("backup directory exists but no issues.jsonl", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, ".beads", "backup")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			t.Fatal(err)
		}

		origWd, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWd) })

		backupPath, issueCount := hasBackupFiles()
		if backupPath != "" || issueCount != 0 {
			t.Errorf("hasBackupFiles() = (%q, %d), want empty", backupPath, issueCount)
		}
	})

	t.Run("backup directory with issues.jsonl", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, ".beads", "backup")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write issues.jsonl with 3 issues
		issuesContent := `{"id":"test-1","title":"Issue 1"}
{"id":"test-2","title":"Issue 2"}
{"id":"test-3","title":"Issue 3"}
`
		if err := os.WriteFile(filepath.Join(backupDir, "issues.jsonl"), []byte(issuesContent), 0600); err != nil {
			t.Fatal(err)
		}

		origWd, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWd) })

		backupPath, issueCount := hasBackupFiles()
		if backupPath == "" {
			t.Error("hasBackupFiles() returned empty backup path, expected non-empty")
		}
		if issueCount != 3 {
			t.Errorf("hasBackupFiles() issueCount = %d, want 3", issueCount)
		}
	})

	t.Run("backup with empty lines", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, ".beads", "backup")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write issues.jsonl with empty lines
		issuesContent := `{"id":"test-1","title":"Issue 1"}

{"id":"test-2","title":"Issue 2"}

`
		if err := os.WriteFile(filepath.Join(backupDir, "issues.jsonl"), []byte(issuesContent), 0600); err != nil {
			t.Fatal(err)
		}

		origWd, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWd) })

		_, issueCount := hasBackupFiles()
		if issueCount != 2 {
			t.Errorf("hasBackupFiles() issueCount = %d, want 2 (empty lines should be skipped)", issueCount)
		}
	})
}

func TestDetectBackupFiles(t *testing.T) {
	t.Run("no backup directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupPath, issueCount := detectBackupFiles(filepath.Join(tmpDir, ".beads"))
		if backupPath != "" || issueCount != 0 {
			t.Errorf("detectBackupFiles() = (%q, %d), want empty", backupPath, issueCount)
		}
	})

	t.Run("backup directory with issues", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		backupDir := filepath.Join(beadsDir, "backup")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			t.Fatal(err)
		}

		issuesContent := `{"id":"proj-1","title":"Test Issue"}
`
		if err := os.WriteFile(filepath.Join(backupDir, "issues.jsonl"), []byte(issuesContent), 0600); err != nil {
			t.Fatal(err)
		}

		backupPath, issueCount := detectBackupFiles(beadsDir)
		if backupPath != backupDir {
			t.Errorf("detectBackupFiles() backupPath = %q, want %q", backupPath, backupDir)
		}
		if issueCount != 1 {
			t.Errorf("detectBackupFiles() issueCount = %d, want 1", issueCount)
		}
	})
}

func TestIsInteractiveTerminal(t *testing.T) {
	// This test just verifies the function doesn't panic
	// In a test environment, stdin is typically not a terminal
	result := isInteractiveTerminal()
	// We can't assert the result since it depends on the environment
	// Just verify the function runs without error
	t.Logf("isInteractiveTerminal() = %v", result)
}

func TestHandleDatabaseNotFoundError(t *testing.T) {
	t.Run("not a database not found error", func(t *testing.T) {
		err := errors.New("connection refused")
		result := handleDatabaseNotFoundError(err)
		if result {
			t.Error("handleDatabaseNotFoundError() = true for non-DB-not-found error, want false")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		result := handleDatabaseNotFoundError(nil)
		if result {
			t.Error("handleDatabaseNotFoundError(nil) = true, want false")
		}
	})

	t.Run("database not found but no backups", func(t *testing.T) {
		// Create temp dir with no backup files
		tmpDir := t.TempDir()
		origWd, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWd) })

		err := errors.New(`database "myproject" not found on Dolt server at 127.0.0.1:3307`)
		result := handleDatabaseNotFoundError(err)
		if result {
			t.Error("handleDatabaseNotFoundError() = true when no backups exist, want false")
		}
	})

	t.Run("database not found with backups", func(t *testing.T) {
		// Create temp dir with backup files
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, ".beads", "backup")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			t.Fatal(err)
		}

		issuesContent := `{"id":"test-1","title":"Issue 1"}
`
		if err := os.WriteFile(filepath.Join(backupDir, "issues.jsonl"), []byte(issuesContent), 0600); err != nil {
			t.Fatal(err)
		}

		origWd, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWd) })

		err := errors.New(`database "myproject" not found on Dolt server at 127.0.0.1:3307`)
		result := handleDatabaseNotFoundError(err)
		if !result {
			t.Error("handleDatabaseNotFoundError() = false when backups exist, want true")
		}
	})
}

func TestAttemptAutoRestore(t *testing.T) {
	t.Run("returns true and prints instructions", func(t *testing.T) {
		result := attemptAutoRestore("/path/to/backup")
		if !result {
			t.Error("attemptAutoRestore() = false, want true")
		}
	})
}
