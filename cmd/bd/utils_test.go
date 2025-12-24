package main

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

func TestPluralize(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{10, "s"},
		{100, "s"},
		{-1, "s"}, // Edge case
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("count=%d", tt.count), func(t *testing.T) {
			result := pluralize(tt.count)
			if result != tt.expected {
				t.Errorf("pluralize(%d) = %q, want %q", tt.count, result, tt.expected)
			}
		})
	}
}

func TestFormatTimeAgo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "just now",
			time:     now.Add(-30 * time.Second),
			expected: "just now",
		},
		{
			name:     "1 min ago",
			time:     now.Add(-1 * time.Minute),
			expected: "1 min ago",
		},
		{
			name:     "multiple minutes ago",
			time:     now.Add(-5 * time.Minute),
			expected: "5 mins ago",
		},
		{
			name:     "1 hour ago",
			time:     now.Add(-1 * time.Hour),
			expected: "1 hour ago",
		},
		{
			name:     "multiple hours ago",
			time:     now.Add(-3 * time.Hour),
			expected: "3 hours ago",
		},
		{
			name:     "1 day ago",
			time:     now.Add(-24 * time.Hour),
			expected: "1 day ago",
		},
		{
			name:     "multiple days ago",
			time:     now.Add(-3 * 24 * time.Hour),
			expected: "3 days ago",
		},
		{
			name:     "more than a week ago",
			time:     now.Add(-10 * 24 * time.Hour),
			expected: now.Add(-10 * 24 * time.Hour).Format("2006-01-02"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeAgo(tt.time)
			if result != tt.expected {
				t.Errorf("formatTimeAgo() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPrintEvent(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	event := rpc.MutationEvent{
		Type:      rpc.MutationCreate,
		IssueID:   "bd-test123",
		Timestamp: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
	}

	printEvent(event)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Check that output contains expected elements
	if len(output) == 0 {
		t.Error("printEvent produced no output")
	}
	if !containsSubstring(output, "bd-test123") {
		t.Errorf("printEvent output missing issue ID, got: %s", output)
	}
	if !containsSubstring(output, "created") {
		t.Errorf("printEvent output missing 'created' message, got: %s", output)
	}
}

func TestShowCleanupDeprecationHint(t *testing.T) {
	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	showCleanupDeprecationHint()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Check that output contains expected elements
	if len(output) == 0 {
		t.Error("showCleanupDeprecationHint produced no output")
	}
	if !containsSubstring(output, "doctor --fix") {
		t.Errorf("showCleanupDeprecationHint output missing 'doctor --fix', got: %s", output)
	}
}

// containsSubstring checks if haystack contains needle
func containsSubstring(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Note: TestExtractPrefix is already defined in helpers_test.go

func TestPinIndicator(t *testing.T) {
	tests := []struct {
		name     string
		pinned   bool
		expected string
	}{
		{
			name:     "pinned issue",
			pinned:   true,
			expected: "📌 ",
		},
		{
			name:     "unpinned issue",
			pinned:   false,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &types.Issue{Pinned: tt.pinned}
			result := pinIndicator(issue)
			if result != tt.expected {
				t.Errorf("pinIndicator() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSortIssues(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	twoDaysAgo := now.Add(-48 * time.Hour)

	closedAt := now
	closedYesterday := yesterday

	baseIssues := func() []*types.Issue {
		return []*types.Issue{
			{ID: "bd-2", Title: "Beta", Priority: 2, CreatedAt: yesterday, UpdatedAt: yesterday, Status: "open"},
			{ID: "bd-1", Title: "Alpha", Priority: 1, CreatedAt: now, UpdatedAt: now, Status: "closed", ClosedAt: &closedAt},
			{ID: "bd-3", Title: "Gamma", Priority: 3, CreatedAt: twoDaysAgo, UpdatedAt: twoDaysAgo, Status: "in_progress", ClosedAt: &closedYesterday},
		}
	}

	t.Run("sort by priority ascending", func(t *testing.T) {
		issues := baseIssues()
		sortIssues(issues, "priority", false)
		if issues[0].Priority != 1 {
			t.Errorf("expected priority 1 first, got %d", issues[0].Priority)
		}
		if issues[1].Priority != 2 {
			t.Errorf("expected priority 2 second, got %d", issues[1].Priority)
		}
		if issues[2].Priority != 3 {
			t.Errorf("expected priority 3 third, got %d", issues[2].Priority)
		}
	})

	t.Run("sort by priority descending", func(t *testing.T) {
		issues := baseIssues()
		sortIssues(issues, "priority", true)
		if issues[0].Priority != 3 {
			t.Errorf("expected priority 3 first, got %d", issues[0].Priority)
		}
	})

	t.Run("sort by title", func(t *testing.T) {
		issues := baseIssues()
		sortIssues(issues, "title", false)
		if issues[0].Title != "Alpha" {
			t.Errorf("expected 'Alpha' first, got %q", issues[0].Title)
		}
	})

	t.Run("sort by ID", func(t *testing.T) {
		issues := baseIssues()
		sortIssues(issues, "id", false)
		if issues[0].ID != "bd-1" {
			t.Errorf("expected 'bd-1' first, got %q", issues[0].ID)
		}
	})

	t.Run("sort by status", func(t *testing.T) {
		issues := baseIssues()
		sortIssues(issues, "status", false)
		// closed < in_progress < open alphabetically
		if issues[0].Status != "closed" {
			t.Errorf("expected 'closed' first, got %q", issues[0].Status)
		}
	})

	t.Run("empty sortBy does nothing", func(t *testing.T) {
		issues := baseIssues()
		origFirst := issues[0].ID
		sortIssues(issues, "", false)
		if issues[0].ID != origFirst {
			t.Error("expected no sorting when sortBy is empty")
		}
	})

	t.Run("unknown sortBy does nothing", func(t *testing.T) {
		issues := baseIssues()
		origFirst := issues[0].ID
		sortIssues(issues, "unknown_field", false)
		if issues[0].ID != origFirst {
			t.Error("expected no sorting when sortBy is unknown")
		}
	})
}

func TestFormatHookWarnings(t *testing.T) {
	t.Run("no warnings when all installed and current", func(t *testing.T) {
		statuses := []HookStatus{
			{Name: "pre-commit", Installed: true, Outdated: false},
			{Name: "post-merge", Installed: true, Outdated: false},
		}
		result := FormatHookWarnings(statuses)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("warning when hooks missing", func(t *testing.T) {
		statuses := []HookStatus{
			{Name: "pre-commit", Installed: false, Outdated: false},
			{Name: "post-merge", Installed: false, Outdated: false},
		}
		result := FormatHookWarnings(statuses)
		if !containsSubstring(result, "2 missing") {
			t.Errorf("expected '2 missing' in output, got %q", result)
		}
		if !containsSubstring(result, "bd hooks install") {
			t.Errorf("expected 'bd hooks install' in output, got %q", result)
		}
	})

	t.Run("warning when hooks outdated", func(t *testing.T) {
		statuses := []HookStatus{
			{Name: "pre-commit", Installed: true, Outdated: true},
			{Name: "post-merge", Installed: true, Outdated: true},
		}
		result := FormatHookWarnings(statuses)
		if !containsSubstring(result, "2 hooks") {
			t.Errorf("expected '2 hooks' in output, got %q", result)
		}
		if !containsSubstring(result, "outdated") {
			t.Errorf("expected 'outdated' in output, got %q", result)
		}
	})

	t.Run("both missing and outdated", func(t *testing.T) {
		statuses := []HookStatus{
			{Name: "pre-commit", Installed: false, Outdated: false},
			{Name: "post-merge", Installed: true, Outdated: true},
		}
		result := FormatHookWarnings(statuses)
		if !containsSubstring(result, "1 missing") {
			t.Errorf("expected '1 missing' in output, got %q", result)
		}
		if !containsSubstring(result, "1 hooks") {
			t.Errorf("expected '1 hooks' in output, got %q", result)
		}
	})

	t.Run("empty statuses", func(t *testing.T) {
		statuses := []HookStatus{}
		result := FormatHookWarnings(statuses)
		if result != "" {
			t.Errorf("expected empty string for empty statuses, got %q", result)
		}
	})
}

func TestGetContributorsSorted(t *testing.T) {
	contributors := getContributorsSorted()

	// Should have at least some contributors
	if len(contributors) == 0 {
		t.Error("expected non-empty contributors list")
	}

	// First contributor should be the one with most commits (Steve Yegge)
	if contributors[0] != "Steve Yegge" {
		t.Errorf("expected 'Steve Yegge' first, got %q", contributors[0])
	}
}

func TestExtractIDSuffix(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected string
	}{
		{
			name:     "hierarchical ID with dot",
			id:       "bd-xyz.1",
			expected: "1",
		},
		{
			name:     "nested hierarchical ID",
			id:       "bd-abc.step1.sub",
			expected: "sub",
		},
		{
			name:     "prefix-hash ID",
			id:       "patrol-abc123",
			expected: "abc123",
		},
		{
			name:     "simple ID",
			id:       "bd-123",
			expected: "123",
		},
		{
			name:     "no separators",
			id:       "standalone",
			expected: "standalone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIDSuffix(tt.id)
			if result != tt.expected {
				t.Errorf("extractIDSuffix(%q) = %q, want %q", tt.id, result, tt.expected)
			}
		})
	}
}

// Note: TestGetRelativeID is already defined in mol_test.go

func TestIsRebaseInProgress(t *testing.T) {
	// Create a temp directory to simulate a git repo
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Create .git directory
	if err := os.MkdirAll(".git", 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	t.Run("no rebase in progress", func(t *testing.T) {
		if isRebaseInProgress() {
			t.Error("expected false when no rebase markers exist")
		}
	})

	t.Run("rebase-merge in progress", func(t *testing.T) {
		if err := os.MkdirAll(".git/rebase-merge", 0755); err != nil {
			t.Fatalf("failed to create rebase-merge dir: %v", err)
		}
		defer os.RemoveAll(".git/rebase-merge")

		if !isRebaseInProgress() {
			t.Error("expected true when .git/rebase-merge exists")
		}
	})

	t.Run("rebase-apply in progress", func(t *testing.T) {
		if err := os.MkdirAll(".git/rebase-apply", 0755); err != nil {
			t.Fatalf("failed to create rebase-apply dir: %v", err)
		}
		defer os.RemoveAll(".git/rebase-apply")

		if !isRebaseInProgress() {
			t.Error("expected true when .git/rebase-apply exists")
		}
	})
}

func TestHasBeadsJSONL(t *testing.T) {
	// Create a temp directory
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	t.Run("no JSONL files", func(t *testing.T) {
		if hasBeadsJSONL() {
			t.Error("expected false when no .beads directory exists")
		}
	})

	t.Run("with issues.jsonl", func(t *testing.T) {
		if err := os.MkdirAll(".beads", 0755); err != nil {
			t.Fatalf("failed to create .beads dir: %v", err)
		}
		if err := os.WriteFile(".beads/issues.jsonl", []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to create issues.jsonl: %v", err)
		}
		defer os.RemoveAll(".beads")

		if !hasBeadsJSONL() {
			t.Error("expected true when .beads/issues.jsonl exists")
		}
	})

	t.Run("with beads.jsonl", func(t *testing.T) {
		if err := os.MkdirAll(".beads", 0755); err != nil {
			t.Fatalf("failed to create .beads dir: %v", err)
		}
		if err := os.WriteFile(".beads/beads.jsonl", []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to create beads.jsonl: %v", err)
		}
		defer os.RemoveAll(".beads")

		if !hasBeadsJSONL() {
			t.Error("expected true when .beads/beads.jsonl exists")
		}
	})
}
