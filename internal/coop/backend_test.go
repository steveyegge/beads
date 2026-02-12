package coop

import (
	"context"
	"encoding/json"
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

func TestResolveBackendRequiresPodIP(t *testing.T) {
	_, err := ResolveBackend("", 0)
	if err == nil {
		t.Error("expected error for empty podIP")
	}
}

func TestResolveBackendCoop(t *testing.T) {
	backend, err := ResolveBackend("10.0.1.5", 3000)
	if err != nil {
		t.Fatalf("ResolveBackend error: %v", err)
	}
	if backend.Name() != "coop" {
		t.Errorf("expected coop backend for podIP, got %q", backend.Name())
	}
}

func TestResolveBackendDefaultPort(t *testing.T) {
	backend, err := ResolveBackend("10.0.1.5", 0)
	if err != nil {
		t.Fatalf("ResolveBackend error: %v", err)
	}
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
