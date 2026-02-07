package controller

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// =============================================================================
// Mock BeadsClient
// =============================================================================

// mockBeadsClient implements BeadsClientInterface for testing.
type mockBeadsClient struct {
	mu sync.Mutex

	spawningAgents []types.Issue
	doneAgents     []types.Issue
	registeredPods []rpc.AgentPodInfo

	// Error injection
	listSpawningErr  error
	listDoneErr      error
	listRegisteredErr error
	registerPodErr   error
	deregisterPodErr error
	updateStatusErr  error

	// Call recording
	registeredCalls   []registerPodCall
	deregisteredCalls []string // agent IDs
	statusUpdates     []statusUpdateCall
}

type registerPodCall struct {
	agentID, podName, podIP, podNode string
}

type statusUpdateCall struct {
	agentID, status string
}

func (m *mockBeadsClient) ListSpawningAgents() ([]types.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listSpawningErr != nil {
		return nil, m.listSpawningErr
	}
	return m.spawningAgents, nil
}

func (m *mockBeadsClient) ListDoneAgents() ([]types.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listDoneErr != nil {
		return nil, m.listDoneErr
	}
	return m.doneAgents, nil
}

func (m *mockBeadsClient) ListRegisteredPods() ([]rpc.AgentPodInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listRegisteredErr != nil {
		return nil, m.listRegisteredErr
	}
	return m.registeredPods, nil
}

func (m *mockBeadsClient) RegisterPod(agentID, podName, podIP, podNode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.registerPodErr != nil {
		return m.registerPodErr
	}
	m.registeredCalls = append(m.registeredCalls, registerPodCall{agentID, podName, podIP, podNode})
	return nil
}

func (m *mockBeadsClient) DeregisterPod(agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deregisterPodErr != nil {
		return m.deregisterPodErr
	}
	m.deregisteredCalls = append(m.deregisteredCalls, agentID)
	return nil
}

func (m *mockBeadsClient) UpdatePodStatus(agentID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateStatusErr != nil {
		return m.updateStatusErr
	}
	m.statusUpdates = append(m.statusUpdates, statusUpdateCall{agentID, status})
	return nil
}

// =============================================================================
// Test Helpers
// =============================================================================

// newTestController creates a Controller with fake K8s client and mock beads client.
func newTestController(t *testing.T, mock *mockBeadsClient) (*Controller, *fake.Clientset) {
	t.Helper()
	fakeClient := fake.NewSimpleClientset()
	k8sClient := NewK8sClientFromClientset(fakeClient, "test-ns")
	logger := log.New(os.Stderr, "[test] ", log.LstdFlags)
	ctrl := New(k8sClient, mock, Config{
		ReconcileInterval: 100 * time.Millisecond,
		StaleThreshold:    15 * time.Minute,
		PodTemplate: PodTemplateConfig{
			Image:     "test-image:latest",
			Namespace: "test-ns",
		},
	}, logger)
	return ctrl, fakeClient
}

// makeAgentPod creates a K8s pod that looks like an agent pod.
func makeAgentPod(agentID, podName, namespace string, phase corev1.PodPhase) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				LabelApp:       LabelAppValue,
				LabelAgent:     agentID,
				LabelManagedBy: LabelManagedByValue,
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
			PodIP: "10.0.0.1",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}
	return pod
}

// makeSpawningAgent creates an Issue with agent_state=spawning.
func makeSpawningAgent(id, role, rig string) types.Issue {
	return types.Issue{
		ID:         id,
		AgentState: types.StateSpawning,
		RoleType:   role,
		Rig:        rig,
	}
}

// makeDoneAgent creates an Issue with the given done state and a pod name.
func makeDoneAgent(id string, state types.AgentState, podName string) types.Issue {
	return types.Issue{
		ID:         id,
		AgentState: state,
		PodName:    podName,
	}
}

// =============================================================================
// Reconciliation Tests
// =============================================================================

func TestReconcile_EmptyState(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, _ := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce with empty state should not error, got: %v", err)
	}

	if len(mock.registeredCalls) != 0 {
		t.Errorf("expected no register calls, got %d", len(mock.registeredCalls))
	}
	if len(mock.deregisteredCalls) != 0 {
		t.Errorf("expected no deregister calls, got %d", len(mock.deregisteredCalls))
	}
}

func TestReconcile_NewAgentNeedsPod(t *testing.T) {
	mock := &mockBeadsClient{
		spawningAgents: []types.Issue{
			makeSpawningAgent("agent-001", "polecat", "beads"),
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// Verify pod was created in K8s
	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods.Items))
	}
	pod := pods.Items[0]
	if pod.Name != "agent-agent-001" {
		t.Errorf("expected pod name 'agent-agent-001', got %q", pod.Name)
	}
	if pod.Labels[LabelAgent] != "agent-001" {
		t.Errorf("expected agent label 'agent-001', got %q", pod.Labels[LabelAgent])
	}

	// Verify pod was registered in beads
	if len(mock.registeredCalls) != 1 {
		t.Fatalf("expected 1 register call, got %d", len(mock.registeredCalls))
	}
	call := mock.registeredCalls[0]
	if call.agentID != "agent-001" {
		t.Errorf("expected register for 'agent-001', got %q", call.agentID)
	}
	if call.podName != "agent-agent-001" {
		t.Errorf("expected pod name 'agent-agent-001', got %q", call.podName)
	}
}

func TestReconcile_AgentDoneDeletesPod(t *testing.T) {
	mock := &mockBeadsClient{
		doneAgents: []types.Issue{
			makeDoneAgent("agent-002", types.StateDone, "agent-agent-002"),
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	// Pre-create the pod in K8s so it can be deleted
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(),
		makeAgentPod("agent-002", "agent-agent-002", "test-ns", corev1.PodRunning),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to pre-create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// Verify pod was deleted from K8s
	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Errorf("expected 0 pods after deletion, got %d", len(pods.Items))
	}

	// Verify deregistered
	if len(mock.deregisteredCalls) != 1 {
		t.Fatalf("expected 1 deregister call, got %d", len(mock.deregisteredCalls))
	}
	if mock.deregisteredCalls[0] != "agent-002" {
		t.Errorf("expected deregister for 'agent-002', got %q", mock.deregisteredCalls[0])
	}
}

func TestReconcile_ActiveAgentRunningPod_NoAction(t *testing.T) {
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "agent-003", PodName: "agent-agent-003", AgentState: "running"},
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	// Pre-create a running pod
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(),
		makeAgentPod("agent-003", "agent-agent-003", "test-ns", corev1.PodRunning),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to pre-create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// Should not create, delete, or update anything
	if len(mock.registeredCalls) != 0 {
		t.Errorf("expected no register calls, got %d", len(mock.registeredCalls))
	}
	if len(mock.deregisteredCalls) != 0 {
		t.Errorf("expected no deregister calls, got %d", len(mock.deregisteredCalls))
	}
	if len(mock.statusUpdates) != 0 {
		t.Errorf("expected no status updates, got %d", len(mock.statusUpdates))
	}
}

