package notification

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// hq-946577.20: Tests for notification dispatch

func TestBuildPayload(t *testing.T) {
	dispatcher := &Dispatcher{
		config:  nil,
		baseURL: "https://beads.example.com",
	}

	dp := &types.DecisionPoint{
		IssueID:       "gt-abc123.decision-1",
		Prompt:        "Which caching strategy?",
		Options:       `[{"id":"a","short":"Redis","label":"Use Redis"},{"id":"b","short":"Memory","label":"In-memory"}]`,
		DefaultOption: "a",
		CreatedAt:     time.Now(),
	}

	issue := &types.Issue{
		ID:       "gt-abc123.decision-1",
		Timeout:  24 * time.Hour,
		Assignee: "beads/crew/test",
	}

	payload, err := dispatcher.BuildPayload(dp, issue, nil)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}

	if payload.Type != "decision_point" {
		t.Errorf("Type = %q, want 'decision_point'", payload.Type)
	}
	if payload.ID != "gt-abc123.decision-1" {
		t.Errorf("ID = %q, want 'gt-abc123.decision-1'", payload.ID)
	}
	if payload.Prompt != "Which caching strategy?" {
		t.Errorf("Prompt = %q, want 'Which caching strategy?'", payload.Prompt)
	}
	if len(payload.Options) != 2 {
		t.Errorf("len(Options) = %d, want 2", len(payload.Options))
	}
	if payload.Default != "a" {
		t.Errorf("Default = %q, want 'a'", payload.Default)
	}
	if payload.TimeoutAt == nil {
		t.Error("TimeoutAt is nil, expected a value")
	}
	if payload.RespondURL != "https://beads.example.com/api/decisions/gt-abc123.decision-1/respond" {
		t.Errorf("RespondURL = %q, unexpected", payload.RespondURL)
	}
	if payload.ViewURL != "https://beads.example.com/decisions/gt-abc123.decision-1" {
		t.Errorf("ViewURL = %q, unexpected", payload.ViewURL)
	}
}

func TestBuildPayload_WithSource(t *testing.T) {
	dispatcher := &Dispatcher{
		config:  nil,
		baseURL: "",
	}

	dp := &types.DecisionPoint{
		IssueID: "gt-abc123.decision-deploy",
		Prompt:  "Choose deployment?",
		Options: `[{"id":"yes","label":"Yes"}]`,
	}

	source := &PayloadSource{
		Agent:    "beads/crew/test",
		Molecule: "gt-abc123",
		Step:     "deploy",
	}

	payload, err := dispatcher.BuildPayload(dp, nil, source)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}

	if payload.Source == nil {
		t.Fatal("Source is nil")
	}
	if payload.Source.Agent != "beads/crew/test" {
		t.Errorf("Source.Agent = %q, want 'beads/crew/test'", payload.Source.Agent)
	}
	if payload.Source.Molecule != "gt-abc123" {
		t.Errorf("Source.Molecule = %q, want 'gt-abc123'", payload.Source.Molecule)
	}
}

func TestLoadEscalationConfig(t *testing.T) {
	// Create temp directory with config
	tempDir := t.TempDir()
	settingsDir := filepath.Join(tempDir, "settings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("Failed to create settings dir: %v", err)
	}

	config := `{
		"type": "escalation",
		"version": 2,
		"decision_routes": {
			"default": ["email:human", "webhook"],
			"urgent": ["email:human", "sms:human"]
		},
		"contacts": {
			"human_email": "test@example.com",
			"human_sms": "+1234567890",
			"decision_webhook": "https://webhook.example.com"
		}
	}`

	configPath := filepath.Join(settingsDir, "escalation.json")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	loaded, err := LoadEscalationConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadEscalationConfig failed: %v", err)
	}

	if loaded.Version != 2 {
		t.Errorf("Version = %d, want 2", loaded.Version)
	}
	if len(loaded.DecisionRoutes["default"]) != 2 {
		t.Errorf("DecisionRoutes[default] = %v, want 2 routes", loaded.DecisionRoutes["default"])
	}
	if loaded.Contacts["human_email"] != "test@example.com" {
		t.Errorf("Contacts[human_email] = %q, want 'test@example.com'", loaded.Contacts["human_email"])
	}
}

func TestLoadEscalationConfig_NotFound(t *testing.T) {
	tempDir := t.TempDir()

	_, err := LoadEscalationConfig(tempDir)
	if err == nil {
		t.Error("Expected error for missing config, got nil")
	}
}

