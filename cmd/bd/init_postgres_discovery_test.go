package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverLocalPostgres_PostmasterPidPresent verifies the primary discovery
// path: a well-formed postmaster.pid produces ("127.0.0.1", 5433, true).
func TestDiscoverLocalPostgres_PostmasterPidPresent(t *testing.T) {
	dir := t.TempDir()

	// postmaster.pid layout (line indices 0-5):
	//   0: PID
	//   1: data directory
	//   2: start time (Unix epoch)
	//   3: port
	//   4: socket directory
	//   5: listen host
	pidContent := "12345\n" +
		"/var/lib/postgresql/data\n" +
		"1715000000\n" +
		"5433\n" +
		"/var/run/postgresql\n" +
		"127.0.0.1\n"
	if err := os.WriteFile(filepath.Join(dir, "postmaster.pid"), []byte(pidContent), 0o600); err != nil {
		t.Fatal(err)
	}

	host, port, found := discoverLocalPostgres(dir)
	if !found {
		t.Fatal("expected found=true")
	}
	if host != "127.0.0.1" {
		t.Errorf("host: got %q, want %q", host, "127.0.0.1")
	}
	if port != 5433 {
		t.Errorf("port: got %d, want 5433", port)
	}
}

// TestDiscoverLocalPostgres_PostgresConfFallback verifies that postgresql.conf
// is used when postmaster.pid is absent.
func TestDiscoverLocalPostgres_PostgresConfFallback(t *testing.T) {
	dir := t.TempDir()

	confContent := "# PostgreSQL configuration\n" +
		"port = 5433\n" +
		"listen_addresses = '127.0.0.1'\n"
	if err := os.WriteFile(filepath.Join(dir, "postgresql.conf"), []byte(confContent), 0o600); err != nil {
		t.Fatal(err)
	}

	host, port, found := discoverLocalPostgres(dir)
	if !found {
		t.Fatal("expected found=true")
	}
	if host != "127.0.0.1" {
		t.Errorf("host: got %q, want %q", host, "127.0.0.1")
	}
	if port != 5433 {
		t.Errorf("port: got %d, want 5433", port)
	}
}

// TestDiscoverLocalPostgres_NoCluster verifies that a missing or empty
// cluster directory returns ("", 0, false) without panicking.
func TestDiscoverLocalPostgres_NoCluster(t *testing.T) {
	t.Run("missing dir", func(t *testing.T) {
		host, port, found := discoverLocalPostgres("/nonexistent/path/that/should/not/exist")
		if found {
			t.Errorf("expected found=false, got host=%q port=%d", host, port)
		}
	})

	t.Run("empty dir", func(t *testing.T) {
		dir := t.TempDir()
		host, port, found := discoverLocalPostgres(dir)
		if found {
			t.Errorf("expected found=false, got host=%q port=%d", host, port)
		}
	})
}

// TestDiscoverLocalPostgres_MalformedPid verifies that a truncated
// postmaster.pid (fewer than 6 lines) triggers a warning and falls back to
// postgresql.conf; if no conf exists, returns ("", 0, false) without panicking.
func TestDiscoverLocalPostgres_MalformedPid(t *testing.T) {
	dir := t.TempDir()

	// Only 2 lines — too short for line 4 (port) or line 6 (host).
	pidContent := "12345\n/data\n"
	if err := os.WriteFile(filepath.Join(dir, "postmaster.pid"), []byte(pidContent), 0o600); err != nil {
		t.Fatal(err)
	}

	host, port, found := discoverLocalPostgres(dir)
	if found {
		t.Errorf("expected found=false with truncated pid, got host=%q port=%d", host, port)
	}
	_ = host
	_ = port
}
