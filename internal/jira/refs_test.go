package jira

import (
	"testing"
)

func TestIsJiraExternalRef(t *testing.T) {
	tests := []struct {
		name        string
		externalRef string
		jiraURL     string
		want        bool
	}{
		{
			name:        "valid Jira Cloud URL",
			externalRef: "https://company.atlassian.net/browse/PROJ-123",
			jiraURL:     "https://company.atlassian.net",
			want:        true,
		},
		{
			name:        "valid Jira Cloud URL with trailing slash in config",
			externalRef: "https://company.atlassian.net/browse/PROJ-123",
			jiraURL:     "https://company.atlassian.net/",
			want:        true,
		},
		{
			name:        "valid Jira Server URL",
			externalRef: "https://jira.company.com/browse/PROJ-456",
			jiraURL:     "https://jira.company.com",
			want:        true,
		},
		{
			name:        "mismatched Jira host",
			externalRef: "https://other.atlassian.net/browse/PROJ-123",
			jiraURL:     "https://company.atlassian.net",
			want:        false,
		},
		{
			name:        "GitHub issue URL",
			externalRef: "https://github.com/org/repo/issues/123",
			jiraURL:     "https://company.atlassian.net",
			want:        false,
		},
		{
			name:        "empty external_ref",
			externalRef: "",
			jiraURL:     "https://company.atlassian.net",
			want:        false,
		},
		{
			name:        "no jiraURL configured - valid pattern",
			externalRef: "https://any.atlassian.net/browse/PROJ-123",
			jiraURL:     "",
			want:        true,
		},
		{
			name:        "no jiraURL configured - invalid pattern",
			externalRef: "https://github.com/org/repo/issues/123",
			jiraURL:     "",
			want:        false,
		},
		{
			name:        "browse in path but not Jira format",
			externalRef: "https://example.com/browse/docs/page",
			jiraURL:     "",
			want:        true, // Contains /browse/, so matches pattern
		},
		{
			name:        "browse in path with jiraURL check",
			externalRef: "https://example.com/browse/docs/page",
			jiraURL:     "https://company.atlassian.net",
			want:        false, // Host doesn't match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsJiraExternalRef(tt.externalRef, tt.jiraURL)
			if got != tt.want {
				t.Errorf("IsJiraExternalRef(%q, %q) = %v, want %v",
					tt.externalRef, tt.jiraURL, got, tt.want)
			}
		})
	}
}

func TestExtractJiraKey(t *testing.T) {
	tests := []struct {
		name        string
		externalRef string
		want        string
	}{
		{
			name:        "standard Jira Cloud URL",
			externalRef: "https://company.atlassian.net/browse/PROJ-123",
			want:        "PROJ-123",
		},
		{
			name:        "Jira Server URL",
			externalRef: "https://jira.company.com/browse/ISSUE-456",
			want:        "ISSUE-456",
		},
		{
			name:        "URL with trailing path",
			externalRef: "https://company.atlassian.net/browse/ABC-789/some/path",
			want:        "ABC-789/some/path",
		},
		{
			name:        "no browse pattern",
			externalRef: "https://github.com/org/repo/issues/123",
			want:        "",
		},
		{
			name:        "empty string",
			externalRef: "",
			want:        "",
		},
		{
			name:        "only browse",
			externalRef: "https://example.com/browse/",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractJiraKey(tt.externalRef)
			if got != tt.want {
				t.Errorf("ExtractJiraKey(%q) = %q, want %q", tt.externalRef, got, tt.want)
			}
		})
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		wantErr   bool
		wantYear  int
	}{
		{
			name:      "standard Jira Cloud format with milliseconds",
			timestamp: "2024-01-15T10:30:00.000+0000",
			wantErr:   false,
			wantYear:  2024,
		},
		{
			name:      "Jira format with Z suffix",
			timestamp: "2024-01-15T10:30:00.000Z",
			wantErr:   false,
			wantYear:  2024,
		},
		{
			name:      "without milliseconds",
			timestamp: "2024-01-15T10:30:00+0000",
			wantErr:   false,
			wantYear:  2024,
		},
		{
			name:      "RFC3339 format",
			timestamp: "2024-01-15T10:30:00Z",
			wantErr:   false,
			wantYear:  2024,
		},
		{
			name:      "empty string",
			timestamp: "",
			wantErr:   true,
		},
		{
			name:      "invalid format",
			timestamp: "not-a-timestamp",
			wantErr:   true,
		},
		{
			name:      "with negative timezone offset",
			timestamp: "2024-06-15T10:30:00.000-0500",
			wantErr:   false,
			wantYear:  2024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimestamp(tt.timestamp)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTimestamp(%q) error = %v, wantErr %v", tt.timestamp, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Year() != tt.wantYear {
				t.Errorf("ParseTimestamp(%q) year = %d, want %d", tt.timestamp, got.Year(), tt.wantYear)
			}
		})
	}
}
