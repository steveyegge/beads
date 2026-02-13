//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// podTestEnv holds the daemon test environment for pod integration tests.
type podTestEnv struct {
	ctx        context.Context
	cancel     context.CancelFunc
	client     *rpc.Client
	store      storage.Storage
	socketPath string
	cleanup    func()
}

// connectClient creates a new RPC client connected to the test daemon.
// Use this to get a separate client for concurrent operations.
func (e *podTestEnv) connectClient(t *testing.T) *rpc.Client {
	t.Helper()
	c, err := rpc.TryConnect(e.socketPath)
	if err != nil || c == nil {
		t.Fatalf("Failed to connect RPC client: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// setupDaemonTestEnvForPod sets up a complete daemon test environment for pod integration tests.
func setupDaemonTestEnvForPod(t *testing.T) *podTestEnv {
	t.Helper()

	tmpDir := makeSocketTempDir(t)
	initTestGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	socketPath := filepath.Join(beadsDir, "bd.sock")
	testDBPath := filepath.Join(beadsDir, "beads.db")

	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log := daemonLogger{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, "", "", "", "", "", log)
	if err != nil {
		cancel()
		t.Fatalf("Failed to start RPC server: %v", err)
	}

	select {
	case <-server.WaitReady():
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Server did not become ready")
	}

	client, err := rpc.TryConnect(socketPath)
	if err != nil || client == nil {
		cancel()
		t.Fatalf("Failed to connect RPC client: %v", err)
	}

	cleanup := func() {
		if client != nil {
			client.Close()
		}
		if server != nil {
			server.Stop()
		}
		os.RemoveAll(tmpDir)
	}

	return &podTestEnv{
		ctx:        ctx,
		cancel:     cancel,
		client:     client,
		store:      testStore,
		socketPath: socketPath,
		cleanup:    cleanup,
	}
}

// createTestAgentViaRPC creates an agent bead through the RPC client and adds the gt:agent label.
func createTestAgentViaRPC(t *testing.T, client *rpc.Client, name string) string {
	t.Helper()

	createArgs := &rpc.CreateArgs{
		Title:     name,
		IssueType: "agent",
		Priority:  2,
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("failed to create agent %s: %v", name, err)
	}
	if !resp.Success {
		t.Fatalf("create agent %s failed: %s", name, resp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("failed to unmarshal agent issue: %v", err)
	}

	labelResp, err := client.Execute(rpc.OpLabelAdd, &rpc.LabelAddArgs{
		ID:    issue.ID,
		Label: "gt:agent",
	})
	if err != nil {
		t.Fatalf("failed to add gt:agent label: %v", err)
	}
	if !labelResp.Success {
		t.Fatalf("add label failed: %s", labelResp.Error)
	}

	return issue.ID
}

// getAgentViaRPC fetches an agent bead by ID and returns the issue.
func getAgentViaRPC(t *testing.T, client *rpc.Client, agentID string) *types.Issue {
	t.Helper()

	resp, err := client.Execute(rpc.OpShow, &rpc.ShowArgs{ID: agentID})
	if err != nil {
		t.Fatalf("failed to get agent %s: %v", agentID, err)
	}
	if !resp.Success {
		t.Fatalf("get agent %s failed: %s", agentID, resp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("failed to unmarshal agent: %v", err)
	}
	return &issue
}

// TestPodIntegration_FullLifecycle tests the complete pod field lifecycle through daemon RPC:
// register → show → list → pod-status → show → deregister → show → list
func TestPodIntegration_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client

	// Step 1: Create a test agent bead
	agentID := createTestAgentViaRPC(t, client, "test-agent-lifecycle")

	// Step 2: Register pod
	regResult, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "test-pod-1",
		PodIP:         "10.0.0.5",
		PodNode:       "node-1",
		PodStatus:     "running",
		ScreenSession: "agent-screen",
	})
	if err != nil {
		t.Fatalf("pod-register failed: %v", err)
	}
	if regResult.AgentID != agentID {
		t.Errorf("register result AgentID = %q, want %q", regResult.AgentID, agentID)
	}
	if regResult.PodName != "test-pod-1" {
		t.Errorf("register result PodName = %q, want %q", regResult.PodName, "test-pod-1")
	}
	if regResult.PodStatus != "running" {
		t.Errorf("register result PodStatus = %q, want %q", regResult.PodStatus, "running")
	}

	// Step 3: Show agent and verify pod fields
	agent := getAgentViaRPC(t, client, agentID)
	if agent.PodName != "test-pod-1" {
		t.Errorf("agent.PodName = %q, want %q", agent.PodName, "test-pod-1")
	}
	if agent.PodIP != "10.0.0.5" {
		t.Errorf("agent.PodIP = %q, want %q", agent.PodIP, "10.0.0.5")
	}
	if agent.PodNode != "node-1" {
		t.Errorf("agent.PodNode = %q, want %q", agent.PodNode, "node-1")
	}
	if agent.PodStatus != "running" {
		t.Errorf("agent.PodStatus = %q, want %q", agent.PodStatus, "running")
	}
	if agent.ScreenSession != "agent-screen" {
		t.Errorf("agent.ScreenSession = %q, want %q", agent.ScreenSession, "agent-screen")
	}

	// Step 4: Verify agent appears in pod-list
	listResult, err := client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list failed: %v", err)
	}
	found := false
	for _, a := range listResult.Agents {
		if a.AgentID == agentID {
			found = true
			if a.PodName != "test-pod-1" {
				t.Errorf("list entry PodName = %q, want %q", a.PodName, "test-pod-1")
			}
			if a.PodIP != "10.0.0.5" {
				t.Errorf("list entry PodIP = %q, want %q", a.PodIP, "10.0.0.5")
			}
			if a.PodNode != "node-1" {
				t.Errorf("list entry PodNode = %q, want %q", a.PodNode, "node-1")
			}
			if a.PodStatus != "running" {
				t.Errorf("list entry PodStatus = %q, want %q", a.PodStatus, "running")
			}
			break
		}
	}
	if !found {
		t.Errorf("agent %s not found in pod-list", agentID)
	}

	// Step 5: Update pod status to "failed"
	statusResult, err := client.AgentPodStatus(&rpc.AgentPodStatusArgs{
		AgentID:   agentID,
		PodStatus: "failed",
	})
	if err != nil {
		t.Fatalf("pod-status failed: %v", err)
	}
	if statusResult.PodStatus != "failed" {
		t.Errorf("status result PodStatus = %q, want %q", statusResult.PodStatus, "failed")
	}

	// Step 6: Show agent and verify status updated, other fields preserved
	agent = getAgentViaRPC(t, client, agentID)
	if agent.PodStatus != "failed" {
		t.Errorf("agent.PodStatus after update = %q, want %q", agent.PodStatus, "failed")
	}
	if agent.PodName != "test-pod-1" {
		t.Errorf("agent.PodName should be preserved = %q, want %q", agent.PodName, "test-pod-1")
	}
	if agent.PodIP != "10.0.0.5" {
		t.Errorf("agent.PodIP should be preserved = %q, want %q", agent.PodIP, "10.0.0.5")
	}

	// Step 7: Deregister pod
	deregResult, err := client.AgentPodDeregister(&rpc.AgentPodDeregisterArgs{
		AgentID: agentID,
	})
	if err != nil {
		t.Fatalf("pod-deregister failed: %v", err)
	}
	if deregResult.AgentID != agentID {
		t.Errorf("deregister result AgentID = %q, want %q", deregResult.AgentID, agentID)
	}

	// Step 8: Show agent and verify pod fields cleared
	agent = getAgentViaRPC(t, client, agentID)
	if agent.PodName != "" {
		t.Errorf("agent.PodName after deregister = %q, want empty", agent.PodName)
	}
	if agent.PodIP != "" {
		t.Errorf("agent.PodIP after deregister = %q, want empty", agent.PodIP)
	}
	if agent.PodNode != "" {
		t.Errorf("agent.PodNode after deregister = %q, want empty", agent.PodNode)
	}
	if agent.PodStatus != "" {
		t.Errorf("agent.PodStatus after deregister = %q, want empty", agent.PodStatus)
	}
	if agent.ScreenSession != "" {
		t.Errorf("agent.ScreenSession after deregister = %q, want empty", agent.ScreenSession)
	}

	// Step 9: Verify agent no longer in pod-list
	listResult, err = client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list after deregister failed: %v", err)
	}
	for _, a := range listResult.Agents {
		if a.AgentID == agentID {
			t.Errorf("agent %s should not appear in pod-list after deregister", agentID)
		}
	}
}

