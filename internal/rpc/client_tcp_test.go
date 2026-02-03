//go:build !windows

package rpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
)

// TestTryConnectTCP tests TCP client connection
func TestTryConnectTCP(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tcp-client-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0") // Use port 0 for automatic assignment

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}
	tcpAddr := tcpListener.Addr().String()

	t.Run("connect_without_token_when_not_required", func(t *testing.T) {
		client, err := TryConnectTCP(tcpAddr, "")
		if err != nil {
			t.Fatalf("TryConnectTCP failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		defer client.Close()

		if !client.IsRemote() {
			t.Error("client should be marked as remote")
		}

		// Verify we can make requests
		health, err := client.Health()
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}
		if health.Status != "healthy" {
			t.Errorf("expected healthy status, got: %s", health.Status)
		}
	})

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestTryConnectTCPWithToken tests TCP client connection with authentication
func TestTryConnectTCPWithToken(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tcp-client-token-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")
	server.SetTCPToken("test-secret-token")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}
	tcpAddr := tcpListener.Addr().String()

	t.Run("connect_with_valid_token", func(t *testing.T) {
		client, err := TryConnectTCP(tcpAddr, "test-secret-token")
		if err != nil {
			t.Fatalf("TryConnectTCP with valid token failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		defer client.Close()

		// Token should be set automatically
		health, err := client.Health()
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}
		if health.Status != "healthy" {
			t.Errorf("expected healthy status, got: %s", health.Status)
		}
	})

	t.Run("connect_with_wrong_token_fails", func(t *testing.T) {
		client, err := TryConnectTCP(tcpAddr, "wrong-token")
		// Connection succeeds but health check fails due to auth
		if err == nil && client != nil {
			_, healthErr := client.Health()
			client.Close()
			if healthErr == nil {
				t.Error("expected error with wrong token")
			}
		}
	})

	t.Run("connect_without_token_fails", func(t *testing.T) {
		client, err := TryConnectTCP(tcpAddr, "")
		// Connection succeeds but health check fails due to auth
		if err == nil && client != nil {
			_, healthErr := client.Health()
			client.Close()
			if healthErr == nil {
				t.Error("expected error without token")
			}
		}
	})

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestTryConnectAuto tests automatic connection mode selection
func TestTryConnectAuto(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auto-connect-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}
	tcpAddr := tcpListener.Addr().String()

	t.Run("auto_connect_uses_unix_by_default", func(t *testing.T) {
		// Ensure env var is not set
		os.Unsetenv("BD_DAEMON_HOST")
		os.Unsetenv("BD_DAEMON_TOKEN")

		client, err := TryConnectAuto(socketPath)
		if err != nil {
			t.Fatalf("TryConnectAuto failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		defer client.Close()

		if client.IsRemote() {
			t.Error("client should NOT be marked as remote when using Unix socket")
		}
	})

	t.Run("auto_connect_uses_tcp_when_host_set", func(t *testing.T) {
		// Set env var to TCP address
		os.Setenv("BD_DAEMON_HOST", tcpAddr)
		defer os.Unsetenv("BD_DAEMON_HOST")

		client, err := TryConnectAuto(socketPath)
		if err != nil {
			t.Fatalf("TryConnectAuto failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		defer client.Close()

		if !client.IsRemote() {
			t.Error("client should be marked as remote when using TCP")
		}
	})

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestGetDaemonHost tests the GetDaemonHost helper function
func TestGetDaemonHost(t *testing.T) {
	// Save and restore original value
	original := os.Getenv("BD_DAEMON_HOST")
	defer os.Setenv("BD_DAEMON_HOST", original)

	t.Run("returns_empty_when_not_set", func(t *testing.T) {
		os.Unsetenv("BD_DAEMON_HOST")
		if got := GetDaemonHost(); got != "" {
			t.Errorf("expected empty string, got: %s", got)
		}
	})

	t.Run("returns_value_when_set", func(t *testing.T) {
		os.Setenv("BD_DAEMON_HOST", "192.168.1.100:9876")
		if got := GetDaemonHost(); got != "192.168.1.100:9876" {
			t.Errorf("expected 192.168.1.100:9876, got: %s", got)
		}
	})
}

// TestGetDaemonToken tests the GetDaemonToken helper function
func TestGetDaemonToken(t *testing.T) {
	// Save and restore original value
	original := os.Getenv("BD_DAEMON_TOKEN")
	defer os.Setenv("BD_DAEMON_TOKEN", original)

	t.Run("returns_empty_when_not_set", func(t *testing.T) {
		os.Unsetenv("BD_DAEMON_TOKEN")
		if got := GetDaemonToken(); got != "" {
			t.Errorf("expected empty string, got: %s", got)
		}
	})

	t.Run("returns_value_when_set", func(t *testing.T) {
		os.Setenv("BD_DAEMON_TOKEN", "my-secret-token")
		if got := GetDaemonToken(); got != "my-secret-token" {
			t.Errorf("expected my-secret-token, got: %s", got)
		}
	})
}

// TestTryConnectTCPFailure tests connection to unreachable daemon
func TestTryConnectTCPFailure(t *testing.T) {
	t.Run("connection_to_invalid_address_fails", func(t *testing.T) {
		// Use a high port that's unlikely to be in use
		client, err := TryConnectTCPWithTimeout("127.0.0.1:59999", "", 500*time.Millisecond)
		if err == nil {
			if client != nil {
				client.Close()
			}
			t.Error("expected error connecting to invalid address")
		}
	})
}