func TestDispatcher_GetRoutes(t *testing.T) {
	tests := []struct {
		name     string
		config   *EscalationConfig
		routeKey string
		want     []string
	}{
		{
			name:     "nil config returns log",
			config:   nil,
			routeKey: "default",
			want:     []string{"log"},
		},
		{
			name: "existing route",
			config: &EscalationConfig{
				DecisionRoutes: map[string][]string{
					"default": {"email:human", "webhook"},
					"urgent":  {"email:human", "sms:human"},
				},
			},
			routeKey: "urgent",
			want:     []string{"email:human", "sms:human"},
		},
		{
			name: "fallback to default",
			config: &EscalationConfig{
				DecisionRoutes: map[string][]string{
					"default": {"webhook"},
				},
			},
			routeKey: "nonexistent",
			want:     []string{"webhook"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Dispatcher{config: tt.config}
			got := d.getRoutes(tt.routeKey)

			if len(got) != len(tt.want) {
				t.Errorf("getRoutes(%q) = %v, want %v", tt.routeKey, got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("getRoutes(%q)[%d] = %q, want %q", tt.routeKey, i, v, tt.want[i])
				}
			}
		})
	}
}

func TestDispatcher_ResolveContact(t *testing.T) {
	config := &EscalationConfig{
		Contacts: map[string]string{
			"human_email":      "human@example.com",
			"human_sms":        "+1234567890",
			"decision_webhook": "https://webhook.example.com",
		},
	}

	d := &Dispatcher{config: config}

	tests := []struct {
		name        string
		contactName string
		contactType string
		want        string
	}{
		{"email with type", "human", "email", "human@example.com"},
		{"sms with type", "human", "sms", "+1234567890"},
		{"webhook direct", "decision_webhook", "", "https://webhook.example.com"},
		{"not found", "unknown", "email", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.resolveContact(tt.contactName, tt.contactType)
			if got != tt.want {
				t.Errorf("resolveContact(%q, %q) = %q, want %q", tt.contactName, tt.contactType, got, tt.want)
			}
		})
	}
}

func TestDispatcher_SendWebhook(t *testing.T) {
	var receivedPayload DecisionPayload
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("Failed to decode payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := &Dispatcher{
		httpClient: server.Client(),
	}

	payload := &DecisionPayload{
		Type:   "decision_point",
		ID:     "test-123",
		Prompt: "Test prompt?",
	}

	err := d.sendWebhook(payload, server.URL)
	if err != nil {
		t.Fatalf("sendWebhook failed: %v", err)
	}

	// Verify payload was received correctly
	if receivedPayload.ID != "test-123" {
		t.Errorf("Received ID = %q, want 'test-123'", receivedPayload.ID)
	}
	if receivedPayload.Prompt != "Test prompt?" {
		t.Errorf("Received Prompt = %q, want 'Test prompt?'", receivedPayload.Prompt)
	}

	// Verify headers
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want 'application/json'", receivedHeaders.Get("Content-Type"))
	}
	if receivedHeaders.Get("X-Beads-Event") != "decision_point" {
		t.Errorf("X-Beads-Event = %q, want 'decision_point'", receivedHeaders.Get("X-Beads-Event"))
	}
}

func TestDispatcher_SendWebhook_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	d := &Dispatcher{
		httpClient: server.Client(),
	}

	payload := &DecisionPayload{
		Type: "decision_point",
		ID:   "test-123",
	}

	err := d.sendWebhook(payload, server.URL)
	if err == nil {
		t.Error("Expected error for 500 response, got nil")
	}
}

func TestDispatch_LogChannel(t *testing.T) {
	d := &Dispatcher{
		config: &EscalationConfig{
			DecisionRoutes: map[string][]string{
				"default": {"log"},
			},
		},
	}

	payload := &DecisionPayload{
		Type:   "decision_point",
		ID:     "test-123",
		Prompt: "Test prompt?",
	}

	results := d.Dispatch(payload, "default")

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Error("Log channel should always succeed")
	}
	if results[0].Channel != "log" {
		t.Errorf("Channel = %q, want 'log'", results[0].Channel)
	}
}

func TestDispatch_NoRoutes(t *testing.T) {
	d := &Dispatcher{
		config: &EscalationConfig{
			DecisionRoutes: map[string][]string{},
		},
	}

	payload := &DecisionPayload{
		Type: "decision_point",
		ID:   "test-123",
	}

	results := d.Dispatch(payload, "default")

	// When no routes are configured, falls back to "log" channel
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	// Log channel always succeeds
	if !results[0].Success {
		t.Error("Expected success for log fallback")
	}
	if results[0].Channel != "log" {
		t.Errorf("Expected log fallback, got %q", results[0].Channel)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"abc", 3, "abc"}, // Exactly at maxLen, no truncation needed
		{"abcd", 3, "..."}, // Over maxLen but maxLen=3 means only "..."
		{"a", 10, "a"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestDispatchDecisionNotification(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	dp := &types.DecisionPoint{
		IssueID: "gt-abc123.decision-deploy",
		Prompt:  "Deploy now?",
		Options: `[{"id":"yes","label":"Yes"}]`,
	}

	// This will use default "log" channel since no config exists
	results, err := DispatchDecisionNotification(tempDir, dp, nil, "")
	if err != nil {
		t.Fatalf("DispatchDecisionNotification failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected at least one result")
	}
}
