package runbook

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestRunbookToIssue(t *testing.T) {
	rb := &RunbookContent{
		Name:     "my-runbook",
		Format:   "hcl",
		Content:  `job "build" {}`,
		Jobs:     []string{"build"},
		Commands: []string{"plan"},
	}

	issue, labels, err := RunbookToIssue(rb, "od-")
	if err != nil {
		t.Fatalf("RunbookToIssue() error: %v", err)
	}

	if issue.ID != "od-runbook-my-runbook" {
		t.Errorf("ID = %q, want %q", issue.ID, "od-runbook-my-runbook")
	}
	if issue.Title != "my-runbook" {
		t.Errorf("Title = %q, want %q", issue.Title, "my-runbook")
	}
	if issue.IssueType != types.TypeRunbook {
		t.Errorf("IssueType = %q, want %q", issue.IssueType, types.TypeRunbook)
	}
	if !issue.IsTemplate {
		t.Error("IsTemplate = false, want true")
	}
	if len(issue.Metadata) == 0 {
		t.Error("Metadata is empty")
	}

	// Check labels
	labelSet := make(map[string]bool)
	for _, l := range labels {
		labelSet[l] = true
	}
	if !labelSet["format:hcl"] {
		t.Error("missing label format:hcl")
	}
	if !labelSet["job:build"] {
		t.Error("missing label job:build")
	}
	if !labelSet["cmd:plan"] {
		t.Error("missing label cmd:plan")
	}
}

func TestRunbookToIssueNil(t *testing.T) {
	_, _, err := RunbookToIssue(nil, "")
	if err == nil {
		t.Error("expected error for nil runbook")
	}
}

func TestRunbookToIssueEmptyName(t *testing.T) {
	rb := &RunbookContent{Format: "hcl", Content: "test"}
	_, _, err := RunbookToIssue(rb, "")
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestIssueToRunbook(t *testing.T) {
	rb := &RunbookContent{
		Name:    "test-runbook",
		Format:  "hcl",
		Content: `command "deploy" { run = "echo deploy" }`,
	}

	issue, _, err := RunbookToIssue(rb, "od-")
	if err != nil {
		t.Fatalf("RunbookToIssue() error: %v", err)
	}

	// Roundtrip
	got, err := IssueToRunbook(issue)
	if err != nil {
		t.Fatalf("IssueToRunbook() error: %v", err)
	}

	if got.Name != rb.Name {
		t.Errorf("Name = %q, want %q", got.Name, rb.Name)
	}
	if got.Content != rb.Content {
		t.Errorf("Content mismatch")
	}
	if got.Format != rb.Format {
		t.Errorf("Format = %q, want %q", got.Format, rb.Format)
	}
	if got.Source != "bead:od-runbook-test-runbook" {
		t.Errorf("Source = %q, want bead prefix", got.Source)
	}
}

func TestIssueToRunbookWrongType(t *testing.T) {
	issue := &types.Issue{IssueType: types.TypeTask}
	_, err := IssueToRunbook(issue)
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestParseRunbookFile(t *testing.T) {
	content := `
command "plan" {
  args = "<name>"
  run  = { job = "plan" }
}

command "deploy" {
  run = { job = "deploy" }
}

job "plan" {
  vars = ["name"]
  step "build" {
    run = { agent = "claude" }
  }
}

job "deploy" {
  step "push" {
    run = "git push"
  }
}

worker "bug" {
  source  = { queue = "bugs" }
  handler = { job = "bug" }
}

cron "nightly" {
  schedule = "0 0 * * *"
  run      = { job = "backup" }
}

queue "bugs" {
  type = "external"
}
`
	rb := ParseRunbookFile("test", content, "hcl")

	if len(rb.Jobs) != 2 {
		t.Errorf("Jobs count = %d, want 2, got %v", len(rb.Jobs), rb.Jobs)
	}
	if len(rb.Commands) != 2 {
		t.Errorf("Commands count = %d, want 2, got %v", len(rb.Commands), rb.Commands)
	}
	if len(rb.Workers) != 1 {
		t.Errorf("Workers count = %d, want 1", len(rb.Workers))
	}
	if len(rb.Crons) != 1 {
		t.Errorf("Crons count = %d, want 1", len(rb.Crons))
	}
	if len(rb.Queues) != 1 {
		t.Errorf("Queues count = %d, want 1", len(rb.Queues))
	}
}

func TestExtractImports(t *testing.T) {
	content := `
import "oj/wok" {
  const "prefix" { value = "oj" }
  const "check"  { value = "make check" }
}

import "oj/git" {
  const "check" { value = "make check" }
}
`
	imports := ExtractImports(content)
	if len(imports) != 2 {
		t.Errorf("ExtractImports count = %d, want 2, got %v", len(imports), imports)
	}
	if len(imports) >= 2 {
		if imports[0] != "oj/wok" {
			t.Errorf("imports[0] = %q, want %q", imports[0], "oj/wok")
		}
		if imports[1] != "oj/git" {
			t.Errorf("imports[1] = %q, want %q", imports[1], "oj/git")
		}
	}
}

func TestExtractImportsNone(t *testing.T) {
	content := `job "build" { run = "make" }`
	imports := ExtractImports(content)
	if len(imports) != 0 {
		t.Errorf("ExtractImports count = %d, want 0", len(imports))
	}
}

func TestExtractConsts(t *testing.T) {
	content := `
const "prefix" {}
const "check"  { default = "true" }
const "submit" { default = "" }

command "fix" {
  args = "<description>"
}
`
	consts := ExtractConsts(content)
	if len(consts) != 3 {
		t.Errorf("ExtractConsts count = %d, want 3, got %v", len(consts), consts)
	}
	if len(consts) >= 3 {
		if consts[0] != "prefix" {
			t.Errorf("consts[0] = %q, want %q", consts[0], "prefix")
		}
		if consts[1] != "check" {
			t.Errorf("consts[1] = %q, want %q", consts[1], "check")
		}
		if consts[2] != "submit" {
			t.Errorf("consts[2] = %q, want %q", consts[2], "submit")
		}
	}
}

func TestNameToSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-runbook", "my-runbook"},
		{"My Runbook", "my-runbook"},
		{"test_runbook.hcl", "test-runbook-hcl"},
		{"UPPER", "upper"},
		{"a--b", "a-b"},
	}
	for _, tt := range tests {
		got := nameToSlug(tt.input)
		if got != tt.want {
			t.Errorf("nameToSlug(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
