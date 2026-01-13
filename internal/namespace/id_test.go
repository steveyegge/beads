package namespace

import (
	"testing"
)

func TestParseIssueID(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		contextProject    string
		contextBranch     string
		expected          IssueID
		expectError       bool
	}{
		// Hash only (uses context)
		{
			name:           "hash only with context",
			input:          "a3f2",
			contextProject: "beads",
			contextBranch:  "fix-auth",
			expected:       IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
		},
		{
			name:           "hash to main branch",
			input:          "a3f2",
			contextProject: "beads",
			contextBranch:  "",
			expected:       IssueID{Project: "beads", Branch: "main", Hash: "a3f2"},
		},

		// Branch-hash (uses context project)
		{
			name:           "branch-hash with context",
			input:          "fix-auth-a3f2",
			contextProject: "beads",
			contextBranch:  "irrelevant",
			expected:       IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
		},
		{
			name:           "main-hash",
			input:          "main-b7c9",
			contextProject: "beads",
			contextBranch:  "feature",
			expected:       IssueID{Project: "beads", Branch: "main", Hash: "b7c9"},
		},

		// Fully qualified
		{
			name:           "project:hash",
			input:          "beads:c4d8",
			contextProject: "ignored",
			contextBranch:  "ignored",
			expected:       IssueID{Project: "beads", Branch: "main", Hash: "c4d8"},
		},
		{
			name:           "project:branch-hash",
			input:          "beads:fix-auth-p5q6",
			contextProject: "ignored",
			contextBranch:  "ignored",
			expected:       IssueID{Project: "beads", Branch: "fix-auth", Hash: "p5q6"},
		},
		{
			name:           "other-project:hash",
			input:          "other:f6g7",
			contextProject: "ignored",
			contextBranch:  "ignored",
			expected:       IssueID{Project: "other", Branch: "main", Hash: "f6g7"},
		},

		// Multi-dash branch names
		{
			name:           "multi-dash branch",
			input:          "fix-auth-bug-a3f2",
			contextProject: "beads",
			contextBranch:  "ignored",
			expected:       IssueID{Project: "beads", Branch: "fix-auth-bug", Hash: "a3f2"},
		},
		{
			name:           "underscore in branch",
			input:          "fix_auth_bug-a3f2",
			contextProject: "beads",
			contextBranch:  "ignored",
			expected:       IssueID{Project: "beads", Branch: "fix_auth_bug", Hash: "a3f2"},
		},

		// Error cases
		{
			name:        "empty input",
			input:       "",
			expectError: true,
		},
		{
			name:        "invalid hash too short",
			input:       "abc",
			expectError: true,
		},
		{
			name:        "invalid hash too long",
			input:       "a3f2extra",
			expectError: true,
		},
		{
			name:        "invalid hash uppercase",
			input:       "A3F2",
			expectError: true,
		},
		{
			name:        "invalid hash special chars",
			input:       "a3f-2",
			expectError: true,
		},
		{
			name:        "empty project in qualified",
			input:       ":a3f2",
			expectError: true,
		},
		{
			name:        "empty hash in qualified",
			input:       "beads:",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIssueID(tt.input, tt.contextProject, tt.contextBranch)

			if (err != nil) != tt.expectError {
				t.Errorf("ParseIssueID() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if err == nil && got != tt.expected {
				t.Errorf("ParseIssueID() = %+v, expected %+v", got, tt.expected)
			}
		})
	}
}