// TestPodIntegration_FieldsSurviveDaemonRestart verifies pod fields persist across daemon restarts.
func TestPodIntegration_FieldsSurviveDaemonRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir := makeSocketTempDir(t)
	defer os.RemoveAll(tmpDir)
	initTestGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	log := daemonLogger{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	// --- Phase 1: Start daemon, register pod ---
	socketPath1 := filepath.Join(beadsDir, "bd1.sock")
	ctx1, cancel1 := context.WithTimeout(context.Background(), 15*time.Second)

	server1, _, err := startRPCServer(ctx1, socketPath1, testStore, tmpDir, testDBPath, "", "", "", "", "", log)
	if err != nil {
		cancel1()
		t.Fatalf("Failed to start RPC server (phase 1): %v", err)
	}

	select {
	case <-server1.WaitReady():
	case <-time.After(5 * time.Second):
		cancel1()
		t.Fatal("Server 1 did not become ready")
	}

	client1, err := rpc.TryConnect(socketPath1)
	if err != nil || client1 == nil {
		cancel1()
		t.Fatalf("Failed to connect RPC client (phase 1): %v", err)
	}

	agentID := createTestAgentViaRPC(t, client1, "restart-test-agent")

	_, err = client1.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "restart-pod-1",
		PodIP:         "10.0.1.1",
		PodNode:       "node-restart",
		PodStatus:     "running",
		ScreenSession: "restart-screen",
	})
	if err != nil {
		t.Fatalf("pod-register (phase 1) failed: %v", err)
	}

	// Verify fields are set
	agent := getAgentViaRPC(t, client1, agentID)
	if agent.PodName != "restart-pod-1" {
		t.Fatalf("agent.PodName = %q before restart, want %q", agent.PodName, "restart-pod-1")
	}

	// Stop phase 1 daemon (this closes testStore via server.Stop)
	client1.Close()
	server1.Stop()
	cancel1()

	// Brief pause for socket cleanup
	time.Sleep(200 * time.Millisecond)

	// --- Phase 2: Start new daemon with fresh store, verify fields persist ---
	socketPath2 := filepath.Join(beadsDir, "bd2.sock")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()

	// Open a fresh store pointing at the same database file (previous was closed by server1.Stop)
	testStore2 := newTestStore(t, testDBPath)

	server2, _, err := startRPCServer(ctx2, socketPath2, testStore2, tmpDir, testDBPath, "", "", "", "", "", log)
	if err != nil {
		t.Fatalf("Failed to start RPC server (phase 2): %v", err)
	}
	defer server2.Stop()

	select {
	case <-server2.WaitReady():
	case <-time.After(5 * time.Second):
		t.Fatal("Server 2 did not become ready")
	}

	client2, err := rpc.TryConnect(socketPath2)
	if err != nil || client2 == nil {
		t.Fatalf("Failed to connect RPC client (phase 2): %v", err)
	}
	defer client2.Close()

	// Verify all pod fields survived the restart
	agent = getAgentViaRPC(t, client2, agentID)
	if agent.PodName != "restart-pod-1" {
		t.Errorf("after restart: PodName = %q, want %q", agent.PodName, "restart-pod-1")
	}
	if agent.PodIP != "10.0.1.1" {
		t.Errorf("after restart: PodIP = %q, want %q", agent.PodIP, "10.0.1.1")
	}
	if agent.PodNode != "node-restart" {
		t.Errorf("after restart: PodNode = %q, want %q", agent.PodNode, "node-restart")
	}
	if agent.PodStatus != "running" {
		t.Errorf("after restart: PodStatus = %q, want %q", agent.PodStatus, "running")
	}
	if agent.ScreenSession != "restart-screen" {
		t.Errorf("after restart: ScreenSession = %q, want %q", agent.ScreenSession, "restart-screen")
	}

	// Verify pod-list also works after restart
	listResult, err := client2.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list after restart failed: %v", err)
	}
	found := false
	for _, a := range listResult.Agents {
		if a.AgentID == agentID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent %s should appear in pod-list after daemon restart", agentID)
	}
}

