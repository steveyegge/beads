package coop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockAgentServer creates a test server that returns a configurable agent state.
func mockAgentServer(state AgentStateResponse) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			json.NewEncoder(w).Encode(HealthResponse{Status: "running"})
		case "/api/v1/agent/state":
			json.NewEncoder(w).Encode(state)
		case "/api/v1/screen/text":
			w.Write([]byte("$ claude\nworking...\n"))
		case "/api/v1/agent/nudge":
			json.NewEncoder(w).Encode(NudgeResponse{Delivered: true, StateBefore: StateWaitingForInput})
		case "/api/v1/agent/respond":
			json.NewEncoder(w).Encode(RespondResponse{Delivered: true, PromptType: "permission"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAgentMonitorAddRemove(t *testing.T) {
	m := NewAgentMonitor(DefaultMonitorConfig())

	m.AddAgent(AgentInfo{AgentID: "gt-test-polecat-nux", PodIP: "10.0.1.5"})
	if m.GetBackend("gt-test-polecat-nux") == nil {
		t.Error("expected backend after AddAgent")
	}
	if m.GetBackend("gt-test-polecat-nux").Name() != "coop" {
		t.Errorf("expected coop backend for agent with PodIP")
	}

	m.AddAgent(AgentInfo{AgentID: "gt-local-crew-dev"})
	if m.GetBackend("gt-local-crew-dev").Name() != "tmux" {
		t.Errorf("expected tmux backend for agent without PodIP")
	}

	m.RemoveAgent("gt-test-polecat-nux")
	if m.GetBackend("gt-test-polecat-nux") != nil {
		t.Error("expected nil backend after RemoveAgent")
	}
}

func TestAgentMonitorPollOnceStateChange(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{
		State:     StateWorking,
		AgentType: "claude",
	})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	// Override: point at our test server instead of a real pod
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	events := m.PollOnce(context.Background())
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	found := false
	for _, ev := range events {
		if ev.EventType == EventStateChange && ev.State.State == StateWorking {
			found = true
		}
	}
	if !found {
		t.Error("expected EventStateChange with state=working")
	}
}

func TestAgentMonitorPollOncePermissionPrompt(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{
		State:     StatePermissionPrompt,
		AgentType: "claude",
		Prompt: &PromptContext{
			Type:         "permission",
			Tool:         "Bash",
			InputPreview: "rm -rf /tmp/test",
		},
	})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	events := m.PollOnce(context.Background())

	hasPrompt := false
	for _, ev := range events {
		if ev.EventType == EventPermissionPrompt {
			hasPrompt = true
			if ev.State.Prompt == nil {
				t.Error("expected prompt context")
			} else if ev.State.Prompt.Tool != "Bash" {
				t.Errorf("prompt tool = %q, want Bash", ev.State.Prompt.Tool)
			}
		}
	}
	if !hasPrompt {
		t.Error("expected EventPermissionPrompt")
	}
}

func TestAgentMonitorPollOnceExited(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{
		State: StateExited,
	})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	events := m.PollOnce(context.Background())

	hasExited := false
	for _, ev := range events {
		if ev.EventType == EventExited {
			hasExited = true
		}
	}
	if !hasExited {
		t.Error("expected EventExited")
	}
}

func TestAgentMonitorPollOnceNoChangeNoDuplicate(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{
		State:     StateWorking,
		AgentType: "claude",
	})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	// First poll should emit state change
	events1 := m.PollOnce(context.Background())
	stateChanges := 0
	for _, ev := range events1 {
		if ev.EventType == EventStateChange {
			stateChanges++
		}
	}
	if stateChanges != 1 {
		t.Errorf("first poll: expected 1 state change, got %d", stateChanges)
	}

	// Second poll with same state should NOT emit state change
	events2 := m.PollOnce(context.Background())
	stateChanges = 0
	for _, ev := range events2 {
		if ev.EventType == EventStateChange {
			stateChanges++
		}
	}
	if stateChanges != 0 {
		t.Errorf("second poll: expected 0 state changes, got %d", stateChanges)
	}
}

func TestAgentMonitorIdleThreshold(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{
		State:     StateWaitingForInput,
		AgentType: "claude",
	})
	defer srv.Close()

	config := DefaultMonitorConfig()
	config.IdleThreshold = 10 * time.Millisecond // very short for test

	m := NewAgentMonitor(config)
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	// First poll: sets idleSince, should NOT trigger EventIdle yet
	events1 := m.PollOnce(context.Background())
	hasIdle := false
	for _, ev := range events1 {
		if ev.EventType == EventIdle {
			hasIdle = true
		}
	}
	if hasIdle {
		t.Error("first poll should not trigger EventIdle immediately")
	}

	// Wait for idle threshold
	time.Sleep(20 * time.Millisecond)

	// Second poll: should trigger EventIdle
	events2 := m.PollOnce(context.Background())
	hasIdle = false
	for _, ev := range events2 {
		if ev.EventType == EventIdle {
			hasIdle = true
		}
	}
	if !hasIdle {
		t.Error("expected EventIdle after threshold")
	}
}

