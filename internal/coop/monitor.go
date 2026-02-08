package coop

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// AgentInfo contains the information needed to monitor an agent via its
// session backend. This is typically populated from agent bead fields.
type AgentInfo struct {
	AgentID       string
	PodIP         string // empty means local tmux
	ScreenSession string // tmux session name (for TmuxBackend)
	RoleType      string // polecat, crew, witness, etc.
	Rig           string
}

// MonitorEvent is emitted by AgentMonitor when an agent's state changes or
// requires attention.
type MonitorEvent struct {
	AgentID   string
	EventType MonitorEventType
	State     *AgentStateResponse // non-nil for state change events
	Error     error               // non-nil for error events
}

// MonitorEventType classifies monitor events.
type MonitorEventType int

const (
	// EventStateChange indicates the agent's state changed.
	EventStateChange MonitorEventType = iota
	// EventIdle indicates the agent has been idle too long and may need a nudge.
	EventIdle
	// EventPermissionPrompt indicates a permission prompt needs attention.
	EventPermissionPrompt
	// EventStuck indicates the agent appears stuck.
	EventStuck
	// EventExited indicates the agent process exited.
	EventExited
	// EventUnreachable indicates the backend could not reach the agent.
	EventUnreachable
)

func (t MonitorEventType) String() string {
	switch t {
	case EventStateChange:
		return "state_change"
	case EventIdle:
		return "idle"
	case EventPermissionPrompt:
		return "permission_prompt"
	case EventStuck:
		return "stuck"
	case EventExited:
		return "exited"
	case EventUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
}

// MonitorConfig configures the AgentMonitor polling behavior.
type MonitorConfig struct {
	// PollInterval is how often to poll agent state (default 5s).
	PollInterval time.Duration
	// IdleThreshold is how long an agent can be idle before triggering EventIdle (default 60s).
	IdleThreshold time.Duration
	// CoopPort is the port Coop sidecars listen on (default 3000).
	CoopPort int
	// Logger for monitor output (optional).
	Logger *log.Logger
}

// DefaultMonitorConfig returns sensible defaults.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		PollInterval:  5 * time.Second,
		IdleThreshold: 60 * time.Second,
		CoopPort:      3000,
	}
}

// AgentMonitor polls agent session backends and emits MonitorEvents when
// agents need attention. It bridges beads agent state tracking with the
// Coop/tmux session layer.
type AgentMonitor struct {
	config MonitorConfig
	opts   []Option // passed to CoopSessionBackend

	mu     sync.Mutex
	agents map[string]*monitoredAgent
}

type monitoredAgent struct {
	info      AgentInfo
	backend   SessionBackend
	lastState string
	idleSince time.Time // when state first became waiting_for_input
}

// NewAgentMonitor creates a monitor with the given config and Coop client options.
func NewAgentMonitor(config MonitorConfig, opts ...Option) *AgentMonitor {
	if config.PollInterval <= 0 {
		config.PollInterval = 5 * time.Second
	}
	if config.IdleThreshold <= 0 {
		config.IdleThreshold = 60 * time.Second
	}
	if config.CoopPort <= 0 {
		config.CoopPort = 3000
	}
	return &AgentMonitor{
		config: config,
		opts:   opts,
		agents: make(map[string]*monitoredAgent),
	}
}

// AddAgent registers an agent for monitoring. The appropriate SessionBackend
// is resolved automatically based on the agent's PodIP.
func (m *AgentMonitor) AddAgent(info AgentInfo) {
	backend := ResolveBackend(info.PodIP, m.config.CoopPort, m.opts...)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents[info.AgentID] = &monitoredAgent{
		info:    info,
		backend: backend,
	}
}

// RemoveAgent stops monitoring an agent.
func (m *AgentMonitor) RemoveAgent(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, agentID)
}

// GetBackend returns the session backend for an agent, or nil if not monitored.
func (m *AgentMonitor) GetBackend(agentID string) SessionBackend {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[agentID]; ok {
		return a.backend
	}
	return nil
}

