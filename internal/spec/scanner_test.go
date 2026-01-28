package spec

import "testing"

func TestIsScannableSpecID(t *testing.T) {
	tests := []struct {
		name     string
		specID   string
		expected bool
	}{
		// Scannable (local file paths)
		{"relative path", "specs/login.md", true},
		{"nested path", "docs/specs/auth/login.md", true},
		{"simple filename", "README.md", true},

		// Not scannable
		{"empty string", "", false},
		{"URL http", "http://example.com/spec.md", false},
		{"URL https", "https://example.com/spec.md", false},
		{"absolute path", "/Users/foo/specs/login.md", false},
		{"SPEC ID uppercase", "SPEC-001", false},
		{"SPEC ID lowercase", "spec-001", false},
		{"REQ ID", "REQ-123", false},
		{"FEAT ID", "FEAT-456", false},
		{"US ID", "US-789", false},
		{"JIRA ID", "JIRA-ABC-123", false},
		{"jira lowercase", "jira-abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsScannableSpecID(tt.specID)
			if got != tt.expected {
				t.Errorf("IsScannableSpecID(%q) = %v, want %v", tt.specID, got, tt.expected)
			}
		})
	}
}