func TestReconcile_ActiveAgentMissingPod_CreatesPod(t *testing.T) {
	// Agent is spawning but has no pod in K8s (recovery scenario)
	mock := &mockBeadsClient{
		spawningAgents: []types.Issue{
			makeSpawningAgent("agent-004", "crew", "gastown"),
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	// No pods pre-created - simulates pod disappearance

	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// Pod should be created
	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 1 {
		t.Fatalf("expected 1 pod created for recovery, got %d", len(pods.Items))
	}
	if pods.Items[0].Name != "agent-agent-004" {
		t.Errorf("expected pod name 'agent-agent-004', got %q", pods.Items[0].Name)
	}
}

func TestReconcile_DoneAgentPodAlreadyGone_Idempotent(t *testing.T) {
	// Agent is done but the pod is already gone from K8s.
	// The controller handles this in two paths:
	// Step 4: done agent with no pod → deregisters orphaned record
	// Step 5: registered pod not in K8s → deregisters missing pod
	// Both paths fire, which is idempotent and safe.
	mock := &mockBeadsClient{
		doneAgents: []types.Issue{
			makeDoneAgent("agent-005", types.StateDone, "agent-agent-005"),
		},
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "agent-005", PodName: "agent-agent-005"},
		},
	}
	ctrl, _ := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// Both Step 4 (done agent cleanup) and Step 5 (missing pod deregistration) fire
	if len(mock.deregisteredCalls) != 2 {
		t.Fatalf("expected 2 deregister calls (step 4 + step 5), got %d", len(mock.deregisteredCalls))
	}
	for _, agentID := range mock.deregisteredCalls {
		if agentID != "agent-005" {
			t.Errorf("expected deregister for 'agent-005', got %q", agentID)
		}
	}
}

func TestReconcile_DoneAgentNoPod_NotRegistered_NoAction(t *testing.T) {
	// Agent is done, pod already gone from K8s, and not in registered list
	mock := &mockBeadsClient{
		doneAgents: []types.Issue{
			makeDoneAgent("agent-clean", types.StateDone, "agent-agent-clean"),
		},
	}
	ctrl, _ := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// No pod in K8s, not in registered list → just deregisters from done agent path
	// (registeredByAgent check returns false, so no deregister from step 4 either)
	if len(mock.deregisteredCalls) != 0 {
		t.Errorf("expected 0 deregister calls when not in registered list, got %d", len(mock.deregisteredCalls))
	}
}

func TestReconcile_MultipleAgentsMixedStates(t *testing.T) {
	mock := &mockBeadsClient{
		spawningAgents: []types.Issue{
			makeSpawningAgent("new-agent-1", "polecat", "beads"),
			makeSpawningAgent("new-agent-2", "crew", "gastown"),
		},
		doneAgents: []types.Issue{
			makeDoneAgent("old-agent-1", types.StateDone, "agent-old-agent-1"),
			makeDoneAgent("old-agent-2", types.StateStopped, "agent-old-agent-2"),
		},
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "running-agent", PodName: "agent-running-agent", AgentState: "running"},
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	// Pre-create pods for done agents and running agent
	for _, pod := range []*corev1.Pod{
		makeAgentPod("old-agent-1", "agent-old-agent-1", "test-ns", corev1.PodRunning),
		makeAgentPod("old-agent-2", "agent-old-agent-2", "test-ns", corev1.PodRunning),
		makeAgentPod("running-agent", "agent-running-agent", "test-ns", corev1.PodRunning),
	} {
		_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to pre-create pod %s: %v", pod.Name, err)
		}
	}

	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// 2 new pods created
	if len(mock.registeredCalls) != 2 {
		t.Errorf("expected 2 register calls for new agents, got %d", len(mock.registeredCalls))
	}

	// 2 done agents deregistered
	if len(mock.deregisteredCalls) != 2 {
		t.Errorf("expected 2 deregister calls for done agents, got %d", len(mock.deregisteredCalls))
	}
}

