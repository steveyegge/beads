package linear

import (
	"net/http"
	"testing"
	"time"
)

func TestCanonicalizeLinearExternalRef(t *testing.T) {
	tests := []struct {
		name        string
		externalRef string
		want        string
		ok          bool
	}{
		{
			name:        "slugged url",
			externalRef: "https://linear.app/crown-dev/issue/BEA-93/updated-title-for-beads",
			want:        "https://linear.app/crown-dev/issue/BEA-93",
			ok:          true,
		},
		{
			name:        "canonical url",
			externalRef: "https://linear.app/crown-dev/issue/BEA-93",
			want:        "https://linear.app/crown-dev/issue/BEA-93",
			ok:          true,
		},
		{
			name:        "not linear",
			externalRef: "https://example.com/issues/BEA-93",
			want:        "",
			ok:          false,
		},
	}

	for _, tt := range tests {
		got, ok := CanonicalizeLinearExternalRef(tt.externalRef)
		if ok != tt.ok {
			t.Fatalf("%s: ok=%v, want %v", tt.name, ok, tt.ok)
		}
		if got != tt.want {
			t.Fatalf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("test-api-key", "test-team-id")

	if client.APIKey != "test-api-key" {
		t.Errorf("APIKey = %q, want %q", client.APIKey, "test-api-key")
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
	client := NewClient("key", "team")
	customEndpoint := "https://custom.linear.app/graphql"

	newClient := client.WithEndpoint(customEndpoint)

	if newClient.Endpoint != customEndpoint {
		t.Errorf("Endpoint = %q, want %q", newClient.Endpoint, customEndpoint)
	}
	// Original should be unchanged
	if client.Endpoint != DefaultAPIEndpoint {
		t.Errorf("Original endpoint changed: %q", client.Endpoint)
	}
	// Other fields preserved
	if newClient.APIKey != "key" {
		t.Errorf("APIKey not preserved: %q", newClient.APIKey)
	}
}

func TestWithHTTPClient(t *testing.T) {
	client := NewClient("key", "team")
	customHTTPClient := &http.Client{Timeout: 60 * time.Second}

	newClient := client.WithHTTPClient(customHTTPClient)

	if newClient.HTTPClient != customHTTPClient {
		t.Error("HTTPClient not set correctly")
	}
	// Other fields preserved
	if newClient.APIKey != "key" {
		t.Errorf("APIKey not preserved: %q", newClient.APIKey)
	}
	if newClient.Endpoint != DefaultAPIEndpoint {
		t.Errorf("Endpoint not preserved: %q", newClient.Endpoint)
	}
}

func TestExtractLinearIdentifier(t *testing.T) {
	tests := []struct {
		name        string
		externalRef string
		want        string
	}{
		{
			name:        "standard URL",
			externalRef: "https://linear.app/team/issue/PROJ-123",
			want:        "PROJ-123",
		},
		{
			name:        "URL with slug",
			externalRef: "https://linear.app/team/issue/PROJ-456/some-title-here",
			want:        "PROJ-456",
		},
		{
			name:        "URL with trailing slash",
			externalRef: "https://linear.app/team/issue/ABC-789/",
			want:        "ABC-789",
		},
		{
			name:        "non-linear URL",
			externalRef: "https://jira.example.com/browse/PROJ-123",
			want:        "",
		},
		{
			name:        "empty string",
			externalRef: "",
			want:        "",
		},
		{
			name:        "malformed URL",
			externalRef: "not-a-url",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLinearIdentifier(tt.externalRef)
			if got != tt.want {
				t.Errorf("ExtractLinearIdentifier(%q) = %q, want %q", tt.externalRef, got, tt.want)
			}
		})
	}
}

func TestIsLinearExternalRef(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"https://linear.app/team/issue/PROJ-123", true},
		{"https://linear.app/team/issue/PROJ-123/slug", true},
		{"https://jira.example.com/browse/PROJ-123", false},
		{"https://github.com/org/repo/issues/123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := IsLinearExternalRef(tt.ref)
			if got != tt.want {
				t.Errorf("IsLinearExternalRef(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

// Note: BuildStateCache and FindStateForBeadsStatus require API calls
// and would need mocking to test. Skipping unit tests for those.
