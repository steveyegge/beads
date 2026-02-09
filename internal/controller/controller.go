package controller

import (
	"context"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	// DefaultReconcileInterval is the default interval between reconciliation loops.
	DefaultReconcileInterval = 10 * time.Second
)

// Config holds the controller configuration.
type Config struct {
	// ReconcileInterval is how often to run the reconciliation loop.
	ReconcileInterval time.Duration
	// StaleThreshold is how long before an inactive agent is considered stale.
	StaleThreshold time.Duration
	// PodTemplate holds pod creation configuration.
	PodTemplate PodTemplateConfig
}

// Controller is the agent pod controller that reconciles desired state (beads)
// with actual state (K8s pods).
type Controller struct {
	k8s    *K8sClient
	beads  BeadsClientInterface
	config Config
	logger *log.Logger
}

// New creates a new Controller.
func New(k8s *K8sClient, beads BeadsClientInterface, config Config, logger *log.Logger) *Controller {
	if config.ReconcileInterval == 0 {
		config.ReconcileInterval = DefaultReconcileInterval
	}
	if config.StaleThreshold == 0 {
		config.StaleThreshold = DefaultStaleThreshold
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Controller{
		k8s:    k8s,
		beads:  beads,
		config: config,
		logger: logger,
	}
}

// Start runs the reconciliation loop until the context is cancelled.
func (c *Controller) Start(ctx context.Context) error {
	c.logger.Printf("agent-controller starting (interval=%s, namespace=%s)",
		c.config.ReconcileInterval, c.config.PodTemplate.Namespace)

	// Run once immediately
	if err := c.reconcileOnce(ctx); err != nil {
		c.logger.Printf("initial reconciliation error: %v", err)
	}

	ticker := time.NewTicker(c.config.ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Printf("agent-controller shutting down")
			return ctx.Err()
		case <-ticker.C:
			if err := c.reconcileOnce(ctx); err != nil {
				c.logger.Printf("reconciliation error: %v", err)
			}
		}
	}
}

