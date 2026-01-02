package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestFormatObsidianTask_StatusMapping(t *testing.T) {
	tests := []struct {
		name     string
		status   types.Status
		expected string
	}{
		{"open", types.StatusOpen, "- [ ]"},
		{"in_progress", types.StatusInProgress, "- [/]"},
		{"blocked", types.StatusBlocked, "- [c]"},
		{"closed", types.StatusClosed, "- [x]"},
		{"tombstone", types.StatusTombstone, "- [-]"},
		{"deferred", types.StatusDeferred, "- [-]"},
		{"pinned", types.StatusPinned, "- [n]"},
		{"hooked", types.StatusHooked, "- [/]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &types.Issue{
				ID:        "test-1",
				Title:     "Test Issue",
				Status:    tt.status,
				Priority:  2,
				CreatedAt: time.Now(),
			}
			result := formatObsidianTask(issue)
			if !strings.HasPrefix(result, tt.expected) {
				t.Errorf("expected prefix %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFormatObsidianTask_PriorityMapping(t *testing.T) {
	tests := []struct {
		priority int
		emoji    string
	}{
		{0, "🔺"},
		{1, "⏫"},
		{2, "🔼"},
		{3, "🔽"},
		{4, "⏬"},
	}

	for _, tt := range tests {
		t.Run(tt.emoji, func(t *testing.T) {
			issue := &types.Issue{
				ID:        "test-1",
				Title:     "Test Issue",
				Status:    types.StatusOpen,
				Priority:  tt.priority,
				CreatedAt: time.Now(),
			}
			result := formatObsidianTask(issue)
			if !strings.Contains(result, tt.emoji) {
				t.Errorf("expected emoji %q in result %q", tt.emoji, result)
			}
		})
	}
}

func TestFormatObsidianTask_TypeTags(t *testing.T) {
	tests := []struct {
		issueType types.IssueType
		tag       string
	}{
		{types.TypeBug, "#Bug"},
		{types.TypeFeature, "#Feature"},
		{types.TypeTask, "#Task"},
		{types.TypeEpic, "#Epic"},
		{types.TypeChore, "#Chore"},
	}

	for _, tt := range tests {
		t.Run(string(tt.issueType), func(t *testing.T) {
			issue := &types.Issue{
				ID:        "test-1",
				Title:     "Test Issue",
				Status:    types.StatusOpen,
				Priority:  2,
				IssueType: tt.issueType,
				CreatedAt: time.Now(),
			}
			result := formatObsidianTask(issue)
			if !strings.Contains(result, tt.tag) {
				t.Errorf("expected tag %q in result %q", tt.tag, result)
			}
		})
	}
}

func TestFormatObsidianTask_Labels(t *testing.T) {
	issue := &types.Issue{
		ID:        "test-1",
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		Labels:    []string{"urgent", "needs review"},
		CreatedAt: time.Now(),
	}
	result := formatObsidianTask(issue)

	if !strings.Contains(result, "#urgent") {
		t.Errorf("expected #urgent in result %q", result)
	}
	if !strings.Contains(result, "#needs-review") {
		t.Errorf("expected #needs-review (spaces replaced with dashes) in result %q", result)
	}
}

func TestFormatObsidianTask_Dates(t *testing.T) {
	created := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	closed := time.Date(2025, 1, 20, 15, 0, 0, 0, time.UTC)

	issue := &types.Issue{
		ID:        "test-1",
		Title:     "Test Issue",
		Status:    types.StatusClosed,
		Priority:  2,
		CreatedAt: created,
		ClosedAt:  &closed,
	}
	result := formatObsidianTask(issue)

	if !strings.Contains(result, "🛫 2025-01-15") {
		t.Errorf("expected start date 🛫 2025-01-15 in result %q", result)
	}
	if !strings.Contains(result, "✅ 2025-01-20") {
		t.Errorf("expected end date ✅ 2025-01-20 in result %q", result)
	}
}

func TestFormatObsidianTask_TaskID(t *testing.T) {
	issue := &types.Issue{
		ID:        "bd-123",
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		CreatedAt: time.Now(),
	}
	result := formatObsidianTask(issue)

	// Check for official Obsidian Tasks ID format: 🆔 id
	if !strings.Contains(result, "🆔 bd-123") {
		t.Errorf("expected '🆔 bd-123' in result %q", result)
	}
}

func TestFormatObsidianTask_Dependencies(t *testing.T) {
	issue := &types.Issue{
		ID:        "test-1",
		Title:     "Test Issue",
		Status:    types.StatusBlocked,
		Priority:  2,
		CreatedAt: time.Now(),
		Dependencies: []*types.Dependency{
			{IssueID: "test-1", DependsOnID: "test-2", Type: types.DepBlocks},
		},
	}
	result := formatObsidianTask(issue)

	// Check for official Obsidian Tasks "blocked by" format: ⛔ id
	if !strings.Contains(result, "⛔ test-2") {
		t.Errorf("expected '⛔ test-2' in result %q", result)
	}
}

func TestGroupIssuesByDate(t *testing.T) {
	date1 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	date2 := time.Date(2025, 1, 16, 10, 0, 0, 0, time.UTC)

	issues := []*types.Issue{
		{ID: "test-1", UpdatedAt: date1},
		{ID: "test-2", UpdatedAt: date1},
		{ID: "test-3", UpdatedAt: date2},
	}

	grouped := groupIssuesByDate(issues)

	if len(grouped) != 2 {
		t.Errorf("expected 2 date groups, got %d", len(grouped))
	}
	if len(grouped["2025-01-15"]) != 2 {
		t.Errorf("expected 2 issues for 2025-01-15, got %d", len(grouped["2025-01-15"]))
	}
	if len(grouped["2025-01-16"]) != 1 {
		t.Errorf("expected 1 issue for 2025-01-16, got %d", len(grouped["2025-01-16"]))
	}
}

func TestGroupIssuesByDate_UsesClosedAt(t *testing.T) {
	updated := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	closed := time.Date(2025, 1, 20, 10, 0, 0, 0, time.UTC)

	issues := []*types.Issue{
		{ID: "test-1", UpdatedAt: updated, ClosedAt: &closed},
	}

	grouped := groupIssuesByDate(issues)

	if _, ok := grouped["2025-01-20"]; !ok {
		t.Error("expected issue to be grouped by closed_at date (2025-01-20)")
	}
	if _, ok := grouped["2025-01-15"]; ok {
		t.Error("issue should not be grouped by updated_at when closed_at exists")
	}
}

func TestWriteObsidianExport(t *testing.T) {
	date1 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	date2 := time.Date(2025, 1, 16, 10, 0, 0, 0, time.UTC)

	issues := []*types.Issue{
		{
			ID:        "test-1",
			Title:     "First Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: date1,
			UpdatedAt: date1,
		},
		{
			ID:        "test-2",
			Title:     "Second Issue",
			Status:    types.StatusClosed,
			Priority:  1,
			IssueType: types.TypeBug,
			CreatedAt: date2,
			UpdatedAt: date2,
		},
	}

	var buf bytes.Buffer
	err := writeObsidianExport(&buf, issues)
	if err != nil {
		t.Fatalf("writeObsidianExport failed: %v", err)
	}

	output := buf.String()

	// Check header
	if !strings.HasPrefix(output, "# Changes Log\n") {
		t.Error("expected output to start with '# Changes Log'")
	}

	// Check date sections exist (most recent first)
	idx1 := strings.Index(output, "## 2025-01-16")
	idx2 := strings.Index(output, "## 2025-01-15")
	if idx1 == -1 || idx2 == -1 {
		t.Error("expected both date headers to exist")
	}
	if idx1 > idx2 {
		t.Error("expected 2025-01-16 (more recent) to appear before 2025-01-15")
	}

	// Check issues are present
	if !strings.Contains(output, "test-1") {
		t.Error("expected test-1 in output")
	}
	if !strings.Contains(output, "test-2") {
		t.Error("expected test-2 in output")
	}
}

func TestWriteObsidianExport_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := writeObsidianExport(&buf, []*types.Issue{})
	if err != nil {
		t.Fatalf("writeObsidianExport failed: %v", err)
	}

	output := buf.String()
	if !strings.HasPrefix(output, "# Changes Log\n") {
		t.Error("expected output to start with '# Changes Log' even when empty")
	}
}

func TestFormatObsidianTask_ParentChildDependency(t *testing.T) {
	issue := &types.Issue{
		ID:        "test-1.1",
		Title:     "Child Task",
		Status:    types.StatusOpen,
		Priority:  2,
		CreatedAt: time.Now(),
		Dependencies: []*types.Dependency{
			{IssueID: "test-1.1", DependsOnID: "test-1", Type: types.DepParentChild},
		},
	}
	result := formatObsidianTask(issue)

	// Parent-child deps should also show as ⛔ (blocked by parent)
	if !strings.Contains(result, "⛔ test-1") {
		t.Errorf("expected '⛔ test-1' for parent-child dep in result %q", result)
	}
}

func TestBuildParentChildMap(t *testing.T) {
	date := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	issues := []*types.Issue{
		{
			ID:        "parent-1",
			Title:     "Parent Epic",
			IssueType: types.TypeEpic,
			CreatedAt: date,
			UpdatedAt: date,
		},
		{
			ID:        "parent-1.1",
			Title:     "Child Task 1",
			IssueType: types.TypeTask,
			CreatedAt: date,
			UpdatedAt: date,
			Dependencies: []*types.Dependency{
				{IssueID: "parent-1.1", DependsOnID: "parent-1", Type: types.DepParentChild},
			},
		},
		{
			ID:        "parent-1.2",
			Title:     "Child Task 2",
			IssueType: types.TypeTask,
			CreatedAt: date,
			UpdatedAt: date,
			Dependencies: []*types.Dependency{
				{IssueID: "parent-1.2", DependsOnID: "parent-1", Type: types.DepParentChild},
			},
		},
	}

	parentToChildren, isChild := buildParentChildMap(issues)

	// Check parent has 2 children
	if len(parentToChildren["parent-1"]) != 2 {
		t.Errorf("expected 2 children for parent-1, got %d", len(parentToChildren["parent-1"]))
	}

	// Check children are marked
	if !isChild["parent-1.1"] {
		t.Error("expected parent-1.1 to be marked as child")
	}
	if !isChild["parent-1.2"] {
		t.Error("expected parent-1.2 to be marked as child")
	}

	// Parent should not be marked as child
	if isChild["parent-1"] {
		t.Error("parent-1 should not be marked as child")
	}
}

func TestWriteObsidianExport_ParentChildHierarchy(t *testing.T) {
	date := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	issues := []*types.Issue{
		{
			ID:        "epic-1",
			Title:     "Auth System",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeEpic,
			CreatedAt: date,
			UpdatedAt: date,
		},
		{
			ID:        "epic-1.1",
			Title:     "Login Page",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: date,
			UpdatedAt: date,
			Dependencies: []*types.Dependency{
				{IssueID: "epic-1.1", DependsOnID: "epic-1", Type: types.DepParentChild},
			},
		},
		{
			ID:        "epic-1.2",
			Title:     "Logout Button",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			CreatedAt: date,
			UpdatedAt: date,
			Dependencies: []*types.Dependency{
				{IssueID: "epic-1.2", DependsOnID: "epic-1", Type: types.DepParentChild},
			},
		},
	}

	var buf bytes.Buffer
	err := writeObsidianExport(&buf, issues)
	if err != nil {
		t.Fatalf("writeObsidianExport failed: %v", err)
	}

	output := buf.String()

	// Check parent is present (not indented)
	if !strings.Contains(output, "- [ ] Auth System") {
		t.Error("expected parent 'Auth System' in output")
	}

	// Check children are indented (2 spaces)
	if !strings.Contains(output, "  - [ ] Login Page") {
		t.Errorf("expected indented child 'Login Page' in output:\n%s", output)
	}
	if !strings.Contains(output, "  - [ ] Logout Button") {
		t.Errorf("expected indented child 'Logout Button' in output:\n%s", output)
	}

	// Children should have ⛔ dependency on parent
	if !strings.Contains(output, "⛔ epic-1") {
		t.Errorf("expected children to have '⛔ epic-1' dependency in output:\n%s", output)
	}
}
