package coop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotSupported is returned when a backend does not support a given operation.
// Coop returns this for tmux-only ops like NewSession (K8s uses external orchestration).
var ErrNotSupported = errors.New("operation not supported by this backend")

// SessionBackend abstracts terminal session operations. Implementations
// include TmuxBackend (local tmux) and CoopSessionBackend (Coop HTTP API).
type SessionBackend interface {
	// HasSession returns true if the agent's terminal session is alive.
	HasSession(ctx context.Context, session string) (bool, error)

	// CapturePane returns the terminal text content (last N lines of scrollback).
	CapturePane(ctx context.Context, session string, lines int) (string, error)

	// AgentState returns the structured agent state, or nil if not supported
	// (e.g. tmux backend cannot classify state).
	AgentState(ctx context.Context, session string) (*AgentStateResponse, error)

	// Session info methods

	// GetSessionInfo returns metadata about the session (PID, uptime, readiness).
	// tmux impl: parses tmux display-message. Coop impl: GET /api/v1/health.
	GetSessionInfo(ctx context.Context, session string) (*SessionInfo, error)

	// GetAgentState returns the agent state as a simple string.
	// tmux impl: infers from pane (running/dead). Coop impl: GET /api/v1/agent/state.
	GetAgentState(ctx context.Context, session string) (string, error)

	// Session lifecycle methods

	// KillSession terminates an agent's terminal session.
	// tmux impl: tmux kill-session. Coop impl: POST /api/v1/shutdown.
	KillSession(ctx context.Context, session string) error

	// NewSession creates a new terminal session running the given command.
	// tmux impl: tmux new-session -d -s <name> <command>.
	// Coop impl: returns ErrNotSupported (K8s uses external orchestration).
	NewSession(ctx context.Context, name, command string) error

	// IsAgentRunning returns true if the agent process is actively running
	// (not exited or crashed).
	// tmux impl: checks pane_dead. Coop impl: GET /api/v1/agent/state, state != exited/crashed.
	IsAgentRunning(ctx context.Context, session string) (bool, error)

	// Environment methods

	// SetEnvironment stores an environment variable. For tmux: tmux set-environment.
	// For coop: PUT /api/v1/env/:key (pending, applied on next switch).
	SetEnvironment(ctx context.Context, session, key, value string) error

	// GetEnvironment reads an environment variable. For tmux: tmux show-environment.
	// For coop: GET /api/v1/env/:key (checks pending first, then child /proc).
	GetEnvironment(ctx context.Context, session, key string) (string, error)

	// GetWorkingDirectory returns the working directory of the agent process.
	// For tmux: tmux display-message #{pane_current_path}.
	// For coop: GET /api/v1/session/cwd.
	GetWorkingDirectory(ctx context.Context, session string) (string, error)

	// SendKeysRaw sends raw text to the terminal pane without enter.
	// For tmux: tmux send-keys -l. For coop: POST /api/v1/input.
	SendKeysRaw(ctx context.Context, session, text string) error

	// Name returns a human-readable backend name for status display.
	Name() string
}

// TmuxBackend implements SessionBackend using local tmux commands.
// This is the legacy backend used when agents run as local tmux sessions.
type TmuxBackend struct{}

func (b *TmuxBackend) HasSession(ctx context.Context, session string) (bool, error) {
	cmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", session)
	err := cmd.Run()
	return err == nil, nil
}

func (b *TmuxBackend) CapturePane(ctx context.Context, session string, lines int) (string, error) {
	scrollback := fmt.Sprintf("-%d", lines)
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", session, "-p", "-S", scrollback)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("capture failed: %s", stderr.String())
	}
	return stdout.String(), nil
}

func (b *TmuxBackend) AgentState(_ context.Context, _ string) (*AgentStateResponse, error) {
	return nil, nil // tmux cannot classify agent state
}

func (b *TmuxBackend) GetSessionInfo(ctx context.Context, session string) (*SessionInfo, error) {
	// Get pane PID via tmux display-message.
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", session, "-p", "#{pane_pid}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tmux display-message: %s", stderr.String())
	}
	pid := 0
	fmt.Sscanf(strings.TrimSpace(stdout.String()), "%d", &pid)

	return &SessionInfo{
		SessionID: session,
		PID:       pid,
		Ready:     true,
		Backend:   "tmux",
	}, nil
}

func (b *TmuxBackend) GetAgentState(ctx context.Context, session string) (string, error) {
	// Check if the pane process is alive via tmux.
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", session, "-p", "#{pane_dead}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux display-message: %s", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "1" {
		return "dead", nil
	}
	return "running", nil
}

func (b *TmuxBackend) KillSession(ctx context.Context, session string) error {
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", session)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux kill-session: %s", stderr.String())
	}
	return nil
}

func (b *TmuxBackend) NewSession(ctx context.Context, name, command string) error {
	cmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", name, command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux new-session: %s", stderr.String())
	}
	return nil
}

func (b *TmuxBackend) IsAgentRunning(ctx context.Context, session string) (bool, error) {
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", session, "-p", "#{pane_dead}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Session doesn't exist — agent is not running.
		return false, nil
	}
	return strings.TrimSpace(stdout.String()) != "1", nil
}