func TestReconcile_SpawningAgentAlreadyHasPod_Skips(t *testing.T) {
	mock := &mockBeadsClient{
		spawningAgents: []types.Issue{
			makeSpawningAgent("agent-006", "polecat", "beads"),
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	// Pre-create the pod - agent already has one
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(),
		makeAgentPod("agent-006", "agent-agent-006", "test-ns", corev1.PodRunning),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to pre-create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// Should not create another pod
	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 1 {
		t.Errorf("expected still 1 pod (not duplicated), got %d", len(pods.Items))
	}

	// No registration call
	if len(mock.registeredCalls) != 0 {
		t.Errorf("expected no register calls for existing pod, got %d", len(mock.registeredCalls))
	}
}

func TestReconcile_StoppedAgentDeletesPod(t *testing.T) {
	mock := &mockBeadsClient{
		doneAgents: []types.Issue{
			makeDoneAgent("agent-007", types.StateStopped, "agent-agent-007"),
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(),
		makeAgentPod("agent-007", "agent-agent-007", "test-ns", corev1.PodRunning),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to pre-create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Errorf("expected 0 pods after stopped agent cleanup, got %d", len(pods.Items))
	}
}

func TestReconcile_DeadAgentDeletesPod(t *testing.T) {
	mock := &mockBeadsClient{
		doneAgents: []types.Issue{
			makeDoneAgent("agent-008", types.StateDead, "agent-agent-008"),
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(),
		makeAgentPod("agent-008", "agent-agent-008", "test-ns", corev1.PodRunning),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to pre-create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Errorf("expected 0 pods after dead agent cleanup, got %d", len(pods.Items))
	}
}

// =============================================================================
// Reconciliation Error Handling Tests
// =============================================================================

func TestReconcile_ListSpawningError(t *testing.T) {
	mock := &mockBeadsClient{
		listSpawningErr: fmt.Errorf("connection refused"),
	}
	ctrl, _ := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err == nil {
		t.Fatal("expected error when ListSpawningAgents fails")
	}
	if !strings.Contains(err.Error(), "spawning") {
		t.Errorf("error should mention spawning, got: %v", err)
	}
}

func TestReconcile_ListDoneError(t *testing.T) {
	mock := &mockBeadsClient{
		listDoneErr: fmt.Errorf("connection refused"),
	}
	ctrl, _ := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err == nil {
		t.Fatal("expected error when ListDoneAgents fails")
	}
	if !strings.Contains(err.Error(), "done") {
		t.Errorf("error should mention done, got: %v", err)
	}
}

func TestReconcile_ListRegisteredError(t *testing.T) {
	mock := &mockBeadsClient{
		listRegisteredErr: fmt.Errorf("connection refused"),
	}
	ctrl, _ := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err == nil {
		t.Fatal("expected error when ListRegisteredPods fails")
	}
	if !strings.Contains(err.Error(), "registered") {
		t.Errorf("error should mention registered, got: %v", err)
	}
}

func TestReconcile_RegisterPodError_ContinuesReconciliation(t *testing.T) {
	mock := &mockBeadsClient{
		spawningAgents: []types.Issue{
			makeSpawningAgent("agent-fail", "polecat", "beads"),
			makeSpawningAgent("agent-ok", "polecat", "beads"),
		},
		registerPodErr: fmt.Errorf("registration failed"),
	}
	ctrl, fakeClient := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	// Reconciliation should not fail, it logs and continues
	if err != nil {
		t.Fatalf("reconcileOnce should not fail on individual pod errors, got: %v", err)
	}

	// Both pods should be attempted (created in K8s) even though registration fails
	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 2 {
		t.Errorf("expected 2 pods created even with registration errors, got %d", len(pods.Items))
	}
}

func TestReconcile_RegisteredPodMissingInK8s_Deregisters(t *testing.T) {
	// Pod is registered in beads but not found in K8s
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "ghost-agent", PodName: "agent-ghost-agent"},
		},
	}
	ctrl, _ := newTestController(t, mock)

	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	if len(mock.deregisteredCalls) != 1 {
		t.Fatalf("expected 1 deregister for missing pod, got %d", len(mock.deregisteredCalls))
	}
	if mock.deregisteredCalls[0] != "ghost-agent" {
		t.Errorf("expected deregister for 'ghost-agent', got %q", mock.deregisteredCalls[0])
	}
}

// =============================================================================
// Controller Start/Stop Tests
// =============================================================================

func TestController_Start_RunsAndStopsOnCancel(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, _ := newTestController(t, mock)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := ctrl.Start(ctx)
	if err == nil {
		t.Fatal("expected context error after cancellation")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestController_Start_ReconcilesContinuously(t *testing.T) {
	callCount := 0
	mock := &mockBeadsClient{}
	ctrl, _ := newTestController(t, mock)

	// Override reconcile interval to be very fast
	ctrl.config.ReconcileInterval = 50 * time.Millisecond

	// Use a custom beads client that counts calls
	countingMock := &countingBeadsClient{calls: &callCount}
	ctrl.beads = countingMock

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ctrl.Start(ctx)

	// Should have run multiple reconciliations
	if callCount < 2 {
		t.Errorf("expected at least 2 reconciliation passes, got %d", callCount)
	}
}

type countingBeadsClient struct {
	mu    sync.Mutex
	calls *int
}

func (c *countingBeadsClient) ListSpawningAgents() ([]types.Issue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.calls++
	return nil, nil
}
func (c *countingBeadsClient) ListDoneAgents() ([]types.Issue, error) { return nil, nil }
func (c *countingBeadsClient) ListRegisteredPods() ([]rpc.AgentPodInfo, error) {
	return nil, nil
}
func (c *countingBeadsClient) RegisterPod(_, _, _, _ string) error { return nil }
func (c *countingBeadsClient) DeregisterPod(_ string) error        { return nil }
func (c *countingBeadsClient) UpdatePodStatus(_, _ string) error   { return nil }

// =============================================================================
// Pod Lifecycle Tests
// =============================================================================

func TestCreatePodForAgent_CorrectLabelsAndEnv(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, fakeClient := newTestController(t, mock)

	ctrl.config.PodTemplate = PodTemplateConfig{
		Image:        "test-image:v1",
		Namespace:    "test-ns",
		BDDaemonHost: "bd-daemon.svc",
		BDDaemonPort: "9090",
		APIKeySecret: "anthropic-secret",
	}

	err := ctrl.createPodForAgent(context.Background(), "test-agent", "polecat", "beads")
	if err != nil {
		t.Fatalf("createPodForAgent error: %v", err)
	}

	pod, err := fakeClient.CoreV1().Pods("test-ns").Get(context.Background(), "agent-test-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get created pod: %v", err)
	}

	// Check labels
	expectedLabels := map[string]string{
		LabelApp:       LabelAppValue,
		LabelRole:      "polecat",
		LabelRig:       "beads",
		LabelAgent:     "test-agent",
		LabelManagedBy: LabelManagedByValue,
	}
	for k, v := range expectedLabels {
		if pod.Labels[k] != v {
			t.Errorf("label %s: expected %q, got %q", k, v, pod.Labels[k])
		}
	}

	// Check container
	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(pod.Spec.Containers))
	}
	container := pod.Spec.Containers[0]
	if container.Image != "test-image:v1" {
		t.Errorf("expected image 'test-image:v1', got %q", container.Image)
	}

	// Check env vars
	envMap := make(map[string]corev1.EnvVar)
	for _, e := range container.Env {
		envMap[e.Name] = e
	}
	if envMap["GT_ROLE"].Value != "polecat" {
		t.Errorf("expected GT_ROLE=polecat, got %q", envMap["GT_ROLE"].Value)
	}
	if envMap["GT_RIG"].Value != "beads" {
		t.Errorf("expected GT_RIG=beads, got %q", envMap["GT_RIG"].Value)
	}
	if envMap["BD_DAEMON_HOST"].Value != "bd-daemon.svc" {
		t.Errorf("expected BD_DAEMON_HOST=bd-daemon.svc, got %q", envMap["BD_DAEMON_HOST"].Value)
	}
	if envMap["BD_DAEMON_PORT"].Value != "9090" {
		t.Errorf("expected BD_DAEMON_PORT=9090, got %q", envMap["BD_DAEMON_PORT"].Value)
	}
	apiKeyEnv := envMap["ANTHROPIC_API_KEY"]
	if apiKeyEnv.ValueFrom == nil || apiKeyEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatal("expected ANTHROPIC_API_KEY to come from secret")
	}
	if apiKeyEnv.ValueFrom.SecretKeyRef.Name != "anthropic-secret" {
		t.Errorf("expected secret name 'anthropic-secret', got %q", apiKeyEnv.ValueFrom.SecretKeyRef.Name)
	}

	// Check resources
	cpuReq := container.Resources.Requests[corev1.ResourceCPU]
	if cpuReq.String() != "500m" {
		t.Errorf("expected CPU request 500m, got %s", cpuReq.String())
	}
	memReq := container.Resources.Requests[corev1.ResourceMemory]
	if memReq.String() != "1Gi" {
		t.Errorf("expected memory request 1Gi, got %s", memReq.String())
	}

	// Check registration
	if len(mock.registeredCalls) != 1 {
		t.Fatalf("expected 1 register call, got %d", len(mock.registeredCalls))
	}
}

func TestCreatePodForAgent_SecurityContext(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, fakeClient := newTestController(t, mock)

	err := ctrl.createPodForAgent(context.Background(), "sec-agent", "polecat", "beads")
	if err != nil {
		t.Fatalf("createPodForAgent error: %v", err)
	}

	pod, err := fakeClient.CoreV1().Pods("test-ns").Get(context.Background(), "agent-sec-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get pod: %v", err)
	}

	// Pod security context
	if pod.Spec.SecurityContext == nil {
		t.Fatal("expected pod security context")
	}
	if pod.Spec.SecurityContext.RunAsNonRoot == nil || !*pod.Spec.SecurityContext.RunAsNonRoot {
		t.Error("expected RunAsNonRoot to be true")
	}

	// Container security context
	container := pod.Spec.Containers[0]
	if container.SecurityContext == nil {
		t.Fatal("expected container security context")
	}
	if container.SecurityContext.RunAsUser == nil || *container.SecurityContext.RunAsUser != 1000 {
		t.Error("expected RunAsUser 1000")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation != false {
		t.Error("expected AllowPrivilegeEscalation to be false")
	}
}

