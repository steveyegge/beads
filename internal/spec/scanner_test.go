package spec

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestExtractTitle(t *testing.T) {
	// Create temp dir for test files
	tmpDir, err := os.MkdirTemp("", "spec-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "h1 at start",
			content:  "# Login Feature\n\nSome content",
			expected: "Login Feature",
		},
		{
			name:     "h1 after blank lines",
			content:  "\n\n# Authentication\n\nContent here",
			expected: "Authentication",
		},
		{
			name:     "no h1",
			content:  "Just some text\nNo heading",
			expected: "",
		},
		{
			name:     "h2 not h1",
			content:  "## Subheading\n\nContent",
			expected: "",
		},
		{
			name:     "h1 with extra spaces",
			content:  "#   Trimmed Title  \n\nContent",
			expected: "Trimmed Title",
		},
		{
			name:     "empty file",
			content:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test file
			path := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			got := ExtractTitle(path)
			if got != tt.expected {
				t.Errorf("ExtractTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestScan(t *testing.T) {
	// Create temp dir structure
	tmpDir, err := os.MkdirTemp("", "spec-scan-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create specs directory
	specsDir := filepath.Join(tmpDir, "specs")
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create nested directory
	nestedDir := filepath.Join(specsDir, "auth")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create test files
	files := map[string]string{
		filepath.Join(specsDir, "login.md"):        "# Login Feature\n\nLogin spec",
		filepath.Join(specsDir, "signup.md"):       "# Signup Feature\n\nSignup spec",
		filepath.Join(nestedDir, "oauth.md"):       "# OAuth Integration\n\nOAuth spec",
		filepath.Join(specsDir, "notes.txt"):       "Not a markdown file",
		filepath.Join(specsDir, ".hidden.md"):      "# Hidden\n\nShould be included",
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Run scan
	specs, err := Scan(tmpDir, "specs")
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	// Should find 4 markdown files (including .hidden.md)
	if len(specs) != 4 {
		t.Errorf("Scan() found %d specs, want 4", len(specs))
	}

	// Verify spec IDs are relative paths
	specIDs := make(map[string]bool)
	for _, s := range specs {
		specIDs[s.SpecID] = true
	}

	expectedIDs := []string{
		"specs/login.md",
		"specs/signup.md",
		"specs/auth/oauth.md",
		"specs/.hidden.md",
	}

	for _, id := range expectedIDs {
		if !specIDs[id] {
			t.Errorf("Expected spec ID %q not found", id)
		}
	}

	// Verify titles extracted
	for _, s := range specs {
		if s.SpecID == "specs/login.md" && s.Title != "Login Feature" {
			t.Errorf("Expected title 'Login Feature', got %q", s.Title)
		}
	}
}
