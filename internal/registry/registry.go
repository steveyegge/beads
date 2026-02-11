// Package registry provides cross-backend agent session discovery.
// It queries beads (via daemon RPC) for agent-type beads and optionally
// health-checks each agent's coop sidecar.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/coop"
	"github.com/steveyegge/beads/internal/rpc"
)

// AgentSession represents a discovered agent session with its backend details.
type AgentSession struct {
	Name      string        `json:"name"`
	Address   string        `json:"address"`
	Rig       string        `json:"rig"`
	Role      string        `json:"role"`
	SessionID string        `json:"session_id"`
	CoopURL   string        `json:"coop_url,omitempty"`
	Alive     bool          `json:"alive"`
	State     string        `json:"state,omitempty"`
	Uptime    time.Duration `json:"uptime,omitempty"`
	PID       int           `json:"pid,omitempty"`
}

// DiscoverOpts controls the discovery behavior.
type DiscoverOpts struct {
	CheckLiveness bool          // If true, health-check each agent's coop sidecar
	Timeout       time.Duration // Per-agent health check timeout (default 5s)
	Concurrency   int           // Max concurrent health checks (default 10)
}

func (o DiscoverOpts) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return 5 * time.Second
}

func (o DiscoverOpts) concurrency() int {
	if o.Concurrency > 0 {
		return o.Concurrency
	}
	return 10
}

// SessionRegistry discovers agent sessions across backends using
// the daemon RPC as the canonical source of truth.
type SessionRegistry struct {
	client *rpc.Client
}

// NewSessionRegistry creates a registry backed by the given RPC client.
func NewSessionRegistry(client *rpc.Client) *SessionRegistry {
	return &SessionRegistry{client: client}
}

// DiscoverAll queries the daemon for all agent beads with active pods
// and optionally health-checks each.
func (r *SessionRegistry) DiscoverAll(ctx context.Context, opts DiscoverOpts) ([]AgentSession, error) {
	return r.discover(ctx, "", opts)
}

// DiscoverRig queries the daemon for agent beads in a specific rig.
func (r *SessionRegistry) DiscoverRig(ctx context.Context, rig string, opts DiscoverOpts) ([]AgentSession, error) {
	if rig == "" {
		return nil, fmt.Errorf("rig name is required")
	}
	return r.discover(ctx, rig, opts)
}

func (r *SessionRegistry) discover(ctx context.Context, rig string, opts DiscoverOpts) ([]AgentSession, error) {
	result, err := r.client.AgentPodList(&rpc.AgentPodListArgs{Rig: rig})
	if err != nil {
		return nil, fmt.Errorf("agent_pod_list RPC: %w", err)
	}

	sessions := make([]AgentSession, 0, len(result.Agents))
	for _, agent := range result.Agents {
		coopURL := ""
		if agent.PodIP != "" {
			coopURL = fmt.Sprintf("http://%s:3000", agent.PodIP)
		}

		sessions = append(sessions, AgentSession{
			Name:      agent.PodName,
			Address:   agent.PodIP,
			Rig:       agent.Rig,
			Role:      agent.RoleType,
			SessionID: agent.AgentID,
			CoopURL:   coopURL,
			State:     agent.AgentState,
			Alive:     agent.PodStatus == "Running",
		})
	}

	if opts.CheckLiveness && len(sessions) > 0 {
		r.healthCheckAll(ctx, sessions, opts)
	}

	return sessions, nil
}

// healthCheckAll concurrently health-checks all sessions with coop URLs.
func (r *SessionRegistry) healthCheckAll(ctx context.Context, sessions []AgentSession, opts DiscoverOpts) {
	sem := make(chan struct{}, opts.concurrency())
	var wg sync.WaitGroup

	for i := range sessions {
		if sessions[i].CoopURL == "" {
			continue
		}

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			alive, state, uptime, pid := healthCheck(ctx, sessions[idx].CoopURL, opts.timeout())
			sessions[idx].Alive = alive
			if state != "" {
				sessions[idx].State = state
			}
			sessions[idx].Uptime = uptime
			sessions[idx].PID = pid
		}(i)
	}

	wg.Wait()
}

// healthCheck probes a coop sidecar and returns liveness + state info.
func healthCheck(ctx context.Context, coopURL string, timeout time.Duration) (alive bool, state string, uptime time.Duration, pid int) {
	client := coop.NewClient(coopURL, coop.WithHTTPClient(&http.Client{Timeout: timeout}))

	health, err := client.Health(ctx)
	if err != nil {
		return false, "", 0, 0
	}

	alive = health.Status == coop.ProcessRunning
	uptime = time.Duration(health.UptimeSec) * time.Second
	if health.PID != nil {
		pid = *health.PID
	}

	// Also get agent state if healthy.
	agentState, err := client.AgentState(ctx)
	if err == nil && agentState != nil {
		state = agentState.State
	}

	return alive, state, uptime, pid
}

// ParseCoopURLFromNotes extracts a coop_url value from a bead's notes field.
// Notes are formatted as "key: value" lines. Returns empty string if not found.
func ParseCoopURLFromNotes(notesJSON json.RawMessage) string {
	var notes string
	if err := json.Unmarshal(notesJSON, &notes); err != nil {
		// Try raw string
		notes = string(notesJSON)
	}

	for _, line := range splitLines(notes) {
		key, value, ok := parseKeyValue(line)
		if ok && key == "coop_url" {
			return value
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func parseKeyValue(line string) (key, value string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			key = trimSpace(line[:i])
			value = trimSpace(line[i+1:])
			return key, value, true
		}
	}
	return "", "", false
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
