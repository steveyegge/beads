package rpc

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// createTestAgent creates an agent bead in the test server and adds the gt:agent label.
// Returns the agent's ID.
func createTestAgent(t *testing.T, client *Client, name string) string {
	t.Helper()

	createArgs := &CreateArgs{
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

	// Add gt:agent label (required for pod-list filtering)
	labelResp, err := client.Execute(OpLabelAdd, &LabelAddArgs{
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

func TestAgentPodRegister_AllFields(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-emma")

	result, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "emma-pod-abc",
		PodIP:         "10.0.1.5",
		PodNode:       "node-1",
		PodStatus:     "running",
		ScreenSession: "emma-screen",
	})
	if err != nil {
		t.Fatalf("AgentPodRegister failed: %v", err)
	}

	if result.AgentID != agentID {
		t.Errorf("result.AgentID = %q, want %q", result.AgentID, agentID)
	}
	if result.PodName != "emma-pod-abc" {
		t.Errorf("result.PodName = %q, want %q", result.PodName, "emma-pod-abc")
	}
	if result.PodStatus != "running" {
		t.Errorf("result.PodStatus = %q, want %q", result.PodStatus, "running")
	}

	// Verify persistence by fetching the issue
	getResp, err := client.Execute(OpShow, &ShowArgs{ID: agentID})
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	var issue types.Issue
	if err := json.Unmarshal(getResp.Data, &issue); err != nil {
		t.Fatalf("failed to unmarshal agent: %v", err)
	}

	if issue.PodName != "emma-pod-abc" {
		t.Errorf("persisted PodName = %q, want %q", issue.PodName, "emma-pod-abc")
	}
	if issue.PodIP != "10.0.1.5" {
		t.Errorf("persisted PodIP = %q, want %q", issue.PodIP, "10.0.1.5")
	}
	if issue.PodNode != "node-1" {
		t.Errorf("persisted PodNode = %q, want %q", issue.PodNode, "node-1")
	}
	if issue.PodStatus != "running" {
		t.Errorf("persisted PodStatus = %q, want %q", issue.PodStatus, "running")
	}
	if issue.ScreenSession != "emma-screen" {
		t.Errorf("persisted ScreenSession = %q, want %q", issue.ScreenSession, "emma-screen")
	}
}

func TestAgentPodRegister_DefaultStatus(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-nux")

	// Register without specifying pod_status - should default to "running"
	result, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID: agentID,
		PodName: "nux-pod-xyz",
	})
	if err != nil {
		t.Fatalf("AgentPodRegister failed: %v", err)
	}
	if result.PodStatus != "running" {
		t.Errorf("default PodStatus = %q, want %q", result.PodStatus, "running")
	}

	// Verify persisted
	getResp, err := client.Execute(OpShow, &ShowArgs{ID: agentID})
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	var issue types.Issue
	json.Unmarshal(getResp.Data, &issue)

	if issue.PodStatus != "running" {
		t.Errorf("persisted PodStatus = %q, want %q", issue.PodStatus, "running")
	}
}

func TestAgentPodRegister_PartialUpdate(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-opal")

	// First register with all fields
	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "opal-pod-v1",
		PodIP:         "10.0.1.10",
		PodNode:       "node-2",
		PodStatus:     "running",
		ScreenSession: "opal-screen",
	})
	if err != nil {
		t.Fatalf("first AgentPodRegister failed: %v", err)
	}

	// Re-register with updated pod_status only (new pod name signals new pod)
	_, err = client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:   agentID,
		PodName:   "opal-pod-v2",
		PodStatus: "pending",
	})
	if err != nil {
		t.Fatalf("second AgentPodRegister failed: %v", err)
	}

	// Verify new values
	getResp, err := client.Execute(OpShow, &ShowArgs{ID: agentID})
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	var issue types.Issue
	json.Unmarshal(getResp.Data, &issue)

	if issue.PodName != "opal-pod-v2" {
		t.Errorf("PodName = %q, want %q", issue.PodName, "opal-pod-v2")
	}
	if issue.PodStatus != "pending" {
		t.Errorf("PodStatus = %q, want %q", issue.PodStatus, "pending")
	}
	// PodIP, PodNode, ScreenSession should be overwritten to empty (new registration)
	if issue.PodIP != "" {
		t.Errorf("PodIP = %q, want empty (overwritten by re-register)", issue.PodIP)
	}
}