func TestCreatePodForAgent_Probes(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, fakeClient := newTestController(t, mock)

	err := ctrl.createPodForAgent(context.Background(), "probe-agent", "polecat", "beads")
	if err != nil {
		t.Fatalf("createPodForAgent error: %v", err)
	}

	pod, _ := fakeClient.CoreV1().Pods("test-ns").Get(context.Background(), "agent-probe-agent", metav1.GetOptions{})
	container := pod.Spec.Containers[0]

	// Startup probe
	if container.StartupProbe == nil {
		t.Fatal("expected startup probe")
	}
	if container.StartupProbe.TCPSocket == nil {
		t.Fatal("expected TCP startup probe")
	}
	if container.StartupProbe.TCPSocket.Port.IntValue() != DefaultScreenPort {
		t.Errorf("expected startup probe on port %d, got %d", DefaultScreenPort, container.StartupProbe.TCPSocket.Port.IntValue())
	}
	if container.StartupProbe.FailureThreshold != 30 {
		t.Errorf("expected startup probe failure threshold 30, got %d", container.StartupProbe.FailureThreshold)
	}

	// Liveness probe
	if container.LivenessProbe == nil {
		t.Fatal("expected liveness probe")
	}

	// Readiness probe
	if container.ReadinessProbe == nil {
		t.Fatal("expected readiness probe")
	}
}

func TestCreatePodForAgent_VolumeMounts(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, fakeClient := newTestController(t, mock)

	err := ctrl.createPodForAgent(context.Background(), "vol-agent", "polecat", "beads")
	if err != nil {
		t.Fatalf("createPodForAgent error: %v", err)
	}

	pod, _ := fakeClient.CoreV1().Pods("test-ns").Get(context.Background(), "agent-vol-agent", metav1.GetOptions{})
	container := pod.Spec.Containers[0]

	mountMap := make(map[string]string)
	for _, vm := range container.VolumeMounts {
		mountMap[vm.Name] = vm.MountPath
	}
	if mountMap["workspace"] != "/home/agent/gt" {
		t.Errorf("expected workspace mount at /home/agent/gt, got %q", mountMap["workspace"])
	}
	if mountMap["tmp"] != "/tmp" {
		t.Errorf("expected tmp mount at /tmp, got %q", mountMap["tmp"])
	}

	// Verify volumes exist
	volMap := make(map[string]bool)
	for _, v := range pod.Spec.Volumes {
		volMap[v.Name] = true
	}
	if !volMap["workspace"] {
		t.Error("expected workspace volume")
	}
	if !volMap["tmp"] {
		t.Error("expected tmp volume")
	}
}

func TestDeletePodForAgent_GracefulTermination(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, fakeClient := newTestController(t, mock)

	// Create pod first
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(),
		makeAgentPod("del-agent", "agent-del-agent", "test-ns", corev1.PodRunning),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to pre-create pod: %v", err)
	}

	err = ctrl.deletePodForAgent(context.Background(), "del-agent", "agent-del-agent")
	if err != nil {
		t.Fatalf("deletePodForAgent error: %v", err)
	}

	// Pod should be gone
	pods, err := fakeClient.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Errorf("expected 0 pods after deletion, got %d", len(pods.Items))
	}

	// Should have deregistered
	if len(mock.deregisteredCalls) != 1 {
		t.Fatalf("expected 1 deregister call, got %d", len(mock.deregisteredCalls))
	}
}

func TestDeletePodForAgent_AlreadyDeleted_Idempotent(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, _ := newTestController(t, mock)

	// Pod doesn't exist in K8s
	err := ctrl.deletePodForAgent(context.Background(), "gone-agent", "agent-gone-agent")
	if err != nil {
		t.Fatalf("deletePodForAgent should be idempotent for missing pods, got: %v", err)
	}

	// Should still deregister
	if len(mock.deregisteredCalls) != 1 {
		t.Fatalf("expected 1 deregister call even for missing pod, got %d", len(mock.deregisteredCalls))
	}
}

func TestCreatePodForAgent_DefaultImage(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, fakeClient := newTestController(t, mock)
	ctrl.config.PodTemplate.Image = "" // empty to trigger default

	err := ctrl.createPodForAgent(context.Background(), "def-agent", "polecat", "beads")
	if err != nil {
		t.Fatalf("createPodForAgent error: %v", err)
	}

	pod, _ := fakeClient.CoreV1().Pods("test-ns").Get(context.Background(), "agent-def-agent", metav1.GetOptions{})
	if pod.Spec.Containers[0].Image != DefaultImage {
		t.Errorf("expected default image %q, got %q", DefaultImage, pod.Spec.Containers[0].Image)
	}
}

func TestCreatePodForAgent_TerminationGracePeriod(t *testing.T) {
	mock := &mockBeadsClient{}
	ctrl, fakeClient := newTestController(t, mock)

	err := ctrl.createPodForAgent(context.Background(), "grace-agent", "polecat", "beads")
	if err != nil {
		t.Fatalf("createPodForAgent error: %v", err)
	}

	pod, _ := fakeClient.CoreV1().Pods("test-ns").Get(context.Background(), "agent-grace-agent", metav1.GetOptions{})
	if pod.Spec.TerminationGracePeriodSeconds == nil || *pod.Spec.TerminationGracePeriodSeconds != 30 {
		t.Error("expected 30 second termination grace period")
	}
}

// =============================================================================
// Health Check Tests
// =============================================================================

