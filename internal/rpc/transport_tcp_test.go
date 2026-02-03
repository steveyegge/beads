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

// TestTCPListenerBindsCorrectly verifies that the TCP listener binds to the specified address.
func TestTCPListenerBindsCorrectly(t *testing.T) {
	// Create a temp directory for the Unix socket
	tmpDir, err := os.MkdirTemp("", "tcp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))

	// Configure TCP address (use port 0 for automatic port assignment)
	server.SetTCPAddr("127.0.0.1:0")

	// Start server in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	// Wait for server to be ready
	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	// Verify TCP listener is active by getting its address
	server.mu.RLock()
	tcpListener := server.tcpListener
	server.mu.RUnlock()

	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}

	tcpAddr := tcpListener.Addr().String()
	t.Logf("TCP listener bound to: %s", tcpAddr)

	// Clean up
	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestBothUnixAndTCPWorkSimultaneously verifies that both Unix socket and TCP
// listeners work at the same time and can handle requests.
func TestBothUnixAndTCPWorkSimultaneously(t *testing.T) {
	// Create a temp directory for the Unix socket
	tmpDir, err := os.MkdirTemp("", "tcp-dual-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))

	// Configure TCP address (use port 0 for automatic port assignment)
	server.SetTCPAddr("127.0.0.1:0")

	// Start server in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	// Wait for server to be ready
	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	// Get TCP address
	server.mu.RLock()
	tcpListener := server.tcpListener
	server.mu.RUnlock()

	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}
	tcpAddr := tcpListener.Addr().String()

	// Test Unix socket connection
	t.Run("unix_socket_works", func(t *testing.T) {
		conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect via Unix socket: %v", err)
		}
		defer conn.Close()

		// Send health request
		req := Request{Operation: "health"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')
		if _, err := conn.Write(reqBytes); err != nil {
			t.Fatalf("failed to write request: %v", err)
		}

		// Read response
		reader := bufio.NewReader(conn)
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		var resp Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success=true, got false: %s", resp.Error)
		}
	})

	// Test TCP connection
	t.Run("tcp_works", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect via TCP: %v", err)
		}
		defer conn.Close()

		// Send health request
		req := Request{Operation: "health"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')
		if _, err := conn.Write(reqBytes); err != nil {
			t.Fatalf("failed to write request: %v", err)
		}

		// Read response
		reader := bufio.NewReader(conn)
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		var resp Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success=true, got false: %s", resp.Error)
		}
	})

	// Test both simultaneously with concurrent requests
	t.Run("concurrent_unix_and_tcp", func(t *testing.T) {
		done := make(chan error, 2)

		// Unix socket request
		go func() {
			conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
			if err != nil {
				done <- err
				return
			}
			defer conn.Close()

			req := Request{Operation: "health"}
			reqBytes, _ := json.Marshal(req)
			reqBytes = append(reqBytes, '\n')
			if _, err := conn.Write(reqBytes); err != nil {
				done <- err
				return
			}

			reader := bufio.NewReader(conn)
			if _, err := reader.ReadBytes('\n'); err != nil {
				done <- err
				return
			}
			done <- nil
		}()

		// TCP request
		go func() {
			conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
			if err != nil {
				done <- err
				return
			}
			defer conn.Close()

			req := Request{Operation: "health"}
			reqBytes, _ := json.Marshal(req)
			reqBytes = append(reqBytes, '\n')
			if _, err := conn.Write(reqBytes); err != nil {
				done <- err
				return
			}

			reader := bufio.NewReader(conn)
			if _, err := reader.ReadBytes('\n'); err != nil {
				done <- err
				return
			}
			done <- nil
		}()

		// Wait for both to complete
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("concurrent request failed: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("timeout waiting for concurrent requests")
			}
		}
	})

	// Clean up
	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestTCPListenerOnlyWhenConfigured verifies that no TCP listener is created
// when tcpAddr is not set.
func TestTCPListenerOnlyWhenConfigured(t *testing.T) {
	// Create a temp directory for the Unix socket
	tmpDir, err := os.MkdirTemp("", "tcp-none-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	// Do NOT set TCP address

	// Start server in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	// Wait for server to be ready
	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	// Verify TCP listener is NOT active
	server.mu.RLock()
	tcpListener := server.tcpListener
	server.mu.RUnlock()

	if tcpListener != nil {
		t.Error("TCP listener should NOT be active when tcpAddr is not set")
	}

	// Unix socket should still work
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect via Unix socket: %v", err)
	}
	conn.Close()

	// Clean up
	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}
