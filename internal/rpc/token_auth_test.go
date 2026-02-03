//go:build !windows

package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
)

// TestTokenAuthRequired verifies that TCP connections require valid token when configured
func TestTokenAuthRequired(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "token-auth-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")
	server.SetTCPToken("secret-token-123")

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

	t.Run("request_without_token_fails", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send request without token
		req := Request{Operation: "health"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')
		if _, err := conn.Write(reqBytes); err != nil {
			t.Fatalf("failed to write: %v", err)
		}

		reader := bufio.NewReader(conn)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}

		var resp Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if resp.Success {
			t.Error("request without token should fail")
		}
		if resp.Error == "" || resp.Error != "authentication failed: invalid or missing token" {
			t.Errorf("expected auth error, got: %s", resp.Error)
		}
	})

	t.Run("request_with_wrong_token_fails", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send request with wrong token
		req := Request{Operation: "health", Token: "wrong-token"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')
		if _, err := conn.Write(reqBytes); err != nil {
			t.Fatalf("failed to write: %v", err)
		}

		reader := bufio.NewReader(conn)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}

		var resp Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if resp.Success {
			t.Error("request with wrong token should fail")
		}
		if resp.Error == "" || resp.Error != "authentication failed: invalid or missing token" {
			t.Errorf("expected auth error, got: %s", resp.Error)
		}
	})

	t.Run("request_with_valid_token_succeeds", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send request with correct token
		req := Request{Operation: "health", Token: "secret-token-123"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')
		if _, err := conn.Write(reqBytes); err != nil {
			t.Fatalf("failed to write: %v", err)
		}

		reader := bufio.NewReader(conn)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}

		var resp Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if !resp.Success {
			t.Errorf("request with valid token should succeed, got error: %s", resp.Error)
		}
	})

	t.Run("unix_socket_bypasses_auth", func(t *testing.T) {
		conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect via Unix socket: %v", err)
		}
		defer conn.Close()

		// Send request without token (should work on Unix socket)
		req := Request{Operation: "health"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')
		if _, err := conn.Write(reqBytes); err != nil {
			t.Fatalf("failed to write: %v", err)
		}

		reader := bufio.NewReader(conn)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}

		var resp Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if !resp.Success {
			t.Errorf("Unix socket should not require auth, got error: %s", resp.Error)
		}
	})

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestNoTokenAuthWhenNotConfigured verifies TCP connections work without token when not configured
func TestNoTokenAuthWhenNotConfigured(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "no-token-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")
	// Note: NOT setting TCP token

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

	// TCP connection without token should work when token not configured
	conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	req := Request{Operation: "health"}
	reqBytes, _ := json.Marshal(req)
	reqBytes = append(reqBytes, '\n')
	if _, err := conn.Write(reqBytes); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	reader := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respBytes, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !resp.Success {
		t.Errorf("TCP without token should work when token not configured, got error: %s", resp.Error)
	}

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}
