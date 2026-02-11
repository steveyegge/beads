package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/coop"
)

func TestParseCoopURLFromNotes(t *testing.T) {
	tests := []struct {
		name  string
		notes string
		want  string
	}{
		{"found", `"coop_url: http://10.0.1.5:3000"`, "http://10.0.1.5:3000"},
		{"multiline", `"agent_id: abc\ncoop_url: http://10.0.1.5:3000\nstatus: running"`, "http://10.0.1.5:3000"},
		{"not found", `"agent_id: abc\nstatus: running"`, ""},
		{"empty", `""`, ""},
		{"with spaces", `"coop_url:  http://10.0.1.5:3000 "`, "http://10.0.1.5:3000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCoopURLFromNotes(json.RawMessage(tt.notes))
			if got != tt.want {
				t.Errorf("ParseCoopURLFromNotes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHealthCheck(t *testing.T) {
	pid := 42
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			json.NewEncoder(w).Encode(coop.HealthResponse{
				Status:    "running",
				PID:       &pid,
				UptimeSec: 120,
				AgentType: "claude",
			})
		case "/api/v1/agent/state":
			json.NewEncoder(w).Encode(coop.AgentStateResponse{
				State:     coop.StateWorking,
				AgentType: "claude",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	alive, state, uptime, gotPID := healthCheck(context.Background(), srv.URL, 5*time.Second)
	if !alive {
		t.Error("expected alive = true")
	}
	if state != coop.StateWorking {
		t.Errorf("state = %q, want %q", state, coop.StateWorking)
	}
	if uptime != 120*time.Second {
		t.Errorf("uptime = %v, want 2m0s", uptime)
	}
	if gotPID != 42 {
		t.Errorf("pid = %d, want 42", gotPID)
	}
}

func TestHealthCheckUnreachable(t *testing.T) {
	alive, state, uptime, pid := healthCheck(context.Background(), "http://127.0.0.1:1", 100*time.Millisecond)
	if alive {
		t.Error("expected alive = false for unreachable server")
	}
	if state != "" {
		t.Errorf("state = %q, want empty", state)
	}
	if uptime != 0 {
		t.Errorf("uptime = %v, want 0", uptime)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

func TestHealthCheckExited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			json.NewEncoder(w).Encode(coop.HealthResponse{
				Status:    "exited",
				UptimeSec: 60,
			})
		case "/api/v1/agent/state":
			json.NewEncoder(w).Encode(coop.AgentStateResponse{
				State: coop.StateExited,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	alive, state, _, _ := healthCheck(context.Background(), srv.URL, 5*time.Second)
	if alive {
		t.Error("expected alive = false for exited status")
	}
	if state != coop.StateExited {
		t.Errorf("state = %q, want %q", state, coop.StateExited)
	}
}

func TestDiscoverOptsDefaults(t *testing.T) {
	opts := DiscoverOpts{}
	if opts.timeout() != 5*time.Second {
		t.Errorf("timeout() = %v, want 5s", opts.timeout())
	}
	if opts.concurrency() != 10 {
		t.Errorf("concurrency() = %d, want 10", opts.concurrency())
	}
}

func TestDiscoverOptsCustom(t *testing.T) {
	opts := DiscoverOpts{
		Timeout:     2 * time.Second,
		Concurrency: 5,
	}
	if opts.timeout() != 2*time.Second {
		t.Errorf("timeout() = %v, want 2s", opts.timeout())
	}
	if opts.concurrency() != 5 {
		t.Errorf("concurrency() = %d, want 5", opts.concurrency())
	}
}

func TestNewSessionRegistry(t *testing.T) {
	// Verify constructor doesn't panic with nil
	reg := NewSessionRegistry(nil)
	if reg == nil {
		t.Fatal("NewSessionRegistry returned nil")
	}
}

func TestDiscoverRigEmptyName(t *testing.T) {
	reg := NewSessionRegistry(nil)
	_, err := reg.DiscoverRig(context.Background(), "", DiscoverOpts{})
	if err == nil {
		t.Error("expected error for empty rig name")
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"one", 1},
		{"one\ntwo", 2},
		{"one\ntwo\n", 2},
		{"one\ntwo\nthree", 3},
	}

	for _, tt := range tests {
		lines := splitLines(tt.input)
		if len(lines) != tt.want {
			t.Errorf("splitLines(%q) = %d lines, want %d", tt.input, len(lines), tt.want)
		}
	}
}

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		input     string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{"key: value", "key", "value", true},
		{"key:value", "key", "value", true},
		{"key:  value  ", "key", "value", true},
		{"no colon here", "", "", false},
		{"url: http://host:3000", "url", "http://host:3000", true},
	}

	for _, tt := range tests {
		key, value, ok := parseKeyValue(tt.input)
		if ok != tt.wantOK || key != tt.wantKey || value != tt.wantValue {
			t.Errorf("parseKeyValue(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.input, key, value, ok, tt.wantKey, tt.wantValue, tt.wantOK)
		}
	}
}