// TestPodIntegration_ConcurrentRegistration verifies that concurrent pod-register
// from multiple agents doesn't corrupt data.
func TestPodIntegration_ConcurrentRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	numAgents := 10
	agentIDs := make([]string, numAgents)

	// Create all agents first (sequential, single client)
	for i := 0; i < numAgents; i++ {
		agentIDs[i] = createTestAgentViaRPC(t, env.client, fmt.Sprintf("concurrent-agent-%d", i))
	}

	// Register pods concurrently - each goroutine gets its own client
	// (the RPC client is not goroutine-safe since it shares a single connection)
	var wg sync.WaitGroup
	errChan := make(chan error, numAgents)

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := rpc.TryConnect(env.socketPath)
			if err != nil || c == nil {
				errChan <- fmt.Errorf("agent %d connect failed: %v", idx, err)
				return
			}
			defer c.Close()

			_, err = c.AgentPodRegister(&rpc.AgentPodRegisterArgs{
				AgentID:       agentIDs[idx],
				PodName:       fmt.Sprintf("pod-%d", idx),
				PodIP:         fmt.Sprintf("10.0.0.%d", idx+10),
				PodNode:       fmt.Sprintf("node-%d", idx%3),
				PodStatus:     "running",
				ScreenSession: fmt.Sprintf("screen-%d", idx),
			})
			if err != nil {
				errChan <- fmt.Errorf("agent %d pod-register failed: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}

	// Verify all agents have correct pod fields
	listResult, err := env.client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list failed: %v", err)
	}

	if len(listResult.Agents) != numAgents {
		t.Errorf("pod-list returned %d agents, want %d", len(listResult.Agents), numAgents)
	}

	// Build map for easy lookup
	agentMap := make(map[string]rpc.AgentPodInfo)
	for _, a := range listResult.Agents {
		agentMap[a.AgentID] = a
	}

	// Verify each agent has the correct fields
	for i := 0; i < numAgents; i++ {
		info, ok := agentMap[agentIDs[i]]
		if !ok {
			t.Errorf("agent %d (%s) not found in pod-list", i, agentIDs[i])
			continue
		}
		expectedPodName := fmt.Sprintf("pod-%d", i)
		if info.PodName != expectedPodName {
			t.Errorf("agent %d PodName = %q, want %q", i, info.PodName, expectedPodName)
		}
		expectedIP := fmt.Sprintf("10.0.0.%d", i+10)
		if info.PodIP != expectedIP {
			t.Errorf("agent %d PodIP = %q, want %q", i, info.PodIP, expectedIP)
		}
		if info.PodStatus != "running" {
			t.Errorf("agent %d PodStatus = %q, want %q", i, info.PodStatus, "running")
		}
	}
}

