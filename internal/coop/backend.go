package coop

import (
	"context"
	"fmt"
	"strings"
)

// SessionBackend abstracts terminal session operations. The canonical
// implementation is CoopSessionBackend (Coop HTTP API).
type SessionBackend interface {
	// HasSession returns true if the agent's terminal session is alive.
	HasSession(ctx context.Context, session string) (bool, error)

	// CapturePane returns the terminal text content (last N lines of scrollback).
	CapturePane(ctx context.Context, session string, lines int) (string, error)

	// AgentState returns the structured agent state, or nil if not supported
	// (e.g. tmux backend cannot classify state).
	AgentState(ctx context.Context, session string) (*AgentStateResponse, error)

	// Name returns a human-readable backend name for status display.
	Name() string
}

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

func (b *CoopSessionBackend) Name() string { return "coop" }

// ResolveBackend returns a CoopSessionBackend pointing at the Coop sidecar
// on the given pod. podIP must be non-empty — all agents run in K8s pods
// with Coop sidecars. coopPort is the port Coop listens on (default 3000).
func ResolveBackend(podIP string, coopPort int, opts ...Option) (SessionBackend, error) {
	if podIP == "" {
		return nil, fmt.Errorf("podIP is required: all agents must have a Coop sidecar")
	}
	if coopPort <= 0 {
		coopPort = 3000
	}
	baseURL := fmt.Sprintf("http://%s:%d", podIP, coopPort)
	return NewCoopSessionBackend(baseURL, opts...), nil
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
