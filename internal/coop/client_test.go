package coop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http://localhost:3000")
	if c.baseURL != "http://localhost:3000" {
		t.Errorf("baseURL = %q, want http://localhost:3000", c.baseURL)
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", c.httpClient.Timeout)
	}
}

func TestNewClientTrailingSlash(t *testing.T) {
	c := NewClient("http://localhost:3000/")
	if c.baseURL != "http://localhost:3000" {
		t.Errorf("baseURL = %q, want trailing slash stripped", c.baseURL)
	}
}

func TestNewClientWithOptions(t *testing.T) {
	c := NewClient("http://localhost:3000",
		WithToken("secret"),
		WithTimeout(5*time.Second),
	)
	if c.token != "secret" {
		t.Errorf("token = %q, want secret", c.token)
	}
	if c.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", c.httpClient.Timeout)
	}
}

func TestHasSession(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"running", "running", true},
		{"exited", "exited", false},
		{"starting", "starting", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/health" {
					t.Errorf("path = %q, want /api/v1/health", r.URL.Path)
				}
				json.NewEncoder(w).Encode(HealthResponse{
					Status:    tt.status,
					AgentType: "claude",
				})
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			got, err := c.HasSession(context.Background())
			if err != nil {
				t.Fatalf("HasSession error: %v", err)
			}
			if got != tt.want {
				t.Errorf("HasSession = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCapturePane(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/screen/text" {
			t.Errorf("path = %q, want /api/v1/screen/text", r.URL.Path)
		}
		w.Write([]byte("$ claude\nHello, I'm Claude.\n"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	text, err := c.CapturePane(context.Background())
	if err != nil {
		t.Fatalf("CapturePane error: %v", err)
	}
	if text != "$ claude\nHello, I'm Claude.\n" {
		t.Errorf("CapturePane = %q", text)
	}
}

func TestNudgeSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/agent/nudge" {
			t.Errorf("path = %q", r.URL.Path)
		}

		var req NudgeRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Message != "Fix the bug" {
			t.Errorf("message = %q, want 'Fix the bug'", req.Message)
		}

		json.NewEncoder(w).Encode(NudgeResponse{
			Delivered:   true,
			StateBefore: StateWaitingForInput,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.NudgeSession(context.Background(), "Fix the bug")
	if err != nil {
		t.Fatalf("NudgeSession error: %v", err)
	}
	if !resp.Delivered {
		t.Error("expected delivered=true")
	}
	if resp.StateBefore != StateWaitingForInput {
		t.Errorf("state_before = %q", resp.StateBefore)
	}
}

func TestNudgeBusy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:    "AGENT_BUSY",
			Message: "agent is working",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.NudgeSession(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for busy agent")
	}
	cerr, ok := err.(*CoopError)
	if !ok {
		t.Fatalf("expected *CoopError, got %T", err)
	}
	if !cerr.IsAgentBusy() {
		t.Errorf("expected AGENT_BUSY, got %s", cerr.ErrorCode)
	}
	if cerr.StatusCode != 409 {
		t.Errorf("status = %d, want 409", cerr.StatusCode)
	}
}

func TestAgentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AgentStateResponse{
			AgentType:     "claude",
			State:         StatePermissionPrompt,
			SinceSeq:      4215,
			ScreenSeq:     4217,
			DetectionTier: "session_log",
			Prompt: &PromptContext{
				Type:         "permission",
				Tool:         "Bash",
				InputPreview: "npm install express",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	state, err := c.AgentState(context.Background())
	if err != nil {
		t.Fatalf("AgentState error: %v", err)
	}
	if state.State != StatePermissionPrompt {
		t.Errorf("state = %q", state.State)
	}
	if state.Prompt == nil {
		t.Fatal("expected prompt context")
	}
	if state.Prompt.Tool != "Bash" {
		t.Errorf("tool = %q", state.Prompt.Tool)
	}
}

func TestRespondToPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RespondRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Accept == nil || !*req.Accept {
			t.Error("expected accept=true")
		}

		json.NewEncoder(w).Encode(RespondResponse{
			Delivered:  true,
			PromptType: "permission",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.AcceptPrompt(context.Background())
	if err != nil {
		t.Fatalf("AcceptPrompt error: %v", err)
	}
	if !resp.Delivered {
		t.Error("expected delivered")
	}
	if resp.PromptType != "permission" {
		t.Errorf("prompt_type = %q", resp.PromptType)
	}
}

func TestSelectOption(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RespondRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Option == nil || *req.Option != 2 {
			t.Errorf("expected option=2, got %v", req.Option)
		}

		json.NewEncoder(w).Encode(RespondResponse{
			Delivered:  true,
			PromptType: "question",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.SelectOption(context.Background(), 2)
	if err != nil {
		t.Fatalf("SelectOption error: %v", err)
	}
	if resp.PromptType != "question" {
		t.Errorf("prompt_type = %q", resp.PromptType)
	}
}

func TestRespondNoPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:    "NO_PROMPT",
			Message: "no active prompt",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.AcceptPrompt(context.Background())
	cerr, ok := err.(*CoopError)
	if !ok {
		t.Fatalf("expected *CoopError, got %T", err)
	}
	if !cerr.IsNoPrompt() {
		t.Errorf("expected NO_PROMPT, got %s", cerr.ErrorCode)
	}
}

func TestAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			t.Errorf("auth = %q, want 'Bearer my-token'", auth)
		}
		json.NewEncoder(w).Encode(HealthResponse{Status: "running"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithToken("my-token"))
	c.HasSession(context.Background())
}

func TestNoAuthHeaderWhenNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "" {
			t.Errorf("expected no auth header, got %q", auth)
		}
		json.NewEncoder(w).Encode(HealthResponse{Status: "running"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.HasSession(context.Background())
}

func TestSignal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SignalRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Signal != "SIGTERM" {
			t.Errorf("signal = %q, want SIGTERM", req.Signal)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.Signal(context.Background(), "SIGTERM")
	if err != nil {
		t.Fatalf("Signal error: %v", err)
	}
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthResponse{
			Status:    "running",
			PID:       intPtr(12345),
			UptimeSec: 3600,
			AgentType: "claude",
			Terminal:  TerminalSize{Cols: 200, Rows: 50},
			WSClients: 2,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health error: %v", err)
	}
	if resp.Status != "running" {
		t.Errorf("status = %q", resp.Status)
	}
	if resp.PID == nil || *resp.PID != 12345 {
		t.Errorf("pid = %v", resp.PID)
	}
	if resp.Terminal.Cols != 200 || resp.Terminal.Rows != 50 {
		t.Errorf("terminal = %+v", resp.Terminal)
	}
}

func TestExitedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:    "EXITED",
			Message: "child process exited",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.AgentState(context.Background())
	cerr, ok := err.(*CoopError)
	if !ok {
		t.Fatalf("expected *CoopError, got %T", err)
	}
	if !cerr.IsExited() {
		t.Errorf("expected EXITED, got %s", cerr.ErrorCode)
	}
	if cerr.StatusCode != 410 {
		t.Errorf("status = %d, want 410", cerr.StatusCode)
	}
}

func TestCoopErrorString(t *testing.T) {
	tests := []struct {
		name string
		err  CoopError
		want string
	}{
		{
			"with code",
			CoopError{StatusCode: 409, ErrorCode: "AGENT_BUSY", Message: "agent is working"},
			"coop: AGENT_BUSY (409): agent is working",
		},
		{
			"without code",
			CoopError{StatusCode: 500, Message: "internal error"},
			"coop: HTTP 500: internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShutdown(t *testing.T) {
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

	c := NewClient(srv.URL)
	err := c.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown error: %v", err)
	}
}

func TestIsAgentRunning(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{"working", StateWorking, true},
		{"idle", StateWaitingForInput, true},
		{"starting", StateStarting, true},
		{"exited", StateExited, false},
		{"crashed", "crashed", false},
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

			c := NewClient(srv.URL)
			got, err := c.IsAgentRunning(context.Background())
			if err != nil {
				t.Fatalf("IsAgentRunning error: %v", err)
			}
			if got != tt.want {
				t.Errorf("IsAgentRunning = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAgentRunningExitedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:    "EXITED",
			Message: "child process exited",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	got, err := c.IsAgentRunning(context.Background())
	if err != nil {
		t.Fatalf("IsAgentRunning error: %v", err)
	}
	if got {
		t.Error("expected IsAgentRunning = false for EXITED error")
	}
}

func intPtr(i int) *int { return &i }