func TestAgentMonitorRun(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{
		State:     StateWorking,
		AgentType: "claude",
	})
	defer srv.Close()

	config := DefaultMonitorConfig()
	config.PollInterval = 50 * time.Millisecond

	m := NewAgentMonitor(config)
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := m.Run(ctx)

	var received []MonitorEvent
	for ev := range ch {
		received = append(received, ev)
	}

	if len(received) == 0 {
		t.Error("expected at least one event from Run")
	}
}

func TestAgentMonitorNudge(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{State: StateWaitingForInput})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	resp, err := m.NudgeAgent(context.Background(), "test-agent", "Fix the bug")
	if err != nil {
		t.Fatalf("NudgeAgent error: %v", err)
	}
	if !resp.Delivered {
		t.Error("expected delivered=true")
	}
}

func TestAgentMonitorNudgeUnknownAgent(t *testing.T) {
	m := NewAgentMonitor(DefaultMonitorConfig())

	_, err := m.NudgeAgent(context.Background(), "nonexistent", "hello")
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

func TestAgentMonitorNudgeTmuxBackend(t *testing.T) {
	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["local-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "local-agent"},
		backend: &TmuxBackend{},
	}

	_, err := m.NudgeAgent(context.Background(), "local-agent", "hello")
	if err == nil {
		t.Error("expected error: nudge not supported via tmux")
	}
}

func TestAgentMonitorRespondToAgent(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{State: StatePermissionPrompt})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	accept := true
	resp, err := m.RespondToAgent(context.Background(), "test-agent", RespondRequest{Accept: &accept})
	if err != nil {
		t.Fatalf("RespondToAgent error: %v", err)
	}
	if !resp.Delivered {
		t.Error("expected delivered=true")
	}
}

func TestAgentMonitorCaptureAgent(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{State: StateWorking})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent", ScreenSession: "bd-test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	text, err := m.CaptureAgent(context.Background(), "test-agent")
	if err != nil {
		t.Fatalf("CaptureAgent error: %v", err)
	}
	if text != "$ claude\nworking...\n" {
		t.Errorf("CaptureAgent = %q", text)
	}
}

func TestAgentMonitorUnreachable(t *testing.T) {
	// Point at a closed server
	srv := mockAgentServer(AgentStateResponse{State: StateWorking})
	srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	events := m.PollOnce(context.Background())
	hasUnreachable := false
	for _, ev := range events {
		if ev.EventType == EventUnreachable {
			hasUnreachable = true
		}
	}
	if !hasUnreachable {
		t.Error("expected EventUnreachable for closed server")
	}
}

func TestAgentMonitorErrorState(t *testing.T) {
	srv := mockAgentServer(AgentStateResponse{
		State: StateError,
	})
	defer srv.Close()

	m := NewAgentMonitor(DefaultMonitorConfig())
	m.agents["test-agent"] = &monitoredAgent{
		info:    AgentInfo{AgentID: "test-agent"},
		backend: NewCoopSessionBackend(srv.URL),
	}

	events := m.PollOnce(context.Background())
	hasStuck := false
	for _, ev := range events {
		if ev.EventType == EventStuck {
			hasStuck = true
		}
	}
	if !hasStuck {
		t.Error("expected EventStuck for error state")
	}
}

func TestMonitorEventTypeString(t *testing.T) {
	tests := []struct {
		t    MonitorEventType
		want string
	}{
		{EventStateChange, "state_change"},
		{EventIdle, "idle"},
		{EventPermissionPrompt, "permission_prompt"},
		{EventStuck, "stuck"},
		{EventExited, "exited"},
		{EventUnreachable, "unreachable"},
		{MonitorEventType(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestDefaultMonitorConfig(t *testing.T) {
	c := DefaultMonitorConfig()
	if c.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want 5s", c.PollInterval)
	}
	if c.IdleThreshold != 60*time.Second {
		t.Errorf("IdleThreshold = %v, want 60s", c.IdleThreshold)
	}
	if c.CoopPort != 3000 {
		t.Errorf("CoopPort = %d, want 3000", c.CoopPort)
	}
}
