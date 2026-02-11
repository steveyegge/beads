package coop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCoopSessionBackendHasSession(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"running", "running", true},
		{"exited", "exited", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(HealthResponse{Status: tt.status})
			}))
			defer srv.Close()

			backend := NewCoopSessionBackend(srv.URL)
			got, err := backend.HasSession(context.Background(), "ignored")
			if err != nil {
				t.Fatalf("HasSession error: %v", err)
			}
			if got != tt.want {
				t.Errorf("HasSession = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoopSessionBackendCapturePane(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("$ claude\nHello world\n"))
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	text, err := backend.CapturePane(context.Background(), "ignored", 100)
	if err != nil {
		t.Fatalf("CapturePane error: %v", err)
	}
	if text != "$ claude\nHello world\n" {
		t.Errorf("CapturePane = %q", text)
	}
}

func TestCoopSessionBackendAgentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AgentStateResponse{
			State:     StateWorking,
			AgentType: "claude",
		})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	state, err := backend.AgentState(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("AgentState error: %v", err)
	}
	if state.State != StateWorking {
		t.Errorf("state = %q, want working", state.State)
	}
}

func TestCoopSessionBackendName(t *testing.T) {
	backend := NewCoopSessionBackend("http://localhost:3000")
	if backend.Name() != "coop" {
		t.Errorf("Name() = %q, want coop", backend.Name())
	}
}

func TestTmuxBackendName(t *testing.T) {
	backend := &TmuxBackend{}
	if backend.Name() != "tmux" {
		t.Errorf("Name() = %q, want tmux", backend.Name())
	}
}

func TestTmuxBackendAgentStateReturnsNil(t *testing.T) {
	backend := &TmuxBackend{}
	state, err := backend.AgentState(context.Background(), "test")
	if err != nil {
		t.Fatalf("AgentState error: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state from tmux backend, got %+v", state)
	}
}

func TestResolveBackendTmux(t *testing.T) {
	backend := ResolveBackend("", 0)
	if backend.Name() != "tmux" {
		t.Errorf("expected tmux backend for empty podIP, got %q", backend.Name())
	}
}

func TestResolveBackendCoop(t *testing.T) {
	backend := ResolveBackend("10.0.1.5", 3000)
	if backend.Name() != "coop" {
		t.Errorf("expected coop backend for podIP, got %q", backend.Name())
	}
}

func TestResolveBackendDefaultPort(t *testing.T) {
	backend := ResolveBackend("10.0.1.5", 0)
	if backend.Name() != "coop" {
		t.Errorf("expected coop backend, got %q", backend.Name())
	}
}

func TestGetSessionName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantErr   bool
	}{
		{"rig/type/name", "gastown/crew/decision_point", "bd-gastown-crew-decision_point", false},
		{"rig/type", "gastown/polecats", "bd-gastown-polecats", false},
		{"empty", "", "", true},
		{"overseer", "overseer", "", true},
		{"human", "human", "", true},
		{"single part", "gastown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetSessionName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetSessionName(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("GetSessionName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCoopSessionBackendKillSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/shutdown" {
			t.Errorf("path = %q, want /api/v1/shutdown", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	err := backend.KillSession(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("KillSession error: %v", err)
	}
}

func TestCoopSessionBackendNewSessionNotSupported(t *testing.T) {
	backend := NewCoopSessionBackend("http://localhost:3000")
	err := backend.NewSession(context.Background(), "test", "echo hello")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("NewSession error = %v, want ErrNotSupported", err)
	}
}

func TestCoopSessionBackendIsAgentRunning(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{"working", StateWorking, true},
		{"idle", StateWaitingForInput, true},
		{"exited", StateExited, false},
		{"starting", StateStarting, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(AgentStateResponse{
					State:     tt.state,
					AgentType: "claude",
				})
			}))
			defer srv.Close()

			backend := NewCoopSessionBackend(srv.URL)
			got, err := backend.IsAgentRunning(context.Background(), "ignored")
			if err != nil {
				t.Fatalf("IsAgentRunning error: %v", err)
			}
			if got != tt.want {
				t.Errorf("IsAgentRunning = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoopSessionBackendIsAgentRunningExitedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:    "EXITED",
			Message: "child process exited",
		})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	got, err := backend.IsAgentRunning(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("IsAgentRunning error: %v", err)
	}
	if got {
		t.Error("expected IsAgentRunning = false for EXITED error")
	}
}

