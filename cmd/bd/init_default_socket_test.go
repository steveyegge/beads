package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDetectDefaultDoltSocket_Present verifies that detectDefaultDoltSocket
// returns the configured path when a real unix socket is listening there.
//
// Uses MkdirTemp under /tmp with a short prefix instead of t.TempDir() —
// macOS limits unix socket paths to 104 chars (sun_path), and the default
// per-test temp dir blows past that with the test name embedded.
func TestDetectDefaultDoltSocket_Present(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix domain sockets are not supported on Windows")
	}

	tmpDir, err := os.MkdirTemp("/tmp", "bds")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	sockPath := filepath.Join(tmpDir, "s.sock")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %s: %v", sockPath, err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	orig := defaultDoltSocketPath
	defaultDoltSocketPath = sockPath
	t.Cleanup(func() { defaultDoltSocketPath = orig })

	got := detectDefaultDoltSocket()
	if got != sockPath {
		t.Errorf("detectDefaultDoltSocket() = %q; want %q", got, sockPath)
	}
}

// TestDetectDefaultDoltSocket_Absent verifies that detectDefaultDoltSocket
// returns empty when nothing is at the configured path.
func TestDetectDefaultDoltSocket_Absent(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistent := filepath.Join(tmpDir, "does-not-exist.sock")

	orig := defaultDoltSocketPath
	defaultDoltSocketPath = nonExistent
	t.Cleanup(func() { defaultDoltSocketPath = orig })

	got := detectDefaultDoltSocket()
	if got != "" {
		t.Errorf("detectDefaultDoltSocket() = %q; want empty (path does not exist)", got)
	}
}

// TestDetectDefaultDoltSocket_RegularFile verifies that a non-socket file
// at the configured path is rejected. This guards against false positives
// from leftover regular files (e.g., a `mysql.sock` text file from a
// previous experiment).
func TestDetectDefaultDoltSocket_RegularFile(t *testing.T) {
	tmpDir := t.TempDir()
	notSocket := filepath.Join(tmpDir, "regular-file.sock")
	if err := os.WriteFile(notSocket, []byte("not actually a socket"), 0o644); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	orig := defaultDoltSocketPath
	defaultDoltSocketPath = notSocket
	t.Cleanup(func() { defaultDoltSocketPath = orig })

	got := detectDefaultDoltSocket()
	if got != "" {
		t.Errorf("detectDefaultDoltSocket() = %q; want empty (path is a regular file, not a socket)", got)
	}
}

// TestDetectDefaultDoltSocket_Directory verifies that a directory at the
// configured path is rejected (defense in depth — same category of
// false-positive guard as TestDetectDefaultDoltSocket_RegularFile).
func TestDetectDefaultDoltSocket_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "a-dir-not-a-socket")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	orig := defaultDoltSocketPath
	defaultDoltSocketPath = dir
	t.Cleanup(func() { defaultDoltSocketPath = orig })

	got := detectDefaultDoltSocket()
	if got != "" {
		t.Errorf("detectDefaultDoltSocket() = %q; want empty (path is a directory, not a socket)", got)
	}
}
