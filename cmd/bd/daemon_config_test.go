package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetSocketPathForPID tests socket path derivation from PID file path
func TestGetSocketPathForPID(t *testing.T) {
	tests := []struct {
		name     string
		pidFile  string
		expected string
	}{
		{
			name:     "absolute path",
			pidFile:  "/home/user/.beads/daemon.pid",
			expected: "/home/user/.beads/bd.sock",
		},
		{
			name:     "relative path",
			pidFile:  ".beads/daemon.pid",
			expected: ".beads/bd.sock",
		},
		{
			name:     "nested path",
			pidFile:  "/var/run/beads/project/.beads/daemon.pid",
			expected: "/var/run/beads/project/.beads/bd.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSocketPathForPID(tt.pidFile)
			if result != tt.expected {
				t.Errorf("getSocketPathForPID(%q) = %q, want %q", tt.pidFile, result, tt.expected)
			}
		})
	}
}

// Note: TestGetEnvInt, TestGetEnvBool, TestBoolToFlag are already defined in
// daemon_rotation_test.go and autoimport_test.go respectively

func TestEnsureBeadsDir(t *testing.T) {
	// Save original dbPath and restore after
	originalDbPath := dbPath
	defer func() { dbPath = originalDbPath }()

	t.Run("creates directory when dbPath is set", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		dbPath = filepath.Join(beadsDir, "beads.db")

		result, err := ensureBeadsDir()
		if err != nil {
			t.Fatalf("ensureBeadsDir() error = %v", err)
		}

		if result != beadsDir {
			t.Errorf("ensureBeadsDir() = %q, want %q", result, beadsDir)
		}

		// Verify directory was created
		if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
			t.Error("directory was not created")
		}
	})

	t.Run("returns existing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		// Pre-create the directory
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("failed to create test directory: %v", err)
		}
		dbPath = filepath.Join(beadsDir, "beads.db")

		result, err := ensureBeadsDir()
		if err != nil {
			t.Fatalf("ensureBeadsDir() error = %v", err)
		}

		if result != beadsDir {
			t.Errorf("ensureBeadsDir() = %q, want %q", result, beadsDir)
		}
	})
}

func TestGetPIDFilePath(t *testing.T) {
	// Save original dbPath and restore after
	originalDbPath := dbPath
	defer func() { dbPath = originalDbPath }()

	t.Run("returns correct PID file path", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		dbPath = filepath.Join(beadsDir, "beads.db")

		result, err := getPIDFilePath()
		if err != nil {
			t.Fatalf("getPIDFilePath() error = %v", err)
		}

		expected := filepath.Join(beadsDir, "daemon.pid")
		if result != expected {
			t.Errorf("getPIDFilePath() = %q, want %q", result, expected)
		}
	})
}

func TestGetLogFilePath(t *testing.T) {
	// Save original dbPath and restore after
	originalDbPath := dbPath
	defer func() { dbPath = originalDbPath }()

	t.Run("returns user-specified path when provided", func(t *testing.T) {
		userPath := "/custom/path/daemon.log"
		result, err := getLogFilePath(userPath)
		if err != nil {
			t.Fatalf("getLogFilePath() error = %v", err)
		}
		if result != userPath {
			t.Errorf("getLogFilePath(%q) = %q, want %q", userPath, result, userPath)
		}
	})

	t.Run("returns default path when empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		dbPath = filepath.Join(beadsDir, "beads.db")

		result, err := getLogFilePath("")
		if err != nil {
			t.Fatalf("getLogFilePath() error = %v", err)
		}

		expected := filepath.Join(beadsDir, "daemon.log")
		if result != expected {
			t.Errorf("getLogFilePath(\"\") = %q, want %q", result, expected)
		}
	})
}
