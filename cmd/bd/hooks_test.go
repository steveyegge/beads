//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostCheckoutHook_NoWarningInServerMode(t *testing.T) {
	// In Dolt server mode, .beads/dolt/ doesn't exist locally — that's expected.
	// The hook should NOT print a warning.
	tmpDir := newGitRepo(t)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	// metadata.json with server mode
	meta := `{"dolt_mode":"server","dolt_server_host":"127.0.0.1","dolt_server_port":3307}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(meta), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	// No .beads/dolt/ — this is expected in server mode

	runInDir(t, tmpDir, func() {
		output := captureStderr(t, func() {
			exitCode := runPostCheckoutHook([]string{"abc1234", "def5678", "1"})
			if exitCode != 0 {
				t.Errorf("post-checkout exit code = %d, want 0", exitCode)
			}
		})

		if strings.Contains(output, "bd init") {
			t.Errorf("post-checkout should NOT warn in server mode, but got: %q", output)
		}
	})
}

func TestPostCheckoutHook_PrintsWarningWhenDBMissing(t *testing.T) {
	// runPostCheckoutHook should return 0 (never block git)
	// but when it detects no database, it should print a helpful message
	// to stderr so the user knows to run bd init.
	tmpDir := newGitRepo(t)

	// Create .beads/ with metadata.json so FindBeadsDir() recognizes workspace
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	// Do NOT create .beads/dolt/ — simulates missing database

	runInDir(t, tmpDir, func() {
		output := captureStderr(t, func() {
			exitCode := runPostCheckoutHook([]string{"abc1234", "def5678", "1"})
			if exitCode != 0 {
				t.Errorf("post-checkout exit code = %d, want 0 (must never block git)", exitCode)
			}
		})

		if !strings.Contains(output, "bd init") {
			t.Errorf("post-checkout warning should mention 'bd init', got: %q", output)
		}
	})
}
