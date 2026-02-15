package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestDispatcher_NilSafe(t *testing.T) {
	var d *Dispatcher
	// Should not panic
	d.Fire("create", &types.Issue{ID: "test-abc"})
	d.FireComment(&types.Issue{ID: "test-abc"}, "author", "body")
}

func TestDispatcher_EmptyHooks(t *testing.T) {
	d := NewDispatcher(nil)
	if d != nil {
		t.Error("NewDispatcher(nil) should return nil")
	}

	d = NewDispatcher([]EventHook{})
	if d != nil {
		t.Error("NewDispatcher([]) should return nil")
	}
}

func TestDispatcher_Fire_PostWrite(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.txt")

	hooks := []EventHook{
		{
			Event:   "post-write",
			Command: "echo ${BEAD_ID} ${BEAD_EVENT} > " + outputFile,
			Async:   false,
		},
	}

	d := NewDispatcher(hooks)
	issue := &types.Issue{ID: "aegis-abc", Title: "Test"}

	d.Fire("create", issue)

	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	expected := "aegis-abc create\n"
	if string(output) != expected {
		t.Errorf("output = %q, want %q", string(output), expected)
	}
}

func TestDispatcher_Fire_EventFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.txt")

	hooks := []EventHook{
		{
			Event:   "post-close",
			Command: "echo closed > " + outputFile,
			Async:   false,
		},
	}

	d := NewDispatcher(hooks)
	issue := &types.Issue{ID: "test-abc", Title: "Test"}

	// Should NOT fire for create event
	d.Fire("create", issue)

	if _, err := os.Stat(outputFile); err == nil {
		t.Error("hook fired for wrong event")
	}

	// Should fire for close event
	d.Fire("close", issue)

	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(output) != "closed\n" {
		t.Errorf("output = %q", string(output))
	}
}

func TestDispatcher_Fire_PriorityFilter(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.txt")

	hooks := []EventHook{
		{
			Event:   "post-create",
			Command: "echo alert > " + outputFile,
			Filter:  "priority:P0,P1",
			Async:   false,
		},
	}

	d := NewDispatcher(hooks)

	// P2 should not trigger
	d.Fire("create", &types.Issue{ID: "test-abc", Priority: 2})
	if _, err := os.Stat(outputFile); err == nil {
		t.Error("hook fired for P2 (should only fire for P0,P1)")
	}

	// P1 should trigger
	d.Fire("create", &types.Issue{ID: "test-abc", Priority: 1})
	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(output) != "alert\n" {
		t.Errorf("output = %q", string(output))
	}
}

func TestDispatcher_Fire_Async(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.txt")

	hooks := []EventHook{
		{
			Event:   "post-write",
			Command: "echo async > " + outputFile,
			Async:   true,
		},
	}

	d := NewDispatcher(hooks)
	d.Fire("create", &types.Issue{ID: "test-abc"})

	// Wait for async completion
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(outputFile); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("async hook output not found: %v", err)
	}
	if string(output) != "async\n" {
		t.Errorf("output = %q", string(output))
	}
}

func TestDispatcher_FireComment(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.txt")

	hooks := []EventHook{
		{
			Event:   "post-comment",
			Command: "echo ${COMMENT_AUTHOR} ${BEAD_ID} > " + outputFile,
			Async:   false,
		},
	}

	d := NewDispatcher(hooks)
	issue := &types.Issue{ID: "test-abc"}

	d.FireComment(issue, "alice", "great work")

	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(output) != "alice test-abc\n" {
		t.Errorf("output = %q", string(output))
	}
}

func TestMatchesFilter(t *testing.T) {
	issue := &types.Issue{
		ID:        "aegis-abc",
		Priority:  1,
		IssueType: "bug",
		Status:    "open",
	}

	tests := []struct {
		filter   string
		expected bool
	}{
		{"priority:P0,P1", true},
		{"priority:P2,P3", false},
		{"type:bug", true},
		{"type:task", false},
		{"status:open", true},
		{"status:closed", false},
		{"rig:aegis", true},
		{"rig:other", false},
		{"", true},           // empty filter
		{"malformed", true},  // no colon — pass through
		{"unknown:val", true}, // unknown field — pass through
	}

	for _, tt := range tests {
		t.Run(tt.filter, func(t *testing.T) {
			result := matchesFilter(tt.filter, issue)
			if result != tt.expected {
				t.Errorf("matchesFilter(%q) = %v, want %v", tt.filter, result, tt.expected)
			}
		})
	}
}