func TestCoopSessionBackendGetSessionInfo(t *testing.T) {
	pid := 12345
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthResponse{
			Status:    "running",
			PID:       &pid,
			UptimeSec: 3600,
			AgentType: "claude",
		})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	info, err := backend.GetSessionInfo(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("GetSessionInfo error: %v", err)
	}
	if info.PID != 12345 {
		t.Errorf("PID = %d, want 12345", info.PID)
	}
	if info.Uptime != 3600 {
		t.Errorf("Uptime = %d, want 3600", info.Uptime)
	}
	if !info.Ready {
		t.Error("Ready = false, want true")
	}
	if info.AgentType != "claude" {
		t.Errorf("AgentType = %q, want claude", info.AgentType)
	}
	if info.Backend != "coop" {
		t.Errorf("Backend = %q, want coop", info.Backend)
	}
}

func TestCoopSessionBackendGetSessionInfoExited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthResponse{
			Status:    "exited",
			UptimeSec: 100,
		})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	info, err := backend.GetSessionInfo(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("GetSessionInfo error: %v", err)
	}
	if info.Ready {
		t.Error("Ready = true, want false for exited status")
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 for nil PID", info.PID)
	}
}

func TestCoopSessionBackendGetAgentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AgentStateResponse{
			State:     StateWaitingForInput,
			AgentType: "claude",
		})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	state, err := backend.GetAgentState(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("GetAgentState error: %v", err)
	}
	if state != StateWaitingForInput {
		t.Errorf("state = %q, want %q", state, StateWaitingForInput)
	}
}

func TestCoopSessionBackendSetEnvironment(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody EnvPutRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(EnvPutResponse{Key: "MY_VAR", Updated: true})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	err := backend.SetEnvironment(context.Background(), "ignored", "MY_VAR", "hello")
	if err != nil {
		t.Fatalf("SetEnvironment error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/env/MY_VAR" {
		t.Errorf("path = %q, want /api/v1/env/MY_VAR", gotPath)
	}
	if gotBody.Value != "hello" {
		t.Errorf("body.value = %q, want hello", gotBody.Value)
	}
}

func TestCoopSessionBackendGetEnvironment(t *testing.T) {
	val := "world"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/env/MY_VAR" {
			t.Errorf("path = %q, want /api/v1/env/MY_VAR", r.URL.Path)
		}
		json.NewEncoder(w).Encode(EnvGetResponse{Key: "MY_VAR", Value: &val, Source: "pending"})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	got, err := backend.GetEnvironment(context.Background(), "ignored", "MY_VAR")
	if err != nil {
		t.Fatalf("GetEnvironment error: %v", err)
	}
	if got != "world" {
		t.Errorf("GetEnvironment = %q, want world", got)
	}
}

func TestCoopSessionBackendGetEnvironmentNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(EnvGetResponse{Key: "MISSING", Value: nil, Source: "child"})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	got, err := backend.GetEnvironment(context.Background(), "ignored", "MISSING")
	if err != nil {
		t.Fatalf("GetEnvironment error: %v", err)
	}
	if got != "" {
		t.Errorf("GetEnvironment = %q, want empty string for missing var", got)
	}
}

func TestCoopSessionBackendGetWorkingDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/cwd" {
			t.Errorf("path = %q, want /api/v1/session/cwd", r.URL.Path)
		}
		json.NewEncoder(w).Encode(CwdResponse{Cwd: "/home/agent/workspace"})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	got, err := backend.GetWorkingDirectory(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("GetWorkingDirectory error: %v", err)
	}
	if got != "/home/agent/workspace" {
		t.Errorf("GetWorkingDirectory = %q, want /home/agent/workspace", got)
	}
}

func TestCoopSessionBackendSendKeysRaw(t *testing.T) {
	var gotBody InputRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(InputResponse{BytesWritten: 5})
	}))
	defer srv.Close()

	backend := NewCoopSessionBackend(srv.URL)
	err := backend.SendKeysRaw(context.Background(), "ignored", "hello")
	if err != nil {
		t.Fatalf("SendKeysRaw error: %v", err)
	}
	if gotBody.Text != "hello" {
		t.Errorf("text = %q, want hello", gotBody.Text)
	}
	if gotBody.Enter {
		t.Error("Enter should be false for raw keys")
	}
}