// TestPodIntegration_ConcurrentRegisterDeregister verifies concurrent register and deregister
// operations on different agents don't interfere with each other.
func TestPodIntegration_ConcurrentRegisterDeregister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client

	numAgents := 6
	agentIDs := make([]string, numAgents)
	for i := 0; i < numAgents; i++ {
		agentIDs[i] = createTestAgentViaRPC(t, client, fmt.Sprintf("mix-agent-%d", i))
	}

	// Register all pods first
	for i := 0; i < numAgents; i++ {
		_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
			AgentID:   agentIDs[i],
			PodName:   fmt.Sprintf("mix-pod-%d", i),
			PodIP:     fmt.Sprintf("10.1.0.%d", i+10),
			PodNode:   "node-mix",
			PodStatus: "running",
		})
		if err != nil {
			t.Fatalf("initial register agent %d failed: %v", i, err)
		}
	}

	// Concurrently: deregister even-indexed agents, re-register odd-indexed agents with new data
	// Each goroutine gets its own client (RPC client is not goroutine-safe)
	var wg sync.WaitGroup
	errChan := make(chan error, numAgents)

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := rpc.TryConnect(env.socketPath)
			if err != nil || c == nil {
				errChan <- fmt.Errorf("agent %d connect failed: %v", idx, err)
				return
			}
			defer c.Close()

			if idx%2 == 0 {
				// Deregister
				_, err := c.AgentPodDeregister(&rpc.AgentPodDeregisterArgs{
					AgentID: agentIDs[idx],
				})
				if err != nil {
					errChan <- fmt.Errorf("deregister agent %d failed: %w", idx, err)
				}
			} else {
				// Re-register with new pod name
				_, err := c.AgentPodRegister(&rpc.AgentPodRegisterArgs{
					AgentID:   agentIDs[idx],
					PodName:   fmt.Sprintf("new-pod-%d", idx),
					PodIP:     fmt.Sprintf("10.2.0.%d", idx+10),
					PodNode:   "node-new",
					PodStatus: "running",
				})
				if err != nil {
					errChan <- fmt.Errorf("re-register agent %d failed: %w", idx, err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}

	// Verify results
	listResult, err := client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list failed: %v", err)
	}

	agentMap := make(map[string]rpc.AgentPodInfo)
	for _, a := range listResult.Agents {
		agentMap[a.AgentID] = a
	}

	for i := 0; i < numAgents; i++ {
		if i%2 == 0 {
			// Even: should be deregistered (not in list)
			if _, ok := agentMap[agentIDs[i]]; ok {
				t.Errorf("agent %d should be deregistered but found in pod-list", i)
			}
		} else {
			// Odd: should have new pod data
			info, ok := agentMap[agentIDs[i]]
			if !ok {
				t.Errorf("agent %d should be in pod-list but not found", i)
				continue
			}
			expectedPodName := fmt.Sprintf("new-pod-%d", i)
			if info.PodName != expectedPodName {
				t.Errorf("agent %d PodName = %q, want %q", i, info.PodName, expectedPodName)
			}
		}
	}
}

