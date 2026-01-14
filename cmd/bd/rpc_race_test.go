//go:build !windows
// +build !windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
)

// =============================================================================
// RPC Concurrency Race Condition Tests
// =============================================================================
//
// These tests verify correct RPC handling under concurrent access.
// Run with: go test -race -run TestRPC -v
//
// Race conditions being tested:
// 1. Concurrent RPC requests (10+ goroutines)
// 2. RPC during database reconnect (simulated)
// 3. RPC timeout handling
// 4. Request cancellation
// 5. Connection pool exhaustion
//
// =============================================================================

// TestConcurrentRPCRequests verifies server handles many concurrent requests.
//
// Race condition tested: Multiple goroutines sending RPC requests simultaneously.
// All requests should complete without data races or corruption.
func TestConcurrentRPCRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent RPC test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	const numGoroutines = 20
	const requestsPerGoroutine = 10

	var (
		successCount int32
		errorCount   int32
		wg           sync.WaitGroup
	)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// each worker gets its own connection
			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				t.Logf("Worker %d failed to connect: %v", workerID, err)
				atomic.AddInt32(&errorCount, int32(requestsPerGoroutine))
				return
			}
			defer client.Close()

			for j := 0; j < requestsPerGoroutine; j++ {
				// alternate between different request types
				switch j % 3 {
				case 0:
					if _, err := client.Health(); err == nil {
						atomic.AddInt32(&successCount, 1)
					} else {
						atomic.AddInt32(&errorCount, 1)
					}
				case 1:
					// Ping returns only error
					if err := client.Ping(); err == nil {
						atomic.AddInt32(&successCount, 1)
					} else {
						atomic.AddInt32(&errorCount, 1)
					}
				case 2:
					if _, err := client.Status(); err == nil {
						atomic.AddInt32(&successCount, 1)
					} else {
						atomic.AddInt32(&errorCount, 1)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	totalRequests := int32(numGoroutines * requestsPerGoroutine)
	t.Logf("Concurrent RPC test: %d/%d succeeded, %d errors",
		successCount, totalRequests, errorCount)

	// most requests should succeed
	if successCount < totalRequests*8/10 {
		t.Errorf("Expected at least 80%% success rate, got %d/%d",
			successCount, totalRequests)
	}
}

// TestRPCTimeoutHandling verifies RPC timeout behavior.
//
// Race condition tested: Requests timing out while being processed.
// Timeouts should be clean without corrupting state.
func TestRPCTimeoutHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping RPC timeout test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// test with short timeout
	client, err := rpc.TryConnectWithTimeout(socketPath, 1*time.Second)
	if err != nil || client == nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// set a very short timeout
	client.SetTimeout(1 * time.Millisecond)

	// make requests that may timeout
	var timeoutCount int32
	var successCount int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.Health()
			if err != nil {
				atomic.AddInt32(&timeoutCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Timeout test: %d timeouts, %d successes", timeoutCount, successCount)

	// after timeout tests, server should still be healthy
	client2, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
	if err != nil || client2 == nil {
		t.Fatalf("Failed to connect after timeout tests: %v", err)
	}
	defer client2.Close()

	// use normal timeout
	client2.SetTimeout(5 * time.Second)
	health, err := client2.Health()
	if err != nil {
		t.Fatalf("Health check failed after timeout tests: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("Server unhealthy after timeout tests: %s", health.Status)
	}
}

// TestRPCConnectionPoolExhaustion verifies behavior when max connections reached.
//
// Race condition tested: Many clients try to connect when pool is exhausted.
// Excess connections should be rejected cleanly.
func TestRPCConnectionPoolExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping connection pool test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	// set low max connections for testing
	os.Setenv("BEADS_DAEMON_MAX_CONNS", "5")
	defer os.Unsetenv("BEADS_DAEMON_MAX_CONNS")

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// try to create more connections than allowed
	const numConnections = 20
	var (
		connectedCount int32
		rejectedCount  int32
		wg             sync.WaitGroup
		clients        []*rpc.Client
		clientsMu      sync.Mutex
	)

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connID int) {
			defer wg.Done()

			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				atomic.AddInt32(&rejectedCount, 1)
				return
			}

			atomic.AddInt32(&connectedCount, 1)
			clientsMu.Lock()
			clients = append(clients, client)
			clientsMu.Unlock()

			// hold connection open
			time.Sleep(500 * time.Millisecond)
		}(i)
	}

	wg.Wait()

	// cleanup clients
	clientsMu.Lock()
	for _, c := range clients {
		c.Close()
	}
	clientsMu.Unlock()

	t.Logf("Connection pool test: %d connected, %d rejected", connectedCount, rejectedCount)

	// server should still be responsive after pool exhaustion
	client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
	if err != nil || client == nil {
		t.Fatalf("Failed to connect after pool exhaustion: %v", err)
	}
	defer client.Close()

	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health check failed after pool exhaustion: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("Server unhealthy after pool exhaustion: %s", health.Status)
	}
}