// reconcileOnce runs a single reconciliation pass.
func (c *Controller) reconcileOnce(ctx context.Context) error {
	c.logger.Printf("reconciliation starting")

	// Step 1: Get desired state from beads
	spawning, err := c.beads.ListSpawningAgents()
	if err != nil {
		return fmt.Errorf("failed to list spawning agents: %w", err)
	}

	done, err := c.beads.ListDoneAgents()
	if err != nil {
		return fmt.Errorf("failed to list done agents: %w", err)
	}

	registered, err := c.beads.ListRegisteredPods()
	if err != nil {
		return fmt.Errorf("failed to list registered pods: %w", err)
	}

	// Step 2: Get actual state from K8s
	actualPods, err := c.k8s.ListAgentPods(ctx)
	if err != nil {
		return fmt.Errorf("failed to list K8s pods: %w", err)
	}

	// Build lookup maps
	podsByAgent := make(map[string]*corev1.Pod)
	for i := range actualPods {
		pod := &actualPods[i]
		agentID := AgentIDFromPod(pod)
		if agentID != "" {
			podsByAgent[agentID] = pod
		}
	}

	registeredByAgent := make(map[string]bool)
	for _, info := range registered {
		registeredByAgent[info.AgentID] = true
	}

	// Step 3: Reconcile - create pods for spawning agents
	for _, agent := range spawning {
		if existingPod, exists := podsByAgent[agent.ID]; exists {
			// Pod exists in K8s — ensure it's registered in beads.
			// This handles the case where pod creation succeeded but
			// registration failed on a previous cycle.
			if !registeredByAgent[agent.ID] {
				c.logger.Printf("REGISTER existing pod %s for agent %s", existingPod.Name, agent.ID)
				if err := c.beads.RegisterPod(agent.ID, existingPod.Name, PodIP(existingPod), PodNode(existingPod)); err != nil {
					c.logger.Printf("ERROR registering existing pod for agent %s: %v", agent.ID, err)
				} else {
					registeredByAgent[agent.ID] = true
				}
			} else {
				c.logger.Printf("agent %s already has a pod, skipping create", agent.ID)
			}
			continue
		}
		c.logger.Printf("CREATE pod for agent %s (state=%s, role=%s, rig=%s)",
			agent.ID, agent.AgentState, agent.RoleType, agent.Rig)

		if err := c.createPodForAgent(ctx, agent.ID, agent.RoleType, agent.Rig); err != nil {
			c.logger.Printf("ERROR creating pod for agent %s: %v", agent.ID, err)
			continue
		}
	}

	// Step 4: Reconcile - delete pods for done/stopped agents
	for _, agent := range done {
		pod, exists := podsByAgent[agent.ID]
		if !exists {
			// No pod running, just deregister if needed
			if registeredByAgent[agent.ID] {
				c.logger.Printf("DEREGISTER orphaned pod record for agent %s", agent.ID)
				if err := c.beads.DeregisterPod(agent.ID); err != nil {
					c.logger.Printf("ERROR deregistering pod for agent %s: %v", agent.ID, err)
				}
			}
			continue
		}

		c.logger.Printf("DELETE pod for agent %s (state=%s, pod=%s)",
			agent.ID, agent.AgentState, pod.Name)
		if err := c.deletePodForAgent(ctx, agent.ID, pod.Name); err != nil {
			c.logger.Printf("ERROR deleting pod for agent %s: %v", agent.ID, err)
		}
	}

	// Step 5: Health check running pods
	for _, info := range registered {
		pod, exists := podsByAgent[info.AgentID]
		if !exists {
			// Pod registered but not found in K8s - deregister
			c.logger.Printf("DEREGISTER missing pod for agent %s (pod=%s not found in K8s)",
				info.AgentID, info.PodName)
			if err := c.beads.DeregisterPod(info.AgentID); err != nil {
				c.logger.Printf("ERROR deregistering missing pod for agent %s: %v", info.AgentID, err)
			}
			continue
		}

		health := CheckPodHealth(pod, &info, c.config.StaleThreshold)
		switch health {
		case HealthOK:
			// All good
		case HealthCrashLoop:
			c.logger.Printf("WARN agent %s pod %s is CrashLoopBackOff", info.AgentID, pod.Name)
			if err := c.beads.UpdatePodStatus(info.AgentID, "failed"); err != nil {
				c.logger.Printf("ERROR updating pod status for agent %s: %v", info.AgentID, err)
			}
		case HealthFailed:
			c.logger.Printf("WARN agent %s pod %s has failed (phase=%s)", info.AgentID, pod.Name, pod.Status.Phase)
			if err := c.beads.UpdatePodStatus(info.AgentID, "failed"); err != nil {
				c.logger.Printf("ERROR updating pod status for agent %s: %v", info.AgentID, err)
			}
		case HealthStale:
			c.logger.Printf("WARN agent %s pod %s is stale (no activity)", info.AgentID, pod.Name)
		case HealthUnknown:
			c.logger.Printf("WARN agent %s pod %s phase is unknown", info.AgentID, pod.Name)
		}
	}

	// Step 6: Orphan pod cleanup — delete K8s pods with our managed-by label
	// that are not registered in beads. This catches pods left behind when a
	// pod crashes and the controller creates a replacement in the same cycle:
	// the old pod gets deregistered but not deleted from K8s.
	orphanCount := 0
	for i := range actualPods {
		pod := &actualPods[i]
		agentID := AgentIDFromPod(pod)
		if agentID == "" {
			continue
		}
		// Only clean up pods we manage
		if pod.Labels[LabelManagedBy] != LabelManagedByValue {
			continue
		}
		if !registeredByAgent[agentID] {
			c.logger.Printf("DELETE orphan pod %s for unregistered agent %s", pod.Name, agentID)
			if err := c.k8s.DeletePod(ctx, pod.Name); err != nil {
				if !k8serrors.IsNotFound(err) {
					c.logger.Printf("ERROR deleting orphan pod %s: %v", pod.Name, err)
				}
			} else {
				orphanCount++
			}
		}
	}

	c.logger.Printf("reconciliation complete (spawning=%d, done=%d, registered=%d, actual=%d, orphans=%d)",
		len(spawning), len(done), len(registered), len(actualPods), orphanCount)

	return nil
}

// createPodForAgent creates a K8s pod for an agent and registers it in beads.
func (c *Controller) createPodForAgent(ctx context.Context, agentID, role, rig string) error {
	podSpec := BuildPodSpec(agentID, role, rig, c.config.PodTemplate)

	created, err := c.k8s.CreatePod(ctx, podSpec)
	if err != nil {
		return fmt.Errorf("K8s pod creation failed: %w", err)
	}

	c.logger.Printf("pod %s created for agent %s, waiting for Ready", created.Name, agentID)

	// Register pod in beads immediately with pending status.
	// The health check loop will update status as the pod progresses.
	if err := c.beads.RegisterPod(agentID, created.Name, PodIP(created), PodNode(created)); err != nil {
		// Pod was created but registration failed - log but don't delete the pod
		// since the next reconciliation will pick it up
		c.logger.Printf("WARNING: pod %s created but registration failed: %v", created.Name, err)
		return fmt.Errorf("pod registration failed: %w", err)
	}

	c.logger.Printf("agent %s registered with pod %s", agentID, created.Name)
	return nil
}

// deletePodForAgent deletes a K8s pod and deregisters it from beads.
func (c *Controller) deletePodForAgent(ctx context.Context, agentID, podName string) error {
	// Deregister first to prevent race conditions
	if err := c.beads.DeregisterPod(agentID); err != nil {
		c.logger.Printf("WARNING: failed to deregister pod for agent %s: %v", agentID, err)
	}

	err := c.k8s.DeletePod(ctx, podName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			c.logger.Printf("pod %s already deleted", podName)
			return nil
		}
		return fmt.Errorf("K8s pod deletion failed: %w", err)
	}

	c.logger.Printf("pod %s deleted for agent %s", podName, agentID)
	return nil
}