// TestPodIntegration_RegisterLatency verifies pod-register completes in < 100ms.
func TestPodIntegration_RegisterLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client
	agentID := createTestAgentViaRPC(t, client, "latency-test-agent")

	// Warm up: first call may include connection setup overhead
	_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:   agentID,
		PodName:   "warmup-pod",
		PodStatus: "running",
	})
	if err != nil {
		t.Fatalf("warmup pod-register failed: %v", err)
	}

	// Measure latency over multiple iterations
	iterations := 20
	var totalDuration time.Duration

	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
			AgentID:       agentID,
			PodName:       fmt.Sprintf("latency-pod-%d", i),
			PodIP:         fmt.Sprintf("10.0.0.%d", i+1),
			PodNode:       "node-latency",
			PodStatus:     "running",
			ScreenSession: fmt.Sprintf("screen-%d", i),
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("pod-register iteration %d failed: %v", i, err)
		}
		totalDuration += elapsed

		if elapsed > 100*time.Millisecond {
			t.Errorf("pod-register iteration %d took %v (> 100ms)", i, elapsed)
		}
	}

	avgLatency := totalDuration / time.Duration(iterations)
	t.Logf("pod-register average latency: %v over %d iterations", avgLatency, iterations)

	if avgLatency > 100*time.Millisecond {
		t.Errorf("average pod-register latency %v exceeds 100ms threshold", avgLatency)
	}
}

// TestPodIntegration_DeregisterIdempotent verifies deregister is idempotent.
func TestPodIntegration_DeregisterIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client
	agentID := createTestAgentViaRPC(t, client, "idempotent-agent")

	// Register pod
	_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:   agentID,
		PodName:   "idempotent-pod",
		PodStatus: "running",
	})
	if err != nil {
		t.Fatalf("pod-register failed: %v", err)
	}

	// Deregister twice
	_, err = client.AgentPodDeregister(&rpc.AgentPodDeregisterArgs{AgentID: agentID})
	if err != nil {
		t.Fatalf("first deregister failed: %v", err)
	}
	_, err = client.AgentPodDeregister(&rpc.AgentPodDeregisterArgs{AgentID: agentID})
	if err != nil {
		t.Fatalf("second deregister (idempotent) failed: %v", err)
	}

	// Verify fields are cleared
	agent := getAgentViaRPC(t, client, agentID)
	if agent.PodName != "" {
		t.Errorf("PodName should be empty after double deregister, got %q", agent.PodName)
	}
}

