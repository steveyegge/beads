package controller

import (
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// BeadsClient wraps the BD RPC client for controller operations.
// Each method creates its own RPC connection since the client is not goroutine-safe.
type BeadsClient struct {
	daemonAddr string
	token      string
}

// NewBeadsClient creates a new beads client that connects to the BD daemon.
func NewBeadsClient(daemonAddr, token string) *BeadsClient {
	return &BeadsClient{
		daemonAddr: daemonAddr,
		token:      token,
	}
}

// connect creates a new RPC connection for a single operation.
func (b *BeadsClient) connect() (*rpc.Client, error) {
	// Auto-prepend http:// if bare host:port
	addr := b.daemonAddr
	if !rpc.IsHTTPURL(addr) {
		addr = "http://" + addr
	}
	httpClient, err := rpc.TryConnectHTTP(addr, b.token)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to BD daemon at %s: %w", b.daemonAddr, err)
	}
	return rpc.WrapHTTPClient(httpClient), nil
}

// ListSpawningAgents returns agents with agent_state=spawning that need pods.
func (b *BeadsClient) ListSpawningAgents() ([]types.Issue, error) {
	client, err := b.connect()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	resp, err := client.List(&rpc.ListArgs{
		Labels: []string{"gt:agent"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	var issues []types.Issue
	if err := json.Unmarshal(resp.Data, &issues); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent list: %w", err)
	}

	// Filter to spawning agents
	var spawning []types.Issue
	for _, issue := range issues {
		if issue.AgentState == types.StateSpawning {
			spawning = append(spawning, issue)
		}
	}
	return spawning, nil
}

// ListDoneAgents returns agents with agent_state=done or stopped that may need pod cleanup.
func (b *BeadsClient) ListDoneAgents() ([]types.Issue, error) {
	client, err := b.connect()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	resp, err := client.List(&rpc.ListArgs{
		Labels: []string{"gt:agent"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	var issues []types.Issue
	if err := json.Unmarshal(resp.Data, &issues); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent list: %w", err)
	}

	var done []types.Issue
	for _, issue := range issues {
		if issue.AgentState == types.StateDone || issue.AgentState == types.StateStopped || issue.AgentState == types.StateDead {
			if issue.PodName != "" {
				done = append(done, issue)
			}
		}
	}
	return done, nil
}

// ListRegisteredPods returns all agents with active pod registrations.
func (b *BeadsClient) ListRegisteredPods() ([]rpc.AgentPodInfo, error) {
	client, err := b.connect()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := client.AgentPodList(&rpc.AgentPodListArgs{})
	if err != nil {
		return nil, fmt.Errorf("failed to list registered pods: %w", err)
	}
	return result.Agents, nil
}

// RegisterPod registers a K8s pod for an agent.
func (b *BeadsClient) RegisterPod(agentID, podName, podIP, podNode string) error {
	client, err := b.connect()
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:   agentID,
		PodName:   podName,
		PodIP:     podIP,
		PodNode:   podNode,
		PodStatus: "running",
	})
	if err != nil {
		return fmt.Errorf("failed to register pod for agent %s: %w", agentID, err)
	}
	return nil
}

// DeregisterPod clears pod fields for an agent.
func (b *BeadsClient) DeregisterPod(agentID string) error {
	client, err := b.connect()
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.AgentPodDeregister(&rpc.AgentPodDeregisterArgs{
		AgentID: agentID,
	})
	if err != nil {
		return fmt.Errorf("failed to deregister pod for agent %s: %w", agentID, err)
	}
	return nil
}

// UpdatePodStatus updates just the pod status for an agent.
func (b *BeadsClient) UpdatePodStatus(agentID, status string) error {
	client, err := b.connect()
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.AgentPodStatus(&rpc.AgentPodStatusArgs{
		AgentID:   agentID,
		PodStatus: status,
	})
	if err != nil {
		return fmt.Errorf("failed to update pod status for agent %s: %w", agentID, err)
	}
	return nil
}
