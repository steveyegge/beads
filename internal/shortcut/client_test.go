package shortcut

import (
	"net/http"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-api-token", "test-team-id")

	if client.APIToken != "test-api-token" {
		t.Errorf("APIToken = %q, want %q", client.APIToken, "test-api-token")
	}
	if client.TeamID != "test-team-id" {
		t.Errorf("TeamID = %q, want %q", client.TeamID, "test-team-id")
	}
	if client.Endpoint != DefaultAPIEndpoint {
		t.Errorf("Endpoint = %q, want %q", client.Endpoint, DefaultAPIEndpoint)
	}
	if client.HTTPClient == nil {
		t.Error("HTTPClient should not be nil")
	}
}

func TestWithEndpoint(t *testing.T) {
	client := NewClient("token", "team")
	customEndpoint := "https://custom.shortcut.com/api/v3"

	newClient := client.WithEndpoint(customEndpoint)

	if newClient.Endpoint != customEndpoint {
		t.Errorf("Endpoint = %q, want %q", newClient.Endpoint, customEndpoint)
	}
	// Original should be unchanged
	if client.Endpoint != DefaultAPIEndpoint {
		t.Errorf("Original endpoint changed: %q", client.Endpoint)
	}
	// Other fields preserved
	if newClient.APIToken != "token" {
		t.Errorf("APIToken not preserved: %q", newClient.APIToken)
	}
}

func TestWithHTTPClient(t *testing.T) {
	client := NewClient("token", "team")
	customHTTPClient := &http.Client{Timeout: 60 * time.Second}

	newClient := client.WithHTTPClient(customHTTPClient)

	if newClient.HTTPClient != customHTTPClient {
		t.Error("HTTPClient not set correctly")
	}
	// Other fields preserved
	if newClient.APIToken != "token" {
		t.Errorf("APIToken not preserved: %q", newClient.APIToken)
	}
	if newClient.Endpoint != DefaultAPIEndpoint {
		t.Errorf("Endpoint not preserved: %q", newClient.Endpoint)
	}
}

func TestIsShortcutExternalRef(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"https://app.shortcut.com/org/story/12345", true},
		{"https://app.shortcut.com/org/story/12345/some-title", true},
		{"https://linear.app/team/issue/PROJ-123", false},
		{"https://github.com/org/repo/issues/123", false},
		{"https://jira.example.com/browse/PROJ-123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := IsShortcutExternalRef(tt.ref)
			if got != tt.want {
				t.Errorf("IsShortcutExternalRef(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestCanonicalizeShortcutExternalRef(t *testing.T) {
	tests := []struct {
		name        string
		externalRef string
		want        string
		ok          bool
	}{
		{
			name:        "url with slug",
			externalRef: "https://app.shortcut.com/org/story/12345/some-title-here",
			want:        "https://app.shortcut.com/org/story/12345",
			ok:          true,
		},
		{
			name:        "canonical url",
			externalRef: "https://app.shortcut.com/org/story/12345",
			want:        "https://app.shortcut.com/org/story/12345",
			ok:          true,
		},
		{
			name:        "not shortcut",
			externalRef: "https://example.com/story/12345",
			want:        "",
			ok:          false,
		},
		{
			name:        "empty string",
			externalRef: "",
			want:        "",
			ok:          false,
		},
		{
			name:        "linear url",
			externalRef: "https://linear.app/team/issue/PROJ-123",
			want:        "",
			ok:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CanonicalizeShortcutExternalRef(tt.externalRef)
			if ok != tt.ok {
				t.Fatalf("ok=%v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractStoryID(t *testing.T) {
	tests := []struct {
		name        string
		externalRef string
		want        int64
		ok          bool
	}{
		{
			name:        "standard URL",
			externalRef: "https://app.shortcut.com/org/story/12345",
			want:        12345,
			ok:          true,
		},
		{
			name:        "URL with slug",
			externalRef: "https://app.shortcut.com/org/story/67890/some-title-here",
			want:        67890,
			ok:          true,
		},
		{
			name:        "URL with trailing slash",
			externalRef: "https://app.shortcut.com/org/story/11111/",
			want:        11111,
			ok:          true,
		},
		{
			name:        "non-shortcut URL",
			externalRef: "https://linear.app/team/issue/PROJ-123",
			want:        0,
			ok:          false,
		},
		{
			name:        "empty string",
			externalRef: "",
			want:        0,
			ok:          false,
		},
		{
			name:        "malformed URL",
			externalRef: "not-a-url",
			want:        0,
			ok:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractStoryID(tt.externalRef)
			if ok != tt.ok {
				t.Errorf("ExtractStoryID(%q) ok = %v, want %v", tt.externalRef, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("ExtractStoryID(%q) = %d, want %d", tt.externalRef, got, tt.want)
			}
		})
	}
}

func TestBuildStoryURL(t *testing.T) {
	tests := []struct {
		org     string
		storyID int64
		want    string
	}{
		{"myorg", 12345, "https://app.shortcut.com/myorg/story/12345"},
		{"another-org", 1, "https://app.shortcut.com/another-org/story/1"},
	}

	for _, tt := range tests {
		t.Run(tt.org, func(t *testing.T) {
			got := BuildStoryURL(tt.org, tt.storyID)
			if got != tt.want {
				t.Errorf("BuildStoryURL(%q, %d) = %q, want %q", tt.org, tt.storyID, got, tt.want)
			}
		})
	}
}

func TestStateCacheFindStateForBeadsStatus(t *testing.T) {
	cache := &StateCache{
		StatesByID: map[int64]WorkflowState{
			1: {ID: 1, Name: "Todo", Type: "unstarted"},
			2: {ID: 2, Name: "In Progress", Type: "started"},
			3: {ID: 3, Name: "Done", Type: "done"},
		},
		OpenStateID: 1,
		DoneStateID: 3,
	}

	tests := []struct {
		status string
		want   int64
	}{
		{"open", 1},
		{"blocked", 1},
		{"deferred", 1},
		{"in_progress", 2},
		{"hooked", 2},
		{"pinned", 2},
		{"closed", 3},
		{"unknown", 1}, // Default to unstarted
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := cache.FindStateForBeadsStatus(tt.status)
			if got != tt.want {
				t.Errorf("FindStateForBeadsStatus(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}