func TestAgentPodRegister_MissingAgentID(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		PodName: "some-pod",
	})
	if err == nil {
		t.Error("expected error when agent_id is missing")
	}
}

func TestAgentPodRegister_MissingPodName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-test")

	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID: agentID,
	})
	if err == nil {
		t.Error("expected error when pod_name is missing")
	}
}

func TestAgentPodDeregister(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-ruby")

	// Register pod first
	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "ruby-pod-abc",
		PodIP:         "10.0.1.20",
		PodNode:       "node-3",
		PodStatus:     "running",
		ScreenSession: "ruby-screen",
	})
	if err != nil {
		t.Fatalf("AgentPodRegister failed: %v", err)
	}

	// Deregister
	result, err := client.AgentPodDeregister(&AgentPodDeregisterArgs{
		AgentID: agentID,
	})
	if err != nil {
		t.Fatalf("AgentPodDeregister failed: %v", err)
	}
	if result.AgentID != agentID {
		t.Errorf("result.AgentID = %q, want %q", result.AgentID, agentID)
	}

	// Verify all pod fields are cleared
	getResp, err := client.Execute(OpShow, &ShowArgs{ID: agentID})
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	var issue types.Issue
	json.Unmarshal(getResp.Data, &issue)

	if issue.PodName != "" {
		t.Errorf("PodName = %q, want empty", issue.PodName)
	}
	if issue.PodIP != "" {
		t.Errorf("PodIP = %q, want empty", issue.PodIP)
	}
	if issue.PodNode != "" {
		t.Errorf("PodNode = %q, want empty", issue.PodNode)
	}
	if issue.PodStatus != "" {
		t.Errorf("PodStatus = %q, want empty", issue.PodStatus)
	}
	if issue.ScreenSession != "" {
		t.Errorf("ScreenSession = %q, want empty", issue.ScreenSession)
	}
}

func TestAgentPodDeregister_AlreadyEmpty(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-jade")

	// Deregister without ever registering - should succeed (idempotent)
	result, err := client.AgentPodDeregister(&AgentPodDeregisterArgs{
		AgentID: agentID,
	})
	if err != nil {
		t.Fatalf("AgentPodDeregister on empty agent failed: %v", err)
	}
	if result.AgentID != agentID {
		t.Errorf("result.AgentID = %q, want %q", result.AgentID, agentID)
	}
}

func TestAgentPodDeregister_MissingAgentID(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.AgentPodDeregister(&AgentPodDeregisterArgs{})
	if err == nil {
		t.Error("expected error when agent_id is missing")
	}
}

func TestAgentPodStatus(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-pearl")

	// Register pod first
	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:   agentID,
		PodName:   "pearl-pod-abc",
		PodStatus: "running",
	})
	if err != nil {
		t.Fatalf("AgentPodRegister failed: %v", err)
	}

	// Update status
	result, err := client.AgentPodStatus(&AgentPodStatusArgs{
		AgentID:   agentID,
		PodStatus: "terminating",
	})
	if err != nil {
		t.Fatalf("AgentPodStatus failed: %v", err)
	}
	if result.AgentID != agentID {
		t.Errorf("result.AgentID = %q, want %q", result.AgentID, agentID)
	}
	if result.PodStatus != "terminating" {
		t.Errorf("result.PodStatus = %q, want %q", result.PodStatus, "terminating")
	}

	// Verify only pod_status changed, pod_name preserved
	getResp, err := client.Execute(OpShow, &ShowArgs{ID: agentID})
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	var issue types.Issue
	json.Unmarshal(getResp.Data, &issue)

	if issue.PodStatus != "terminating" {
		t.Errorf("persisted PodStatus = %q, want %q", issue.PodStatus, "terminating")
	}
	if issue.PodName != "pearl-pod-abc" {
		t.Errorf("PodName changed unexpectedly: got %q, want %q", issue.PodName, "pearl-pod-abc")
	}
}