func TestCheckPodHealth_RunningAndHealthy(t *testing.T) {
	pod := makeAgentPod("h-agent", "agent-h-agent", "test-ns", corev1.PodRunning)
	info := &rpc.AgentPodInfo{AgentID: "h-agent", AgentState: "running"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthOK {
		t.Errorf("expected HealthOK, got %s", health)
	}
}

func TestCheckPodHealth_WorkingAgent(t *testing.T) {
	pod := makeAgentPod("w-agent", "agent-w-agent", "test-ns", corev1.PodRunning)
	info := &rpc.AgentPodInfo{AgentID: "w-agent", AgentState: "working"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthOK {
		t.Errorf("expected HealthOK for working agent, got %s", health)
	}
}

func TestCheckPodHealth_PodFailed(t *testing.T) {
	pod := makeAgentPod("f-agent", "agent-f-agent", "test-ns", corev1.PodFailed)
	info := &rpc.AgentPodInfo{AgentID: "f-agent", AgentState: "running"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthFailed {
		t.Errorf("expected HealthFailed, got %s", health)
	}
}

func TestCheckPodHealth_PodUnknown(t *testing.T) {
	pod := makeAgentPod("u-agent", "agent-u-agent", "test-ns", corev1.PodUnknown)
	info := &rpc.AgentPodInfo{AgentID: "u-agent", AgentState: "running"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthUnknown {
		t.Errorf("expected HealthUnknown, got %s", health)
	}
}

func TestCheckPodHealth_PodSucceeded(t *testing.T) {
	pod := makeAgentPod("s-agent", "agent-s-agent", "test-ns", corev1.PodSucceeded)
	info := &rpc.AgentPodInfo{AgentID: "s-agent"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthFailed {
		t.Errorf("expected HealthFailed for succeeded pod (should be cleaned up), got %s", health)
	}
}

func TestCheckPodHealth_CrashLoopBackOff(t *testing.T) {
	pod := makeAgentPod("cl-agent", "agent-cl-agent", "test-ns", corev1.PodRunning)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "agent",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff",
				},
			},
		},
	}
	info := &rpc.AgentPodInfo{AgentID: "cl-agent"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthCrashLoop {
		t.Errorf("expected HealthCrashLoop, got %s", health)
	}
}

func TestCheckPodHealth_NilAgentInfo(t *testing.T) {
	pod := makeAgentPod("nil-agent", "agent-nil-agent", "test-ns", corev1.PodRunning)

	health := CheckPodHealth(pod, nil, 15*time.Minute)
	if health != HealthOK {
		t.Errorf("expected HealthOK with nil agent info and running pod, got %s", health)
	}
}

func TestCheckPodHealth_DefaultStaleThreshold(t *testing.T) {
	pod := makeAgentPod("st-agent", "agent-st-agent", "test-ns", corev1.PodRunning)
	info := &rpc.AgentPodInfo{AgentID: "st-agent", AgentState: "running"}

	// Pass 0 to trigger default
	health := CheckPodHealth(pod, info, 0)
	if health != HealthOK {
		t.Errorf("expected HealthOK with default stale threshold, got %s", health)
	}
}

func TestCheckPodHealth_PendingPod(t *testing.T) {
	pod := makeAgentPod("p-agent", "agent-p-agent", "test-ns", corev1.PodPending)
	info := &rpc.AgentPodInfo{AgentID: "p-agent"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthOK {
		t.Errorf("expected HealthOK for pending pod (not failed), got %s", health)
	}
}

func TestCheckPodHealth_CrashLoopTakesPriority(t *testing.T) {
	// Pod phase is Running but container is CrashLoopBackOff
	pod := makeAgentPod("pri-agent", "agent-pri-agent", "test-ns", corev1.PodRunning)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "agent",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff",
				},
			},
		},
	}
	info := &rpc.AgentPodInfo{AgentID: "pri-agent", AgentState: "running"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	if health != HealthCrashLoop {
		t.Errorf("CrashLoop should take priority even with running agent state, got %s", health)
	}
}

func TestCheckPodHealth_FailedTakesPriorityOverCrashLoop(t *testing.T) {
	// Pod phase is Failed and container is CrashLoopBackOff
	pod := makeAgentPod("fp-agent", "agent-fp-agent", "test-ns", corev1.PodFailed)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "agent",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff",
				},
			},
		},
	}
	info := &rpc.AgentPodInfo{AgentID: "fp-agent"}

	health := CheckPodHealth(pod, info, 15*time.Minute)
	// Pod phase is checked first, so HealthFailed should win
	if health != HealthFailed {
		t.Errorf("HealthFailed should take priority over CrashLoop when pod phase is Failed, got %s", health)
	}
}

// =============================================================================
// Health Status Escalation in Reconciler Tests
// =============================================================================

func TestReconcile_CrashLoopPod_UpdatesStatusFailed(t *testing.T) {
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "crash-agent", PodName: "agent-crash-agent"},
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	// Create a pod with CrashLoopBackOff
	pod := makeAgentPod("crash-agent", "agent-crash-agent", "test-ns", corev1.PodRunning)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "agent",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff",
				},
			},
		},
	}
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create crash loop pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	if len(mock.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update for crash loop, got %d", len(mock.statusUpdates))
	}
	if mock.statusUpdates[0].agentID != "crash-agent" {
		t.Errorf("expected status update for 'crash-agent', got %q", mock.statusUpdates[0].agentID)
	}
	if mock.statusUpdates[0].status != "failed" {
		t.Errorf("expected status 'failed', got %q", mock.statusUpdates[0].status)
	}
}

func TestReconcile_FailedPod_UpdatesStatusFailed(t *testing.T) {
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "fail-agent", PodName: "agent-fail-agent"},
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	pod := makeAgentPod("fail-agent", "agent-fail-agent", "test-ns", corev1.PodFailed)
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create failed pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	if len(mock.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update, got %d", len(mock.statusUpdates))
	}
	if mock.statusUpdates[0].status != "failed" {
		t.Errorf("expected status 'failed', got %q", mock.statusUpdates[0].status)
	}
}

func TestReconcile_HealthyPod_NoStatusUpdate(t *testing.T) {
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "ok-agent", PodName: "agent-ok-agent", AgentState: "running"},
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	pod := makeAgentPod("ok-agent", "agent-ok-agent", "test-ns", corev1.PodRunning)
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	if len(mock.statusUpdates) != 0 {
		t.Errorf("expected no status updates for healthy pod, got %d", len(mock.statusUpdates))
	}
}

// =============================================================================
// K8s Helper Function Tests
// =============================================================================

func TestPodPhase(t *testing.T) {
	tests := []struct {
		name  string
		phase corev1.PodPhase
	}{
		{"Running", corev1.PodRunning},
		{"Pending", corev1.PodPending},
		{"Failed", corev1.PodFailed},
		{"Succeeded", corev1.PodSucceeded},
		{"Unknown", corev1.PodUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{Status: corev1.PodStatus{Phase: tt.phase}}
			if PodPhase(pod) != tt.phase {
				t.Errorf("PodPhase() = %v, want %v", PodPhase(pod), tt.phase)
			}
		})
	}
}

func TestIsPodCrashLooping(t *testing.T) {
	tests := []struct {
		name   string
		pod    *corev1.Pod
		expect bool
	}{
		{
			name: "not crash looping",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					},
				},
			},
			expect: false,
		},
		{
			name: "crash looping",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
						}},
					},
				},
			},
			expect: true,
		},
		{
			name: "waiting but not crash loop",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
						}},
					},
				},
			},
			expect: false,
		},
		{
			name:   "no container statuses",
			pod:    &corev1.Pod{},
			expect: false,
		},
		{
			name: "multiple containers one crash looping",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
						{State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
						}},
					},
				},
			},
			expect: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPodCrashLooping(tt.pod); got != tt.expect {
				t.Errorf("IsPodCrashLooping() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestPodIP(t *testing.T) {
	pod := &corev1.Pod{Status: corev1.PodStatus{PodIP: "10.1.2.3"}}
	if PodIP(pod) != "10.1.2.3" {
		t.Errorf("PodIP() = %q, want %q", PodIP(pod), "10.1.2.3")
	}
}

func TestPodNode(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{NodeName: "worker-1"}}
	if PodNode(pod) != "worker-1" {
		t.Errorf("PodNode() = %q, want %q", PodNode(pod), "worker-1")
	}
}

func TestAgentIDFromPod(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		expect string
	}{
		{"with agent label", map[string]string{LabelAgent: "agent-123"}, "agent-123"},
		{"no agent label", map[string]string{LabelApp: LabelAppValue}, ""},
		{"nil labels", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: tt.labels}}
			if got := AgentIDFromPod(pod); got != tt.expect {
				t.Errorf("AgentIDFromPod() = %q, want %q", got, tt.expect)
			}
		})
	}
}

// =============================================================================
// HealthStatus String Tests
// =============================================================================

