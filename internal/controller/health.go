package controller

import (
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	corev1 "k8s.io/api/core/v1"
)

const (
	// DefaultStaleThreshold is the duration after which an agent with no activity
	// is considered stale and may need its pod restarted.
	DefaultStaleThreshold = 15 * time.Minute
)

// HealthStatus represents the health assessment of an agent pod.
type HealthStatus int

const (
	// HealthOK means the pod and agent are both healthy.
	HealthOK HealthStatus = iota
	// HealthStale means the agent has not reported activity recently.
	HealthStale
	// HealthCrashLoop means the pod is in CrashLoopBackOff.
	HealthCrashLoop
	// HealthFailed means the pod has failed.
	HealthFailed
	// HealthUnknown means the pod phase is unknown.
	HealthUnknown
)

func (h HealthStatus) String() string {
	switch h {
	case HealthOK:
		return "ok"
	case HealthStale:
		return "stale"
	case HealthCrashLoop:
		return "crash_loop"
	case HealthFailed:
		return "failed"
	case HealthUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// CheckPodHealth evaluates the health of an agent pod.
func CheckPodHealth(pod *corev1.Pod, agentInfo *rpc.AgentPodInfo, staleThreshold time.Duration) HealthStatus {
	if staleThreshold == 0 {
		staleThreshold = DefaultStaleThreshold
	}

	// Check K8s pod phase
	switch PodPhase(pod) {
	case corev1.PodFailed:
		return HealthFailed
	case corev1.PodUnknown:
		return HealthUnknown
	case corev1.PodSucceeded:
		// Pod completed - should be cleaned up
		return HealthFailed
	}

	// Check for CrashLoopBackOff
	if IsPodCrashLooping(pod) {
		return HealthCrashLoop
	}

	// If we have agent info, check last activity
	// Note: agentInfo may be nil if we only have K8s data
	if agentInfo != nil && agentInfo.AgentState == "running" || (agentInfo != nil && agentInfo.AgentState == "working") {
		// Agent claims to be active - trust K8s health probes for now
		return HealthOK
	}

	return HealthOK
}
