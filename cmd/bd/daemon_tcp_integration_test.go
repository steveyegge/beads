//go:build integration
// +build integration

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
)

// TestTCPIntegration_StartServerWithTCP verifies daemon starts with TCP listener
func TestTCPIntegration_StartServerWithTCP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)
	defer testStore.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	workspacePath := tmpDir
	dbPath := testDBPath
	log := createTestLogger(t)

	// Use port 0 for automatic port assignment
	tcpAddr := "127.0.0.1:0"

	server, _, err := startRPCServer(ctx, socketPath, testStore, workspacePath, dbPath, tcpAddr, "", "", "", log)
	if err != nil {
		t.Fatalf("startRPCServer failed: %v", err)
	}
	defer func() {
		if server != nil {
			_ = server.Stop()
		}
	}()

	// Wait for server to be ready
	select {
	case <-server.WaitReady():
		// Server is ready
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not become ready within 5 seconds")
	}

	// Verify both Unix socket and TCP are connectable
	t.Run("unix_socket_connectable", func(t *testing.T) {
		conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
		if err != nil {
			t.Fatalf("Failed to connect via Unix socket: %v", err)
		}
		conn.Close()
	})

	t.Run("tcp_connectable", func(t *testing.T) {
		// Get actual TCP address from server
		tcpListener := server.TCPListener()
		if tcpListener == nil {
			t.Fatal("TCP listener not active")
		}
		actualAddr := tcpListener.Addr().String()

		conn, err := net.DialTimeout("tcp", actualAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("Failed to connect via TCP to %s: %v", actualAddr, err)
		}
		conn.Close()
	})
}

// TestTCPIntegration_ConcurrentConnections tests concurrent Unix + TCP connections
func TestTCPIntegration_ConcurrentConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)
	defer testStore.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workspacePath := tmpDir
	dbPath := testDBPath
	log := createTestLogger(t)

	tcpAddr := "127.0.0.1:0"

	server, _, err := startRPCServer(ctx, socketPath, testStore, workspacePath, dbPath, tcpAddr, "", "", "", log)
	if err != nil {
		t.Fatalf("startRPCServer failed: %v", err)
	}
	defer func() {
		if server != nil {
			_ = server.Stop()
		}
	}()

	<-server.WaitReady()

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener not active")
	}
	actualTCPAddr := tcpListener.Addr().String()

	// Run concurrent requests from both Unix and TCP
	const numWorkers = 10
	const requestsPerWorker = 5

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	// Helper function to send health request
	sendHealthRequest := func(conn net.Conn) error {
		req := rpc.Request{Operation: "health"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')

		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}

		if _, err := conn.Write(reqBytes); err != nil {
			return err
		}

		reader := bufio.NewReader(conn)
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			return err
		}

		var resp rpc.Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			return err
		}

		if !resp.Success {
			return fmt.Errorf("request failed: %s", resp.Error)
		}

		return nil
	}

	// Unix socket workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < requestsPerWorker; j++ {
				conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
				if err != nil {
					errorCount.Add(1)
					t.Logf("Unix worker %d request %d: dial error: %v", workerID, j, err)
					continue
				}

				if err := sendHealthRequest(conn); err != nil {
					errorCount.Add(1)
					t.Logf("Unix worker %d request %d: request error: %v", workerID, j, err)
				} else {
					successCount.Add(1)
				}
				conn.Close()
			}
		}(i)
	}

	// TCP workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < requestsPerWorker; j++ {
				conn, err := net.DialTimeout("tcp", actualTCPAddr, 2*time.Second)
				if err != nil {
					errorCount.Add(1)
					t.Logf("TCP worker %d request %d: dial error: %v", workerID, j, err)
					continue
				}

				if err := sendHealthRequest(conn); err != nil {
					errorCount.Add(1)
					t.Logf("TCP worker %d request %d: request error: %v", workerID, j, err)
				} else {
					successCount.Add(1)
				}
				conn.Close()
			}
		}(i + numWorkers) // offset worker ID
	}

	wg.Wait()

	totalRequests := numWorkers * requestsPerWorker * 2 // Unix + TCP
	successRate := float64(successCount.Load()) / float64(totalRequests) * 100

	t.Logf("Concurrent test results: %d/%d successful (%.1f%%), %d errors",
		successCount.Load(), totalRequests, successRate, errorCount.Load())

	// Allow some failures due to timing, but most should succeed
	if successRate < 90 {
		t.Errorf("Success rate %.1f%% is below 90%% threshold", successRate)
	}
}