func TestHealthStatus_String(t *testing.T) {
	tests := []struct {
		status HealthStatus
		expect string
	}{
		{HealthOK, "ok"},
		{HealthStale, "stale"},
		{HealthCrashLoop, "crash_loop"},
		{HealthFailed, "failed"},
		{HealthUnknown, "unknown"},
		{HealthStatus(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expect {
				t.Errorf("HealthStatus(%d).String() = %q, want %q", tt.status, got, tt.expect)
			}
		})
	}
}

// =============================================================================
// BuildPodSpec Tests
// =============================================================================

func TestBuildPodSpec_BasicStructure(t *testing.T) {
	cfg := PodTemplateConfig{
		Image:     "my-image:v2",
		Namespace: "production",
	}

	pod := BuildPodSpec("agent-xyz", "crew", "gastown", cfg)

	if pod.Name != "agent-agent-xyz" {
		t.Errorf("expected pod name 'agent-agent-xyz', got %q", pod.Name)
	}
	if pod.Namespace != "production" {
		t.Errorf("expected namespace 'production', got %q", pod.Namespace)
	}
	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(pod.Spec.Containers))
	}
	if pod.Spec.Containers[0].Name != "agent" {
		t.Errorf("expected container name 'agent', got %q", pod.Spec.Containers[0].Name)
	}
	if pod.Spec.Containers[0].ImagePullPolicy != corev1.PullAlways {
		t.Errorf("expected PullAlways, got %v", pod.Spec.Containers[0].ImagePullPolicy)
	}
}

func TestBuildPodSpec_DefaultImage(t *testing.T) {
	cfg := PodTemplateConfig{Namespace: "test"}
	pod := BuildPodSpec("agent-1", "polecat", "beads", cfg)

	if pod.Spec.Containers[0].Image != DefaultImage {
		t.Errorf("expected default image, got %q", pod.Spec.Containers[0].Image)
	}
}

func TestBuildPodSpec_ScreenPort(t *testing.T) {
	cfg := PodTemplateConfig{Namespace: "test"}
	pod := BuildPodSpec("agent-1", "polecat", "beads", cfg)

	container := pod.Spec.Containers[0]
	if len(container.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(container.Ports))
	}
	if container.Ports[0].ContainerPort != DefaultScreenPort {
		t.Errorf("expected screen port %d, got %d", DefaultScreenPort, container.Ports[0].ContainerPort)
	}
	if container.Ports[0].Name != "screen" {
		t.Errorf("expected port name 'screen', got %q", container.Ports[0].Name)
	}
}

func TestBuildPodSpec_NoDaemonEnvWhenEmpty(t *testing.T) {
	cfg := PodTemplateConfig{Namespace: "test"}
	pod := BuildPodSpec("agent-1", "polecat", "beads", cfg)

	container := pod.Spec.Containers[0]
	for _, e := range container.Env {
		if e.Name == "BD_DAEMON_HOST" || e.Name == "BD_DAEMON_PORT" {
			t.Errorf("should not include %s when config is empty", e.Name)
		}
	}
}

func TestBuildPodSpec_NoSecretWhenEmpty(t *testing.T) {
	cfg := PodTemplateConfig{Namespace: "test"}
	pod := BuildPodSpec("agent-1", "polecat", "beads", cfg)

	container := pod.Spec.Containers[0]
	for _, e := range container.Env {
		if e.Name == "ANTHROPIC_API_KEY" {
			t.Error("should not include ANTHROPIC_API_KEY when APIKeySecret is empty")
		}
	}
}

// =============================================================================
// Controller New() Tests
// =============================================================================

func TestNew_DefaultConfig(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	k8s := NewK8sClientFromClientset(fakeClient, "ns")
	mock := &mockBeadsClient{}

	ctrl := New(k8s, mock, Config{}, nil)

	if ctrl.config.ReconcileInterval != DefaultReconcileInterval {
		t.Errorf("expected default reconcile interval %v, got %v", DefaultReconcileInterval, ctrl.config.ReconcileInterval)
	}
	if ctrl.config.StaleThreshold != DefaultStaleThreshold {
		t.Errorf("expected default stale threshold %v, got %v", DefaultStaleThreshold, ctrl.config.StaleThreshold)
	}
	if ctrl.logger == nil {
		t.Error("expected default logger when nil is passed")
	}
}

func TestNew_CustomConfig(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	k8s := NewK8sClientFromClientset(fakeClient, "ns")
	mock := &mockBeadsClient{}
	logger := log.New(os.Stderr, "custom", 0)

	ctrl := New(k8s, mock, Config{
		ReconcileInterval: 30 * time.Second,
		StaleThreshold:    5 * time.Minute,
	}, logger)

	if ctrl.config.ReconcileInterval != 30*time.Second {
		t.Errorf("expected 30s reconcile interval, got %v", ctrl.config.ReconcileInterval)
	}
	if ctrl.config.StaleThreshold != 5*time.Minute {
		t.Errorf("expected 5m stale threshold, got %v", ctrl.config.StaleThreshold)
	}
	if ctrl.logger != logger {
		t.Error("expected custom logger")
	}
}

// =============================================================================
// Concurrent Reconciliation Tests
// =============================================================================

func TestReconcile_ConcurrentSafety(t *testing.T) {
	mock := &mockBeadsClient{
		spawningAgents: []types.Issue{
			makeSpawningAgent("conc-agent", "polecat", "beads"),
		},
	}
	ctrl, _ := newTestController(t, mock)

	// Run multiple reconciliations concurrently
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ctrl.reconcileOnce(context.Background()); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent reconciliation error: %v", err)
	}
}

// =============================================================================
// K8s Client Method Tests
// =============================================================================

func TestK8sClient_GetPod(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	k8s := NewK8sClientFromClientset(fakeClient, "test-ns")

	// Create a pod
	pod := makeAgentPod("get-agent", "agent-get-agent", "test-ns", corev1.PodRunning)
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	// Get the pod
	got, err := k8s.GetPod(context.Background(), "agent-get-agent")
	if err != nil {
		t.Fatalf("GetPod error: %v", err)
	}
	if got.Name != "agent-get-agent" {
		t.Errorf("expected pod name 'agent-get-agent', got %q", got.Name)
	}
}

func TestK8sClient_GetPod_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	k8s := NewK8sClientFromClientset(fakeClient, "test-ns")

	_, err := k8s.GetPod(context.Background(), "nonexistent-pod")
	if err == nil {
		t.Fatal("expected error for nonexistent pod")
	}
	if !strings.Contains(err.Error(), "failed to get pod") {
		t.Errorf("error should mention 'failed to get pod', got: %v", err)
	}
}

func TestK8sClient_ListAgentPods_Error(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	fakeClient.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("K8s API unreachable")
	})
	k8s := NewK8sClientFromClientset(fakeClient, "test-ns")

	_, err := k8s.ListAgentPods(context.Background())
	if err == nil {
		t.Fatal("expected error when K8s API is unreachable")
	}
	if !strings.Contains(err.Error(), "failed to list agent pods") {
		t.Errorf("error should mention 'failed to list agent pods', got: %v", err)
	}
}