func TestAgentPodStatus_MissingFields(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-test2")

	tests := []struct {
		name string
		args *AgentPodStatusArgs
	}{
		{
			name: "missing agent_id",
			args: &AgentPodStatusArgs{PodStatus: "running"},
		},
		{
			name: "missing pod_status",
			args: &AgentPodStatusArgs{AgentID: agentID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.AgentPodStatus(tt.args)
			if err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}

func TestAgentPodList_WithPods(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create agents - some with pods, some without
	agent1 := createTestAgent(t, client, "gt-beads-polecat-alpha")
	agent2 := createTestAgent(t, client, "gt-beads-polecat-beta")
	_ = createTestAgent(t, client, "gt-beads-polecat-gamma") // no pod

	// Register pods for agent1 and agent2
	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:   agent1,
		PodName:   "alpha-pod",
		PodIP:     "10.0.1.1",
		PodNode:   "node-1",
		PodStatus: "running",
	})
	if err != nil {
		t.Fatalf("register agent1 pod failed: %v", err)
	}

	_, err = client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:   agent2,
		PodName:   "beta-pod",
		PodIP:     "10.0.1.2",
		PodStatus: "pending",
	})
	if err != nil {
		t.Fatalf("register agent2 pod failed: %v", err)
	}

	// List all agents with pods
	result, err := client.AgentPodList(&AgentPodListArgs{})
	if err != nil {
		t.Fatalf("AgentPodList failed: %v", err)
	}

	if len(result.Agents) != 2 {
		t.Fatalf("expected 2 agents with pods, got %d", len(result.Agents))
	}

	// Verify fields are populated
	found := map[string]bool{}
	for _, a := range result.Agents {
		found[a.PodName] = true
		if a.AgentID == "" {
			t.Error("agent has empty AgentID")
		}
		if a.PodStatus == "" {
			t.Error("agent has empty PodStatus")
		}
	}
	if !found["alpha-pod"] {
		t.Error("alpha-pod not found in results")
	}
	if !found["beta-pod"] {
		t.Error("beta-pod not found in results")
	}
}

func TestAgentPodList_FilterByRig(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agent1 := createTestAgent(t, client, "gt-beads-polecat-a1")
	agent2 := createTestAgent(t, client, "gt-gastown-polecat-a2")

	// Set rig field on agents
	beadsRig := "beads"
	_, err := client.Execute(OpUpdate, &UpdateArgs{
		ID:  agent1,
		Rig: &beadsRig,
	})
	if err != nil {
		t.Fatalf("failed to set rig on agent1: %v", err)
	}
	gastownRig := "gastown"
	_, err = client.Execute(OpUpdate, &UpdateArgs{
		ID:  agent2,
		Rig: &gastownRig,
	})
	if err != nil {
		t.Fatalf("failed to set rig on agent2: %v", err)
	}

	// Register pods for both
	_, err = client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID: agent1, PodName: "beads-pod",
	})
	if err != nil {
		t.Fatalf("register pod failed: %v", err)
	}
	_, err = client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID: agent2, PodName: "gastown-pod",
	})
	if err != nil {
		t.Fatalf("register pod failed: %v", err)
	}

	// List filtered by rig
	result, err := client.AgentPodList(&AgentPodListArgs{Rig: "beads"})
	if err != nil {
		t.Fatalf("AgentPodList with rig filter failed: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Fatalf("expected 1 agent in rig beads, got %d", len(result.Agents))
	}
	if result.Agents[0].PodName != "beads-pod" {
		t.Errorf("expected beads-pod, got %s", result.Agents[0].PodName)
	}
	if result.Agents[0].Rig != "beads" {
		t.Errorf("expected rig beads, got %s", result.Agents[0].Rig)
	}
}

func TestAgentPodList_Empty(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create agent without pod
	createTestAgent(t, client, "gt-beads-polecat-lonely")

	result, err := client.AgentPodList(&AgentPodListArgs{})
	if err != nil {
		t.Fatalf("AgentPodList failed: %v", err)
	}
	if len(result.Agents) != 0 {
		t.Errorf("expected 0 agents with pods, got %d", len(result.Agents))
	}
}