// TestPodIntegration_ReRegisterOverwrite verifies re-registering a pod overwrites previous data.
func TestPodIntegration_ReRegisterOverwrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client
	agentID := createTestAgentViaRPC(t, client, "overwrite-agent")

	// Register with initial data
	_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "old-pod",
		PodIP:         "10.0.0.1",
		PodNode:       "old-node",
		PodStatus:     "running",
		ScreenSession: "old-screen",
	})
	if err != nil {
		t.Fatalf("first pod-register failed: %v", err)
	}

	// Re-register with new data
	_, err = client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "new-pod",
		PodIP:         "10.0.0.2",
		PodNode:       "new-node",
		PodStatus:     "pending",
		ScreenSession: "new-screen",
	})
	if err != nil {
		t.Fatalf("second pod-register failed: %v", err)
	}

	// Verify new data
	agent := getAgentViaRPC(t, client, agentID)
	if agent.PodName != "new-pod" {
		t.Errorf("PodName = %q, want %q", agent.PodName, "new-pod")
	}
	if agent.PodIP != "10.0.0.2" {
		t.Errorf("PodIP = %q, want %q", agent.PodIP, "10.0.0.2")
	}
	if agent.PodNode != "new-node" {
		t.Errorf("PodNode = %q, want %q", agent.PodNode, "new-node")
	}
	if agent.PodStatus != "pending" {
		t.Errorf("PodStatus = %q, want %q", agent.PodStatus, "pending")
	}
	if agent.ScreenSession != "new-screen" {
		t.Errorf("ScreenSession = %q, want %q", agent.ScreenSession, "new-screen")
	}

	// Verify only one entry in pod-list (not two)
	listResult, err := client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list failed: %v", err)
	}
	count := 0
	for _, a := range listResult.Agents {
		if a.AgentID == agentID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry in pod-list for agent, got %d", count)
	}
}

// TestPodIntegration_PodListFilterByRig verifies pod-list rig filtering through daemon.
func TestPodIntegration_PodListFilterByRig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client
	store := env.store
	ctx := context.Background()

	// Create agents in different rigs
	agent1ID := createTestAgentViaRPC(t, client, "rig-a-agent")
	agent2ID := createTestAgentViaRPC(t, client, "rig-b-agent")
	agent3ID := createTestAgentViaRPC(t, client, "rig-a-agent-2")

	// Set rig field directly on the store (rig is set via agent metadata, not pod-register)
	if err := store.UpdateIssue(ctx, agent1ID, map[string]interface{}{"rig": "rig-a"}, "test"); err != nil {
		t.Fatalf("failed to set rig on agent1: %v", err)
	}
	if err := store.UpdateIssue(ctx, agent2ID, map[string]interface{}{"rig": "rig-b"}, "test"); err != nil {
		t.Fatalf("failed to set rig on agent2: %v", err)
	}
	if err := store.UpdateIssue(ctx, agent3ID, map[string]interface{}{"rig": "rig-a"}, "test"); err != nil {
		t.Fatalf("failed to set rig on agent3: %v", err)
	}

	// Register pods on all agents
	for _, pair := range []struct {
		id      string
		podName string
	}{
		{agent1ID, "rig-a-pod-1"},
		{agent2ID, "rig-b-pod-1"},
		{agent3ID, "rig-a-pod-2"},
	} {
		_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
			AgentID:   pair.id,
			PodName:   pair.podName,
			PodStatus: "running",
		})
		if err != nil {
			t.Fatalf("pod-register %s failed: %v", pair.podName, err)
		}
	}

	// List all pods (no filter)
	allResult, err := client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list (all) failed: %v", err)
	}
	if len(allResult.Agents) != 3 {
		t.Errorf("pod-list (all) returned %d agents, want 3", len(allResult.Agents))
	}

	// Filter by rig-a
	rigAResult, err := client.AgentPodList(&rpc.AgentPodListArgs{Rig: "rig-a"})
	if err != nil {
		t.Fatalf("pod-list (rig-a) failed: %v", err)
	}
	if len(rigAResult.Agents) != 2 {
		t.Errorf("pod-list (rig-a) returned %d agents, want 2", len(rigAResult.Agents))
	}
	for _, a := range rigAResult.Agents {
		if a.Rig != "rig-a" {
			t.Errorf("rig-a filter returned agent with rig=%q", a.Rig)
		}
	}

	// Filter by rig-b
	rigBResult, err := client.AgentPodList(&rpc.AgentPodListArgs{Rig: "rig-b"})
	if err != nil {
		t.Fatalf("pod-list (rig-b) failed: %v", err)
	}
	if len(rigBResult.Agents) != 1 {
		t.Errorf("pod-list (rig-b) returned %d agents, want 1", len(rigBResult.Agents))
	}

	// Filter by non-existent rig
	emptyResult, err := client.AgentPodList(&rpc.AgentPodListArgs{Rig: "rig-nonexistent"})
	if err != nil {
		t.Fatalf("pod-list (nonexistent) failed: %v", err)
	}
	if len(emptyResult.Agents) != 0 {
		t.Errorf("pod-list (nonexistent) returned %d agents, want 0", len(emptyResult.Agents))
	}
}