func TestK8sClient_CreatePod_AlreadyExists(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	k8s := NewK8sClientFromClientset(fakeClient, "test-ns")

	pod := makeAgentPod("dup-agent", "agent-dup-agent", "test-ns", corev1.PodRunning)
	_, err := k8s.CreatePod(context.Background(), pod)
	if err != nil {
		t.Fatalf("first create should succeed: %v", err)
	}

	_, err = k8s.CreatePod(context.Background(), pod)
	if err == nil {
		t.Fatal("expected error when creating duplicate pod")
	}
	if !strings.Contains(err.Error(), "failed to create pod") {
		t.Errorf("error should mention 'failed to create pod', got: %v", err)
	}
}

// =============================================================================
// Reconciliation K8s Error Tests
// =============================================================================

func TestReconcile_K8sListPodsError(t *testing.T) {
	mock := &mockBeadsClient{}
	fakeClient := fake.NewSimpleClientset()
	fakeClient.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("K8s API unreachable")
	})
	k8sClient := NewK8sClientFromClientset(fakeClient, "test-ns")
	logger := log.New(os.Stderr, "[test] ", log.LstdFlags)
	ctrl := New(k8sClient, mock, Config{
		ReconcileInterval: 100 * time.Millisecond,
		StaleThreshold:    15 * time.Minute,
	}, logger)

	err := ctrl.reconcileOnce(context.Background())
	if err == nil {
		t.Fatal("expected error when K8s list fails")
	}
	if !strings.Contains(err.Error(), "K8s pods") {
		t.Errorf("error should mention K8s pods, got: %v", err)
	}
}

func TestReconcile_DeregisterError_ContinuesReconciliation(t *testing.T) {
	mock := &mockBeadsClient{
		doneAgents: []types.Issue{
			makeDoneAgent("dereg-fail", types.StateDone, "agent-dereg-fail"),
		},
		deregisterPodErr: fmt.Errorf("deregister service unavailable"),
	}
	ctrl, fakeClient := newTestController(t, mock)

	// Create the pod so it can be deleted
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(),
		makeAgentPod("dereg-fail", "agent-dereg-fail", "test-ns", corev1.PodRunning),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to pre-create pod: %v", err)
	}

	// Reconciliation should not fail even though deregister errors
	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce should not fail on deregister error, got: %v", err)
	}
}

func TestReconcile_UpdateStatusError_ContinuesReconciliation(t *testing.T) {
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "status-fail", PodName: "agent-status-fail"},
		},
		updateStatusErr: fmt.Errorf("status update failed"),
	}
	ctrl, fakeClient := newTestController(t, mock)

	// Create a crashed pod
	pod := makeAgentPod("status-fail", "agent-status-fail", "test-ns", corev1.PodRunning)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "agent",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff",
				},
			},
		},
	}
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	// Reconciliation should not fail even though status update errors
	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce should not fail on status update error, got: %v", err)
	}
}

func TestReconcile_OrphanedDoneAgent_DeregisterError(t *testing.T) {
	// Done agent with no pod in K8s, registered in beads, deregister fails
	mock := &mockBeadsClient{
		doneAgents: []types.Issue{
			makeDoneAgent("orphan-fail", types.StateDone, "agent-orphan-fail"),
		},
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "orphan-fail", PodName: "agent-orphan-fail"},
		},
		deregisterPodErr: fmt.Errorf("deregister service unavailable"),
	}
	ctrl, _ := newTestController(t, mock)

	// Should not fail even though deregister errors
	err := ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce should continue on deregister error, got: %v", err)
	}
}

func TestReconcile_HealthStale_LogsWarning(t *testing.T) {
	// Agent with idle state (not running/working) - tests HealthOK default path
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "idle-agent", PodName: "agent-idle-agent", AgentState: "idle"},
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	pod := makeAgentPod("idle-agent", "agent-idle-agent", "test-ns", corev1.PodRunning)
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// No status updates expected for OK pods
	if len(mock.statusUpdates) != 0 {
		t.Errorf("expected no status updates for healthy idle agent, got %d", len(mock.statusUpdates))
	}
}

func TestReconcile_UnknownPodPhase_UpdatesNothing(t *testing.T) {
	mock := &mockBeadsClient{
		registeredPods: []rpc.AgentPodInfo{
			{AgentID: "unk-agent", PodName: "agent-unk-agent"},
		},
	}
	ctrl, fakeClient := newTestController(t, mock)

	pod := makeAgentPod("unk-agent", "agent-unk-agent", "test-ns", corev1.PodUnknown)
	_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	err = ctrl.reconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcileOnce error: %v", err)
	}

	// Unknown phase doesn't trigger status update (it's logged as warning)
	if len(mock.statusUpdates) != 0 {
		t.Errorf("expected no status updates for unknown phase, got %d", len(mock.statusUpdates))
	}
}

// =============================================================================
// BeadsClient Tests (using in-memory RPC server)
// =============================================================================

// setupTestRPCServer creates a SQLite-backed RPC server with TCP support for testing BeadsClient.
func setupTestRPCServer(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "controller-beads-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	socketPath := filepath.Join(tmpDir, "bd.sock")
	dbPath := filepath.Join(tmpDir, "beads.db")

	ctx := context.Background()
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create SQLite store: %v", err)
	}

	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to set config: %v", err)
	}

	server := rpc.NewServer(socketPath, store, tmpDir, dbPath)
	server.SetTCPAddr("127.0.0.1:0")

	serverCtx, cancel := context.WithCancel(ctx)
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(serverCtx)
	}()

	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		cancel()
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatal("timeout waiting for server to start")
	}

	tcpAddr := server.TCPListener().Addr().String()

	cleanup := func() {
		cancel()
		server.Stop()
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return tcpAddr, cleanup
}

// seedAgents creates agent issues in the test server via RPC.
func seedAgents(t *testing.T, addr string, agents []types.Issue) {
	t.Helper()
	for _, agent := range agents {
		client, err := rpc.TryConnectTCPWithTimeout(addr, "", 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect for seeding: %v", err)
		}

		// Create the agent issue
		_, err = client.Create(&rpc.CreateArgs{
			ID:        agent.ID,
			Title:     "Test agent " + agent.ID,
			IssueType: "task",
			Labels:    []string{"gt:agent"},
		})
		if err != nil {
			client.Close()
			t.Fatalf("failed to seed agent %s: %v", agent.ID, err)
		}

		// Update with agent state
		agentState := string(agent.AgentState)
		_, err = client.Update(&rpc.UpdateArgs{
			ID:         agent.ID,
			AgentState: &agentState,
		})
		client.Close()
		if err != nil {
			t.Fatalf("failed to set agent state for %s: %v", agent.ID, err)
		}

		// If the agent has pod info, register it
		if agent.PodName != "" {
			client2, err := rpc.TryConnectTCPWithTimeout(addr, "", 2*time.Second)
			if err != nil {
				t.Fatalf("failed to connect for pod registration: %v", err)
			}
			_, err = client2.AgentPodRegister(&rpc.AgentPodRegisterArgs{
				AgentID:   agent.ID,
				PodName:   agent.PodName,
				PodIP:     "10.0.0.1",
				PodNode:   "test-node",
				PodStatus: "running",
			})
			client2.Close()
			if err != nil {
				t.Fatalf("failed to register pod for agent %s: %v", agent.ID, err)
			}
		}
	}
}