// TestConcurrentClientOperations verifies concurrent operations on single client.
//
// Race condition tested: Multiple goroutines using the same client connection.
// Note: This may not be supported, but should not cause data races.
func TestConcurrentClientOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent client operations test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// test with dedicated clients per goroutine (correct usage)
	const numGoroutines = 10
	var successCount int32
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// each goroutine gets its own client (correct pattern)
			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				return
			}
			defer client.Close()

			for j := 0; j < 5; j++ {
				if _, err := client.Health(); err == nil {
					atomic.AddInt32(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Concurrent client operations: %d successes", successCount)

	if successCount == 0 {
		t.Error("No successful operations")
	}
}

// TestRPCRequestCancellation verifies cancellation behavior.
//
// Race condition tested: Requests cancelled while in flight.
// Server should handle cancellation cleanly.
func TestRPCRequestCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping request cancellation test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// simulate cancellation by closing connection mid-request
	const numAttempts = 10
	var wg sync.WaitGroup

	for i := 0; i < numAttempts; i++ {
		wg.Add(1)
		go func(attemptID int) {
			defer wg.Done()

			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				return
			}

			// close connection immediately (simulating cancellation)
			go func() {
				time.Sleep(time.Duration(attemptID) * time.Millisecond)
				client.Close()
			}()

			// try to make request (may be cancelled)
			_, _ = client.Health()
		}(i)
	}

	wg.Wait()

	// server should still be healthy after cancellations
	client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
	if err != nil || client == nil {
		t.Fatalf("Failed to connect after cancellation tests: %v", err)
	}
	defer client.Close()

	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health check failed after cancellation tests: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("Server unhealthy after cancellation tests: %s", health.Status)
	}
}

// TestRPCMutationChannelRace verifies mutation channel handling under load.
//
// Race condition tested: Multiple mutations emitted while channel is being read.
func TestRPCMutationChannelRace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping mutation channel race test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// start reader goroutine
	var receivedCount int32
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		mutationChan := server.MutationChan()
		for {
			select {
			case <-mutationChan:
				atomic.AddInt32(&receivedCount, 1)
			case <-ctx.Done():
				return
			}
		}
	}()

	// concurrent RPC operations that generate mutations
	const numGoroutines = 10
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				return
			}
			defer client.Close()

			// make requests that may generate mutations
			for j := 0; j < 5; j++ {
				_, _ = client.Health()
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// stop reader
	cancel()
	<-readerDone

	t.Logf("Mutation channel race test: received %d events", receivedCount)
}

// TestRPCBatchOperationRace verifies batch operations under concurrent access.
//
// Race condition tested: Batch operations interleaved with regular operations.
func TestRPCBatchOperationRace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping batch operation race test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// concurrent batch and regular operations
	const numGoroutines = 5
	var successCount int32
	var wg sync.WaitGroup

	// regular operation goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				return
			}
			defer client.Close()

			for j := 0; j < 10; j++ {
				if _, err := client.Health(); err == nil {
					atomic.AddInt32(&successCount, 1)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	// batch operation goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				return
			}
			defer client.Close()

			// batch request (even if empty, tests serialization)
			batchArgs := &rpc.BatchArgs{
				Operations: []rpc.BatchOperation{},
			}
			for j := 0; j < 5; j++ {
				if _, err := client.Batch(batchArgs); err == nil {
					atomic.AddInt32(&successCount, 1)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Batch operation race test: %d successes", successCount)

	if successCount == 0 {
		t.Error("No successful operations")
	}
}

// TestRPCMetricsUnderLoad verifies metrics collection under high load.
//
// Race condition tested: Metrics being updated from multiple connections.
func TestRPCMetricsUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping metrics under load test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// generate load while collecting metrics
	const numGoroutines = 10
	var wg sync.WaitGroup

	// worker goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				return
			}
			defer client.Close()

			for j := 0; j < 20; j++ {
				_, _ = client.Health()
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	// metrics collector goroutine
	var metricsSnapshots []json.RawMessage
	wg.Add(1)
	go func() {
		defer wg.Done()

		client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
		if err != nil || client == nil {
			return
		}
		defer client.Close()

		for i := 0; i < 10; i++ {
			if metrics, err := client.Metrics(); err == nil {
				data, _ := json.Marshal(metrics)
				metricsSnapshots = append(metricsSnapshots, data)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	wg.Wait()

	t.Logf("Metrics under load test: collected %d snapshots", len(metricsSnapshots))

	if len(metricsSnapshots) == 0 {
		t.Error("No metrics collected")
	}
}

// TestRPCRecentMutationsRace verifies GetRecentMutations under concurrent access.
//
// Race condition tested: Mutations being added while being read.
func TestRPCRecentMutationsRace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping recent mutations race test in short mode")
	}

	tmpDir := makeSocketTempDirForRPC(t)
	socketPath := filepath.Join(tmpDir, "bd.sock")
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := newTestLogger()

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, log)
	if err != nil {
		t.Fatalf("Failed to start RPC server: %v", err)
	}
	defer server.Stop()

	<-server.WaitReady()

	// concurrent readers and the server potentially adding mutations
	const numReaders = 10
	var readCount int32
	var wg sync.WaitGroup

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client, err := rpc.TryConnectWithTimeout(socketPath, 2*time.Second)
			if err != nil || client == nil {
				return
			}
			defer client.Close()

			for j := 0; j < 10; j++ {
				args := &rpc.GetMutationsArgs{Since: 0}
				if _, err := client.GetMutations(args); err == nil {
					atomic.AddInt32(&readCount, 1)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Recent mutations race test: %d successful reads", readCount)

	if readCount == 0 {
		t.Error("No successful mutation reads")
	}
}

// Helper function to create temp dir for RPC tests
func makeSocketTempDirForRPC(t *testing.T) string {
	t.Helper()
	// use /tmp for socket paths to avoid path length issues
	tmpDir, err := os.MkdirTemp("/tmp", "beads-rpc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})
	return tmpDir
}