func (b *TmuxBackend) SetEnvironment(ctx context.Context, session, key, value string) error {
	cmd := exec.CommandContext(ctx, "tmux", "set-environment", "-t", session, key, value)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux set-environment: %s", stderr.String())
	}
	return nil
}

func (b *TmuxBackend) GetEnvironment(ctx context.Context, session, key string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "show-environment", "-t", session, key)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux show-environment: %s", stderr.String())
	}
	// Output format: KEY=VALUE
	line := strings.TrimSpace(stdout.String())
	if _, v, ok := strings.Cut(line, "="); ok {
		return v, nil
	}
	return "", nil
}

func (b *TmuxBackend) GetWorkingDirectory(ctx context.Context, session string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", session, "-p", "#{pane_current_path}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux display-message: %s", stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (b *TmuxBackend) SendKeysRaw(ctx context.Context, session, text string) error {
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", session, "-l", text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux send-keys: %s", stderr.String())
	}
	return nil
}

func (b *TmuxBackend) Name() string { return "tmux" }

// CoopSessionBackend implements SessionBackend using the Coop HTTP API.
// Each instance wraps a Client pointing at a specific Coop sidecar.
type CoopSessionBackend struct {
	client *Client
}

// NewCoopSessionBackend creates a SessionBackend backed by a Coop sidecar
// at the given base URL (e.g. "http://10.0.1.5:3000").
func NewCoopSessionBackend(baseURL string, opts ...Option) *CoopSessionBackend {
	return &CoopSessionBackend{
		client: NewClient(baseURL, opts...),
	}
}

func (b *CoopSessionBackend) HasSession(ctx context.Context, _ string) (bool, error) {
	return b.client.HasSession(ctx)
}

func (b *CoopSessionBackend) CapturePane(ctx context.Context, _ string, _ int) (string, error) {
	return b.client.CapturePane(ctx)
}

func (b *CoopSessionBackend) AgentState(ctx context.Context, _ string) (*AgentStateResponse, error) {
	return b.client.AgentState(ctx)
}

func (b *CoopSessionBackend) GetSessionInfo(ctx context.Context, _ string) (*SessionInfo, error) {
	health, err := b.client.Health(ctx)
	if err != nil {
		return nil, err
	}
	pid := 0
	if health.PID != nil {
		pid = *health.PID
	}
	return &SessionInfo{
		SessionID: "",
		PID:       pid,
		Uptime:    health.UptimeSec,
		Ready:     health.Status == ProcessRunning,
		AgentType: health.AgentType,
		Backend:   "coop",
	}, nil
}

func (b *CoopSessionBackend) GetAgentState(ctx context.Context, _ string) (string, error) {
	resp, err := b.client.AgentState(ctx)
	if err != nil {
		return "", err
	}
	return resp.State, nil
}

func (b *CoopSessionBackend) KillSession(ctx context.Context, _ string) error {
	return b.client.Shutdown(ctx)
}

func (b *CoopSessionBackend) NewSession(_ context.Context, _, _ string) error {
	return ErrNotSupported // K8s uses external orchestration (create bead → controller → pod)
}

func (b *CoopSessionBackend) IsAgentRunning(ctx context.Context, _ string) (bool, error) {
	return b.client.IsAgentRunning(ctx)
}

func (b *CoopSessionBackend) SetEnvironment(ctx context.Context, _, key, value string) error {
	return b.client.SetEnvironment(ctx, key, value)
}

func (b *CoopSessionBackend) GetEnvironment(ctx context.Context, _, key string) (string, error) {
	return b.client.GetEnvironment(ctx, key)
}

func (b *CoopSessionBackend) GetWorkingDirectory(ctx context.Context, _ string) (string, error) {
	return b.client.GetWorkingDirectory(ctx)
}

func (b *CoopSessionBackend) SendKeysRaw(ctx context.Context, _, text string) error {
	_, err := b.client.SendInput(ctx, text, false)
	return err
}

func (b *CoopSessionBackend) Name() string { return "coop" }

// ResolveBackend returns the appropriate SessionBackend for a given agent
// session. If podIP is non-empty, it returns a CoopSessionBackend pointing
// at the Coop sidecar on that pod; otherwise it returns the TmuxBackend.
// coopPort is the port Coop listens on (default 3000).
func ResolveBackend(podIP string, coopPort int, opts ...Option) SessionBackend {
	if podIP == "" {
		return &TmuxBackend{}
	}
	if coopPort <= 0 {
		coopPort = 3000
	}
	baseURL := fmt.Sprintf("http://%s:%d", podIP, coopPort)
	return NewCoopSessionBackend(baseURL, opts...)
}

// GetSessionName converts a RequestedBy agent path to a tmux session name.
// e.g., "gastown/crew/decision_point" -> "bd-gastown-crew-decision_point"
func GetSessionName(requestedBy string) (string, error) {
	if requestedBy == "" {
		return "", fmt.Errorf("no requestor specified")
	}
	if requestedBy == "overseer" || requestedBy == "human" {
		return "", fmt.Errorf("cannot peek human session")
	}
	parts := strings.Split(requestedBy, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid requestor format: %s", requestedBy)
	}
	return "bd-" + strings.ReplaceAll(requestedBy, "/", "-"), nil
}