func TestBeadsClient_NewBeadsClient(t *testing.T) {
	client := NewBeadsClient("localhost:9876", "test-token")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.daemonAddr != "localhost:9876" {
		t.Errorf("expected addr 'localhost:9876', got %q", client.daemonAddr)
	}
	if client.token != "test-token" {
		t.Errorf("expected token 'test-token', got %q", client.token)
	}
}

func TestBeadsClient_ConnectionError(t *testing.T) {
	// Point to a non-existent server
	client := NewBeadsClient("127.0.0.1:1", "")

	_, err := client.ListSpawningAgents()
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("error should mention connection failure, got: %v", err)
	}

	_, err = client.ListDoneAgents()
	if err == nil {
		t.Fatal("expected connection error for ListDoneAgents")
	}

	_, err = client.ListRegisteredPods()
	if err == nil {
		t.Fatal("expected connection error for ListRegisteredPods")
	}

	err = client.RegisterPod("agent-1", "pod-1", "10.0.0.1", "node-1")
	if err == nil {
		t.Fatal("expected connection error for RegisterPod")
	}

	err = client.DeregisterPod("agent-1")
	if err == nil {
		t.Fatal("expected connection error for DeregisterPod")
	}

	err = client.UpdatePodStatus("agent-1", "failed")
	if err == nil {
		t.Fatal("expected connection error for UpdatePodStatus")
	}
}

func TestBeadsClient_ListSpawningAgents(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	seedAgents(t, addr, []types.Issue{
		{ID: "test-sp1", AgentState: types.StateSpawning},
		{ID: "test-sp2", AgentState: types.StateSpawning},
		{ID: "test-run1", AgentState: types.StateRunning},
		{ID: "test-dn1", AgentState: types.StateDone},
	})

	client := NewBeadsClient(addr, "")
	agents, err := client.ListSpawningAgents()
	if err != nil {
		t.Fatalf("ListSpawningAgents error: %v", err)
	}

	if len(agents) != 2 {
		t.Fatalf("expected 2 spawning agents, got %d", len(agents))
	}

	ids := make(map[string]bool)
	for _, a := range agents {
		ids[a.ID] = true
	}
	if !ids["test-sp1"] || !ids["test-sp2"] {
		t.Errorf("expected test-sp1 and test-sp2 in results, got %v", ids)
	}
}

func TestBeadsClient_ListDoneAgents(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	seedAgents(t, addr, []types.Issue{
		{ID: "test-done1", AgentState: types.StateDone, PodName: "agent-test-done1"},
		{ID: "test-stop1", AgentState: types.StateStopped, PodName: "agent-test-stop1"},
		{ID: "test-dead1", AgentState: types.StateDead, PodName: "agent-test-dead1"},
		{ID: "test-donenp", AgentState: types.StateDone}, // no pod - should not be returned
		{ID: "test-runx", AgentState: types.StateRunning},
	})

	client := NewBeadsClient(addr, "")
	agents, err := client.ListDoneAgents()
	if err != nil {
		t.Fatalf("ListDoneAgents error: %v", err)
	}

	if len(agents) != 3 {
		t.Fatalf("expected 3 done agents (with pods), got %d", len(agents))
	}

	ids := make(map[string]bool)
	for _, a := range agents {
		ids[a.ID] = true
	}
	if !ids["test-done1"] || !ids["test-stop1"] || !ids["test-dead1"] {
		t.Errorf("expected test-done1, test-stop1, test-dead1 in results, got %v", ids)
	}
}

func TestBeadsClient_ListRegisteredPods(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	seedAgents(t, addr, []types.Issue{
		{ID: "test-reg1", AgentState: types.StateRunning, PodName: "agent-test-reg1"},
		{ID: "test-reg2", AgentState: types.StateWorking, PodName: "agent-test-reg2"},
		{ID: "test-noreg", AgentState: types.StateRunning}, // no pod registered
	})

	client := NewBeadsClient(addr, "")
	pods, err := client.ListRegisteredPods()
	if err != nil {
		t.Fatalf("ListRegisteredPods error: %v", err)
	}

	if len(pods) != 2 {
		t.Fatalf("expected 2 registered pods, got %d", len(pods))
	}

	for _, pod := range pods {
		if pod.PodName == "" {
			t.Error("expected pod name to be set")
		}
		if pod.AgentID == "" {
			t.Error("expected agent ID to be set")
		}
	}
}

func TestBeadsClient_RegisterPod(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	seedAgents(t, addr, []types.Issue{
		{ID: "test-regag", AgentState: types.StateSpawning},
	})

	client := NewBeadsClient(addr, "")
	err := client.RegisterPod("test-regag", "agent-test-regag", "10.0.0.5", "node-2")
	if err != nil {
		t.Fatalf("RegisterPod error: %v", err)
	}

	pods, err := client.ListRegisteredPods()
	if err != nil {
		t.Fatalf("ListRegisteredPods error: %v", err)
	}

	found := false
	for _, pod := range pods {
		if pod.AgentID == "test-regag" && pod.PodName == "agent-test-regag" {
			found = true
			break
		}
	}
	if !found {
		t.Error("registered pod not found in listing")
	}
}

func TestBeadsClient_DeregisterPod(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	seedAgents(t, addr, []types.Issue{
		{ID: "test-derag", AgentState: types.StateRunning, PodName: "agent-test-derag"},
	})

	client := NewBeadsClient(addr, "")

	pods, err := client.ListRegisteredPods()
	if err != nil {
		t.Fatalf("ListRegisteredPods error: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 registered pod before deregister, got %d", len(pods))
	}

	err = client.DeregisterPod("test-derag")
	if err != nil {
		t.Fatalf("DeregisterPod error: %v", err)
	}

	pods, err = client.ListRegisteredPods()
	if err != nil {
		t.Fatalf("ListRegisteredPods error: %v", err)
	}
	if len(pods) != 0 {
		t.Errorf("expected 0 registered pods after deregister, got %d", len(pods))
	}
}

func TestBeadsClient_UpdatePodStatus(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	seedAgents(t, addr, []types.Issue{
		{ID: "test-stag", AgentState: types.StateRunning, PodName: "agent-test-stag"},
	})

	client := NewBeadsClient(addr, "")

	err := client.UpdatePodStatus("test-stag", "failed")
	if err != nil {
		t.Fatalf("UpdatePodStatus error: %v", err)
	}

	pods, err := client.ListRegisteredPods()
	if err != nil {
		t.Fatalf("ListRegisteredPods error: %v", err)
	}
	for _, pod := range pods {
		if pod.AgentID == "test-stag" && pod.PodStatus != "failed" {
			t.Errorf("expected pod status 'failed', got %q", pod.PodStatus)
		}
	}
}

func TestBeadsClient_ListSpawningAgents_EmptyResult(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	client := NewBeadsClient(addr, "")
	agents, err := client.ListSpawningAgents()
	if err != nil {
		t.Fatalf("ListSpawningAgents error: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 spawning agents, got %d", len(agents))
	}
}

func TestBeadsClient_ListDoneAgents_EmptyResult(t *testing.T) {
	addr, cleanup := setupTestRPCServer(t)
	defer cleanup()

	client := NewBeadsClient(addr, "")
	agents, err := client.ListDoneAgents()
	if err != nil {
		t.Fatalf("ListDoneAgents error: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 done agents, got %d", len(agents))
	}
}