func TestIssueIDString(t *testing.T) {
	tests := []struct {
		name     string
		id       IssueID
		expected string
	}{
		{
			name:     "main branch omitted",
			id:       IssueID{Project: "beads", Branch: "main", Hash: "a3f2"},
			expected: "beads:a3f2",
		},
		{
			name:     "feature branch shown",
			id:       IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
			expected: "beads:fix-auth-a3f2",
		},
		{
			name:     "empty branch treated as main",
			id:       IssueID{Project: "beads", Branch: "", Hash: "a3f2"},
			expected: "beads:a3f2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.id.String()
			if got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestIssueIDShort(t *testing.T) {
	tests := []struct {
		name     string
		id       IssueID
		expected string
	}{
		{
			name:     "main branch hash only",
			id:       IssueID{Project: "beads", Branch: "main", Hash: "a3f2"},
			expected: "a3f2",
		},
		{
			name:     "feature branch with hash",
			id:       IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
			expected: "fix-auth-a3f2",
		},
		{
			name:     "empty branch treated as main",
			id:       IssueID{Project: "beads", Branch: "", Hash: "a3f2"},
			expected: "a3f2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.id.Short()
			if got != tt.expected {
				t.Errorf("Short() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestIssueIDShortWithBranch(t *testing.T) {
	tests := []struct {
		name     string
		id       IssueID
		expected string
	}{
		{
			name:     "main branch explicit",
			id:       IssueID{Project: "beads", Branch: "main", Hash: "a3f2"},
			expected: "main-a3f2",
		},
		{
			name:     "feature branch",
			id:       IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
			expected: "fix-auth-a3f2",
		},
		{
			name:     "empty branch defaults to main",
			id:       IssueID{Project: "beads", Branch: "", Hash: "a3f2"},
			expected: "main-a3f2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.id.ShortWithBranch()
			if got != tt.expected {
				t.Errorf("ShortWithBranch() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestIssueIDValidate(t *testing.T) {
	tests := []struct {
		name      string
		id        IssueID
		expectErr bool
	}{
		{
			name:      "valid ID",
			id:        IssueID{Project: "beads", Branch: "main", Hash: "a3f2"},
			expectErr: false,
		},
		{
			name:      "valid with feature branch",
			id:        IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
			expectErr: false,
		},
		{
			name:      "empty project",
			id:        IssueID{Project: "", Branch: "main", Hash: "a3f2"},
			expectErr: true,
		},
		{
			name:      "empty branch",
			id:        IssueID{Project: "beads", Branch: "", Hash: "a3f2"},
			expectErr: true,
		},
		{
			name:      "empty hash",
			id:        IssueID{Project: "beads", Branch: "main", Hash: ""},
			expectErr: true,
		},
		{
			name:      "invalid hash",
			id:        IssueID{Project: "beads", Branch: "main", Hash: "INVALID"},
			expectErr: true,
		},
		{
			name:      "invalid project",
			id:        IssueID{Project: "my@project", Branch: "main", Hash: "a3f2"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.id.Validate()
			if (err != nil) != tt.expectErr {
				t.Errorf("Validate() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestResolutionRules(t *testing.T) {
	// Table from the proposal document
	tests := []struct {
		name      string
		input     string
		context   ResolutionContext
		expected  IssueID
		explained string
	}{
		{
			name:      "hash only",
			input:     "a3f2",
			context:   ResolutionContext{CurrentProject: "beads", CurrentBranch: "fix-auth"},
			expected:  IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
			explained: "Current context",
		},
		{
			name:      "explicit branch",
			input:     "fix-auth-a3f2",
			context:   ResolutionContext{CurrentProject: "beads", CurrentBranch: "main"},
			expected:  IssueID{Project: "beads", Branch: "fix-auth", Hash: "a3f2"},
			explained: "Explicit branch",
		},
		{
			name:      "different branch",
			input:     "main-b7c9",
			context:   ResolutionContext{CurrentProject: "beads", CurrentBranch: "fix-auth"},
			expected:  IssueID{Project: "beads", Branch: "main", Hash: "b7c9"},
			explained: "Different branch",
		},
		{
			name:      "explicit project default branch",
			input:     "beads:c4d8",
			context:   ResolutionContext{CurrentProject: "other", CurrentBranch: "feature"},
			expected:  IssueID{Project: "beads", Branch: "main", Hash: "c4d8"},
			explained: "Explicit project, default branch",
		},
		{
			name:      "different project",
			input:     "other:f6g7",
			context:   ResolutionContext{CurrentProject: "beads", CurrentBranch: "main"},
			expected:  IssueID{Project: "other", Branch: "main", Hash: "f6g7"},
			explained: "Different project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIssueID(tt.input, tt.context.CurrentProject, tt.context.CurrentBranch)
			if err != nil {
				t.Fatalf("ParseIssueID() error: %v", err)
			}

			if got != tt.expected {
				t.Errorf("[%s]\nGot:      %+v\nExpected: %+v", tt.explained, got, tt.expected)
			}
		})
	}
}