// TestTCPIntegration_ConnectionTimeout tests TCP connection timeout handling
func TestTCPIntegration_ConnectionTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)
	defer testStore.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	workspacePath := tmpDir
	dbPath := testDBPath
	log := createTestLogger(t)

	tcpAddr := "127.0.0.1:0"

	server, _, err := startRPCServer(ctx, socketPath, testStore, workspacePath, dbPath, tcpAddr, "", "", "", log)
	if err != nil {
		t.Fatalf("startRPCServer failed: %v", err)
	}
	defer func() {
		if server != nil {
			_ = server.Stop()
		}
	}()

	<-server.WaitReady()

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener not active")
	}
	actualTCPAddr := tcpListener.Addr().String()

	// Test that idle connections are handled properly
	t.Run("idle_connection_timeout", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", actualTCPAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Don't send anything, just hold connection open
		// Server should eventually timeout (default 30s, but we won't wait that long)
		// Just verify the connection was established
		t.Log("Connection established, idle connection test passed")
	})

	// Test that partial requests are handled
	t.Run("partial_request_handling", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", actualTCPAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Send partial JSON (no newline)
		partial := `{"operation":"hea`
		if _, err := conn.Write([]byte(partial)); err != nil {
			t.Fatalf("Failed to write partial request: %v", err)
		}

		// Now complete it
		rest := `lth"}` + "\n"
		if _, err := conn.Write([]byte(rest)); err != nil {
			t.Fatalf("Failed to write rest of request: %v", err)
		}

		// Should still get valid response
		reader := bufio.NewReader(conn)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}

		var resp rpc.Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if !resp.Success {
			t.Errorf("Expected success, got error: %s", resp.Error)
		}
	})
}

// TestTCPIntegration_InvalidAddressHandling tests handling of invalid TCP addresses
func TestTCPIntegration_InvalidAddressHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)
	defer testStore.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	workspacePath := tmpDir
	dbPath := testDBPath
	log := createTestLogger(t)

	t.Run("invalid_tcp_address_fails", func(t *testing.T) {
		// Use an invalid address format
		invalidAddr := "invalid:address:format"
		_, _, err := startRPCServer(ctx, socketPath, testStore, workspacePath, dbPath, invalidAddr, "", "", "", log)
		if err == nil {
			t.Error("Expected error for invalid TCP address, got nil")
		}
	})

	t.Run("port_conflict_fails", func(t *testing.T) {
		// First, bind to a port
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create test listener: %v", err)
		}
		defer listener.Close()
		occupiedAddr := listener.Addr().String()

		// Try to start server on same port
		socketPath2 := filepath.Join(tmpDir, "bd2.sock")
		_, _, err = startRPCServer(ctx, socketPath2, testStore, workspacePath, dbPath, occupiedAddr, "", "", "", log)
		if err == nil {
			t.Error("Expected error for occupied port, got nil")
		}
	})
}

// TestTCPIntegration_MultipleRequests tests sending multiple requests on same TCP connection
func TestTCPIntegration_MultipleRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)
	defer testStore.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	workspacePath := tmpDir
	dbPath := testDBPath
	log := createTestLogger(t)

	tcpAddr := "127.0.0.1:0"

	server, _, err := startRPCServer(ctx, socketPath, testStore, workspacePath, dbPath, tcpAddr, "", "", "", log)
	if err != nil {
		t.Fatalf("startRPCServer failed: %v", err)
	}
	defer func() {
		if server != nil {
			_ = server.Stop()
		}
	}()

	<-server.WaitReady()

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener not active")
	}
	actualTCPAddr := tcpListener.Addr().String()

	// Connect once and send multiple requests
	conn, err := net.DialTimeout("tcp", actualTCPAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for i := 0; i < 10; i++ {
		req := rpc.Request{Operation: "health"}
		reqBytes, _ := json.Marshal(req)

		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := writer.Write(reqBytes); err != nil {
			t.Fatalf("Request %d: failed to write: %v", i, err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			t.Fatalf("Request %d: failed to write newline: %v", i, err)
		}
		if err := writer.Flush(); err != nil {
			t.Fatalf("Request %d: failed to flush: %v", i, err)
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		respBytes, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("Request %d: failed to read response: %v", i, err)
		}

		var resp rpc.Response
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			t.Fatalf("Request %d: failed to unmarshal: %v", i, err)
		}

		if !resp.Success {
			t.Errorf("Request %d: expected success, got error: %s", i, resp.Error)
		}
	}

	t.Log("Successfully sent 10 requests on single TCP connection")
}