// PollOnce checks all monitored agents once and returns any events.
// This is the core polling function used by Run and also available for
// one-shot monitoring.
func (m *AgentMonitor) PollOnce(ctx context.Context) []MonitorEvent {
	m.mu.Lock()
	agents := make([]*monitoredAgent, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	m.mu.Unlock()

	var events []MonitorEvent
	for _, a := range agents {
		evts := m.pollAgent(ctx, a)
		events = append(events, evts...)
	}
	return events
}

// Run starts the monitoring loop, emitting events to the returned channel.
// The loop runs until the context is cancelled.
func (m *AgentMonitor) Run(ctx context.Context) <-chan MonitorEvent {
	ch := make(chan MonitorEvent, 64)

	go func() {
		defer close(ch)
		ticker := time.NewTicker(m.config.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				events := m.PollOnce(ctx)
				for _, ev := range events {
					select {
					case ch <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}

func (m *AgentMonitor) pollAgent(ctx context.Context, a *monitoredAgent) []MonitorEvent {
	session := a.info.ScreenSession
	if session == "" {
		// Derive from agent ID if no screen session set
		session, _ = GetSessionName(a.info.Rig + "/" + a.info.RoleType + "/" + a.info.AgentID)
	}

	state, err := a.backend.AgentState(ctx, session)
	if err != nil {
		// Check if it's a Coop error
		if cerr, ok := err.(*CoopError); ok && cerr.IsExited() {
			return []MonitorEvent{{
				AgentID:   a.info.AgentID,
				EventType: EventExited,
				Error:     err,
			}}
		}
		return []MonitorEvent{{
			AgentID:   a.info.AgentID,
			EventType: EventUnreachable,
			Error:     err,
		}}
	}

	// tmux backend returns nil state (no classification)
	if state == nil {
		return nil
	}

	var events []MonitorEvent

	// State change detection
	if state.State != a.lastState {
		events = append(events, MonitorEvent{
			AgentID:   a.info.AgentID,
			EventType: EventStateChange,
			State:     state,
		})

		// Reset idle tracking on state change
		if state.State == StateWaitingForInput {
			a.idleSince = time.Now()
		} else {
			a.idleSince = time.Time{}
		}
		a.lastState = state.State
	}

	// Actionable state detection
	switch state.State {
	case StatePermissionPrompt, StatePlanPrompt, StateAskUser:
		events = append(events, MonitorEvent{
			AgentID:   a.info.AgentID,
			EventType: EventPermissionPrompt,
			State:     state,
		})

	case StateWaitingForInput:
		if !a.idleSince.IsZero() && time.Since(a.idleSince) > m.config.IdleThreshold {
			events = append(events, MonitorEvent{
				AgentID:   a.info.AgentID,
				EventType: EventIdle,
				State:     state,
			})
		}

	case StateError:
		events = append(events, MonitorEvent{
			AgentID:   a.info.AgentID,
			EventType: EventStuck,
			State:     state,
		})

	case StateExited:
		events = append(events, MonitorEvent{
			AgentID:   a.info.AgentID,
			EventType: EventExited,
			State:     state,
		})
	}

	return events
}

// NudgeAgent sends a nudge message to a monitored agent. Returns an error
// if the agent is not monitored or the backend doesn't support nudging.
func (m *AgentMonitor) NudgeAgent(ctx context.Context, agentID string, message string) (*NudgeResponse, error) {
	m.mu.Lock()
	a, ok := m.agents[agentID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("agent %s not monitored", agentID)
	}

	// Only CoopSessionBackend supports nudging
	coopBackend, ok := a.backend.(*CoopSessionBackend)
	if !ok {
		return nil, fmt.Errorf("nudge not supported via %s backend", a.backend.Name())
	}
	return coopBackend.client.NudgeSession(ctx, message)
}

// RespondToAgent responds to a prompt on a monitored agent. Returns an error
// if the agent is not monitored or the backend doesn't support responding.
func (m *AgentMonitor) RespondToAgent(ctx context.Context, agentID string, req RespondRequest) (*RespondResponse, error) {
	m.mu.Lock()
	a, ok := m.agents[agentID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("agent %s not monitored", agentID)
	}

	coopBackend, ok := a.backend.(*CoopSessionBackend)
	if !ok {
		return nil, fmt.Errorf("respond not supported via %s backend", a.backend.Name())
	}
	return coopBackend.client.RespondToPrompt(ctx, req)
}

// CaptureAgent captures the terminal content of a monitored agent.
func (m *AgentMonitor) CaptureAgent(ctx context.Context, agentID string) (string, error) {
	m.mu.Lock()
	a, ok := m.agents[agentID]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("agent %s not monitored", agentID)
	}

	session := a.info.ScreenSession
	return a.backend.CapturePane(ctx, session, 100)
}