func TestAgentPodList_NilArgs(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Should work with empty args (no filtering)
	result, err := client.AgentPodList(&AgentPodListArgs{})
	if err != nil {
		t.Fatalf("AgentPodList with empty args failed: %v", err)
	}
	// No agents created, so list should be empty (no crash)
	if len(result.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(result.Agents))
	}
}

func TestAgentPodRoundTrip_RegisterListVerify(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-roundtrip")

	// Set rig and role_type for filter testing
	rig := "beads"
	roleType := "polecat"
	_, err := client.Execute(OpUpdate, &UpdateArgs{
		ID:       agentID,
		Rig:      &rig,
		RoleType: &roleType,
	})
	if err != nil {
		t.Fatalf("failed to set agent fields: %v", err)
	}

	// Register
	regArgs := &AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "roundtrip-pod",
		PodIP:         "10.0.2.1",
		PodNode:       "node-5",
		PodStatus:     "running",
		ScreenSession: "roundtrip-screen",
	}
	_, err = client.AgentPodRegister(regArgs)
	if err != nil {
		t.Fatalf("AgentPodRegister failed: %v", err)
	}

	// List and verify fields match
	listResult, err := client.AgentPodList(&AgentPodListArgs{})
	if err != nil {
		t.Fatalf("AgentPodList failed: %v", err)
	}
	if len(listResult.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(listResult.Agents))
	}

	a := listResult.Agents[0]
	if a.AgentID != agentID {
		t.Errorf("AgentID = %q, want %q", a.AgentID, agentID)
	}
	if a.PodName != "roundtrip-pod" {
		t.Errorf("PodName = %q, want %q", a.PodName, "roundtrip-pod")
	}
	if a.PodIP != "10.0.2.1" {
		t.Errorf("PodIP = %q, want %q", a.PodIP, "10.0.2.1")
	}
	if a.PodNode != "node-5" {
		t.Errorf("PodNode = %q, want %q", a.PodNode, "node-5")
	}
	if a.PodStatus != "running" {
		t.Errorf("PodStatus = %q, want %q", a.PodStatus, "running")
	}
	if a.ScreenSession != "roundtrip-screen" {
		t.Errorf("ScreenSession = %q, want %q", a.ScreenSession, "roundtrip-screen")
	}
	if a.Rig != "beads" {
		t.Errorf("Rig = %q, want %q", a.Rig, "beads")
	}
	if a.RoleType != "polecat" {
		t.Errorf("RoleType = %q, want %q", a.RoleType, "polecat")
	}
}

func TestAgentPodRoundTrip_RegisterDeregisterList(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	agentID := createTestAgent(t, client, "gt-beads-polecat-lifecycle")

	// Register
	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       "lifecycle-pod",
		PodIP:         "10.0.3.1",
		PodNode:       "node-6",
		PodStatus:     "running",
		ScreenSession: "lifecycle-screen",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Verify in list
	result, err := client.AgentPodList(&AgentPodListArgs{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result.Agents) != 1 {
		t.Fatalf("expected 1 agent after register, got %d", len(result.Agents))
	}

	// Deregister
	_, err = client.AgentPodDeregister(&AgentPodDeregisterArgs{AgentID: agentID})
	if err != nil {
		t.Fatalf("deregister failed: %v", err)
	}

	// Verify not in list
	result, err = client.AgentPodList(&AgentPodListArgs{})
	if err != nil {
		t.Fatalf("list after deregister failed: %v", err)
	}
	if len(result.Agents) != 0 {
		t.Errorf("expected 0 agents after deregister, got %d", len(result.Agents))
	}
}

func TestAgentPodRegister_NonExistentAgent(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Register pod for agent that doesn't exist
	_, err := client.AgentPodRegister(&AgentPodRegisterArgs{
		AgentID: "nonexistent-agent-xyz",
		PodName: "ghost-pod",
	})
	if err == nil {
		t.Error("expected error when registering pod for non-existent agent")
	}
}
