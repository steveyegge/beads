package controller

import (
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// BeadsClientInterface defines the operations the controller needs from the beads daemon.
// Extracted from BeadsClient to enable testing with mocks.
type BeadsClientInterface interface {
	ListSpawningAgents() ([]types.Issue, error)
	ListDoneAgents() ([]types.Issue, error)
	ListRegisteredPods() ([]rpc.AgentPodInfo, error)
	RegisterPod(agentID, podName, podIP, podNode string) error
	DeregisterPod(agentID string) error
	UpdatePodStatus(agentID, status string) error
}