// TestPodIntegration_MultipleAgentsSameNode verifies multiple agents can register pods on the same node.
func TestPodIntegration_MultipleAgentsSameNode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client
	numAgents := 5
	agentIDs := make([]string, numAgents)

	for i := 0; i < numAgents; i++ {
		agentIDs[i] = createTestAgentViaRPC(t, client, fmt.Sprintf("same-node-agent-%d", i))
		_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
			AgentID:   agentIDs[i],
			PodName:   fmt.Sprintf("same-node-pod-%d", i),
			PodIP:     fmt.Sprintf("10.0.0.%d", i+20),
			PodNode:   "shared-node",
			PodStatus: "running",
		})
		if err != nil {
			t.Fatalf("pod-register agent %d failed: %v", i, err)
		}
	}

	listResult, err := client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		t.Fatalf("pod-list failed: %v", err)
	}
	if len(listResult.Agents) != numAgents {
		t.Errorf("pod-list returned %d agents, want %d", len(listResult.Agents), numAgents)
	}

	// Verify all agents share the same node
	for _, a := range listResult.Agents {
		if a.PodNode != "shared-node" {
			t.Errorf("agent %s PodNode = %q, want %q", a.AgentID, a.PodNode, "shared-node")
		}
	}
}

// TestPodIntegration_StatusTransitions verifies sequential status transitions.
func TestPodIntegration_StatusTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupDaemonTestEnvForPod(t)
	defer env.cancel()
	defer env.cleanup()

	client := env.client
	agentID := createTestAgentViaRPC(t, client, "status-transitions-agent")

	// Register with initial status
	_, err := client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:   agentID,
		PodName:   "transitions-pod",
		PodIP:     "10.0.0.100",
		PodNode:   "node-0",
		PodStatus: "pending",
	})
	if err != nil {
		t.Fatalf("pod-register failed: %v", err)
	}

	// Walk through status transitions
	statuses := []string{"running", "terminating", "failed", "succeeded"}
	for _, status := range statuses {
		result, err := client.AgentPodStatus(&rpc.AgentPodStatusArgs{
			AgentID:   agentID,
			PodStatus: status,
		})
		if err != nil {
			t.Fatalf("pod-status %q failed: %v", status, err)
		}
		if result.PodStatus != status {
			t.Errorf("pod-status result = %q, want %q", result.PodStatus, status)
		}

		agent := getAgentViaRPC(t, client, agentID)
		if agent.PodStatus != status {
			t.Errorf("agent.PodStatus after %q transition = %q", status, agent.PodStatus)
		}
		// Verify other fields are preserved
		if agent.PodName != "transitions-pod" {
			t.Errorf("PodName changed during %q transition: %q", status, agent.PodName)
		}
		if agent.PodIP != "10.0.0.100" {
			t.Errorf("PodIP changed during %q transition: %q", status, agent.PodIP)
		}
	}
}
