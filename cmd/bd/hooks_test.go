//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostCheckoutHook_PrintsWarningWhenDBMissing(t *testing.T) {
	tmpDir := newGitRepo(t)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	// Do NOT create .beads/dolt/ — simulates missing database

	runInDir(t, tmpDir, func() {
		output := captureStderr(t, func() {
			exitCode := runPostCheckoutHook([]string{"abc1234", "def5678", "1"})
			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0", exitCode)
			}
		})

		if !strings.Contains(output, "bd bootstrap") {
			t.Errorf("warning should mention 'bd bootstrap', got: %q", output)
		}
	})
}

func TestPostCheckoutHook_NoWarningInServerMode(t *testing.T) {
	tmpDir := newGitRepo(t)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"dolt_mode":"server"}`), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	runInDir(t, tmpDir, func() {
		output := captureStderr(t, func() {
			exitCode := runPostCheckoutHook([]string{"abc1234", "def5678", "1"})
			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0", exitCode)
			}
		})

		if strings.Contains(output, "bd bootstrap") {
			t.Errorf("should NOT warn in server mode, got: %q", output)
		}
	})
}

func TestPostCheckoutHook_NoWarningOnFileCheckout(t *testing.T) {
	tmpDir := newGitRepo(t)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o750); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	runInDir(t, tmpDir, func() {
		output := captureStderr(t, func() {
			// flag=0 means file checkout, not branch checkout
			exitCode := runPostCheckoutHook([]string{"abc1234", "def5678", "0"})
			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0", exitCode)
			}
		})

		if strings.Contains(output, "bd bootstrap") {
			t.Errorf("should NOT warn on file checkout, got: %q", output)
		}
	})
}
