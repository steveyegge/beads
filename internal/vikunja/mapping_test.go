// internal/vikunja/mapping_test.go
package vikunja

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestPriorityToBeads(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		vikunjaPriority int
		want            int
	}{
		{0, 4}, // Unset -> Backlog
		{1, 3}, // Low -> Low
		{2, 2}, // Medium -> Medium
		{3, 1}, // High -> High
		{4, 0}, // Urgent -> Critical
		{5, 2}, // Unknown -> Medium (default)
	}

	for _, tt := range tests {
		got := PriorityToBeads(tt.vikunjaPriority, config)
		if got != tt.want {
			t.Errorf("PriorityToBeads(%d) = %d, want %d", tt.vikunjaPriority, got, tt.want)
		}
	}
}

func TestPriorityToVikunja(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		beadsPriority int
		want          int
	}{
		{0, 4}, // Critical -> Urgent
		{1, 3}, // High -> High
		{2, 2}, // Medium -> Medium
		{3, 1}, // Low -> Low
		{4, 0}, // Backlog -> Unset
	}

	for _, tt := range tests {
		got := PriorityToVikunja(tt.beadsPriority, config)
		if got != tt.want {
			t.Errorf("PriorityToVikunja(%d) = %d, want %d", tt.beadsPriority, got, tt.want)
		}
	}
}

func TestStatusToBeads(t *testing.T) {
	tests := []struct {
		done bool
		want types.Status
	}{
		{false, types.StatusOpen},
		{true, types.StatusClosed},
	}

	for _, tt := range tests {
		got := StatusToBeads(tt.done)
		if got != tt.want {
			t.Errorf("StatusToBeads(%v) = %v, want %v", tt.done, got, tt.want)
		}
	}
}

func TestStatusToVikunja(t *testing.T) {
	tests := []struct {
		status types.Status
		want   bool
	}{
		{types.StatusOpen, false},
		{types.StatusInProgress, false},
		{types.StatusClosed, true},
	}

	for _, tt := range tests {
		got := StatusToVikunja(tt.status)
		if got != tt.want {
			t.Errorf("StatusToVikunja(%v) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestRelationToBeadsDep(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		vikunjaRelation string
		want            string
	}{
		{"blocking", "blocks"},
		{"blocked", "blocks"},
		{"subtask", "parent-child"},
		{"parenttask", "parent-child"},
		{"related", "related"},
		{"duplicateof", "duplicates"},
		{"duplicates", "duplicates"},
		{"precedes", "blocks"},
		{"follows", "blocks"},
		{"unknown", "related"}, // fallback
	}

	for _, tt := range tests {
		got := RelationToBeadsDep(tt.vikunjaRelation, config)
		if got != tt.want {
			t.Errorf("RelationToBeadsDep(%q) = %q, want %q", tt.vikunjaRelation, got, tt.want)
		}
	}
}

func TestLabelToIssueType(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		name   string
		labels []Label
		want   types.IssueType
	}{
		{
			name:   "bug label",
			labels: []Label{{Title: "bug"}},
			want:   types.TypeBug,
		},
		{
			name:   "defect label maps to bug",
			labels: []Label{{Title: "defect"}},
			want:   types.TypeBug,
		},
		{
			name:   "feature label",
			labels: []Label{{Title: "feature"}},
			want:   types.TypeFeature,
		},
		{
			name:   "enhancement maps to feature",
			labels: []Label{{Title: "enhancement"}},
			want:   types.TypeFeature,
		},
		{
			name:   "epic label",
			labels: []Label{{Title: "epic"}},
			want:   types.TypeEpic,
		},
		{
			name:   "chore label",
			labels: []Label{{Title: "chore"}},
			want:   types.TypeChore,
		},
		{
			name:   "task label",
			labels: []Label{{Title: "task"}},
			want:   types.TypeTask,
		},
		{
			name:   "empty labels defaults to task",
			labels: []Label{},
			want:   types.TypeTask,
		},
		{
			name:   "nil labels defaults to task",
			labels: nil,
			want:   types.TypeTask,
		},
		{
			name:   "unknown label defaults to task",
			labels: []Label{{Title: "random"}},
			want:   types.TypeTask,
		},
		{
			name:   "first matching label wins",
			labels: []Label{{Title: "random"}, {Title: "bug"}, {Title: "feature"}},
			want:   types.TypeBug,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LabelToIssueType(tt.labels, config)
			if got != tt.want {
				t.Errorf("LabelToIssueType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTaskToBeads(t *testing.T) {
	config := DefaultMappingConfig()

	vikunjaTask := &Task{
		ID:          123,
		Title:       "Test Task",
		Description: "Test description",
		Done:        false,
		Priority:    3, // High
		ProjectID:   1,
		Identifier:  "PROJ-123",
		Labels:      []Label{{Title: "bug"}},
		Created:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Updated:     time.Date(2026, 1, 16, 12, 0, 0, 0, time.UTC),
		Assignees:   []User{{Username: "john", Email: "john@example.com"}},
	}

	result := TaskToBeads(vikunjaTask, "https://vikunja.example.com", config)
	issue := result.Issue.(*types.Issue)

	if issue.Title != "Test Task" {
		t.Errorf("Title = %q, want %q", issue.Title, "Test Task")
	}
	if issue.Description != "Test description" {
		t.Errorf("Description = %q, want %q", issue.Description, "Test description")
	}
	if issue.Priority != 1 { // High in beads
		t.Errorf("Priority = %d, want 1", issue.Priority)
	}
	if issue.Status != types.StatusOpen {
		t.Errorf("Status = %v, want %v", issue.Status, types.StatusOpen)
	}
	if issue.IssueType != types.TypeBug {
		t.Errorf("IssueType = %v, want %v", issue.IssueType, types.TypeBug)
	}
	if issue.ExternalRef == nil || *issue.ExternalRef != "https://vikunja.example.com/tasks/123" {
		t.Errorf("ExternalRef = %v, want https://vikunja.example.com/tasks/123", issue.ExternalRef)
	}
	if issue.Assignee != "john@example.com" {
		t.Errorf("Assignee = %q, want %q", issue.Assignee, "john@example.com")
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug]", issue.Labels)
	}
}

func TestTaskToBeadsWithClosedTask(t *testing.T) {
	config := DefaultMappingConfig()

	doneAt := time.Date(2026, 1, 17, 14, 0, 0, 0, time.UTC)
	vikunjaTask := &Task{
		ID:          456,
		Title:       "Closed Task",
		Description: "Already done",
		Done:        true,
		DoneAt:      doneAt,
		Priority:    2,
		Created:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Updated:     time.Date(2026, 1, 17, 14, 0, 0, 0, time.UTC),
	}

	result := TaskToBeads(vikunjaTask, "https://vikunja.example.com", config)
	issue := result.Issue.(*types.Issue)

	if issue.Status != types.StatusClosed {
		t.Errorf("Status = %v, want %v", issue.Status, types.StatusClosed)
	}
	if issue.ClosedAt == nil {
		t.Error("ClosedAt should be set for done task")
	} else if !issue.ClosedAt.Equal(doneAt) {
		t.Errorf("ClosedAt = %v, want %v", issue.ClosedAt, doneAt)
	}
}

func TestTaskToBeadsWithRelations(t *testing.T) {
	config := DefaultMappingConfig()

	vikunjaTask := &Task{
		ID:       100,
		Title:    "Parent Task",
		Priority: 2,
		Created:  time.Now(),
		Updated:  time.Now(),
		RelatedTasks: map[string][]Task{
			"subtask":  {{ID: 101, Title: "Subtask 1"}},
			"blocking": {{ID: 102, Title: "Blocked Task"}},
			"related":  {{ID: 103, Title: "Related Task"}},
		},
	}

	result := TaskToBeads(vikunjaTask, "https://vikunja.example.com", config)

	// Should have 3 dependencies extracted
	if len(result.Dependencies) != 3 {
		t.Errorf("Got %d dependencies, want 3", len(result.Dependencies))
	}

	// Check that dependencies were extracted with correct types
	depTypes := make(map[string]bool)
	for _, dep := range result.Dependencies {
		depTypes[dep.Type] = true
	}

	if !depTypes["parent-child"] {
		t.Error("Expected parent-child dependency from subtask relation")
	}
	if !depTypes["blocks"] {
		t.Error("Expected blocks dependency from blocking relation")
	}
	if !depTypes["related"] {
		t.Error("Expected related dependency from related relation")
	}
}

func TestTaskToBeadsWithUsernameAssignee(t *testing.T) {
	config := DefaultMappingConfig()

	vikunjaTask := &Task{
		ID:        789,
		Title:     "Task with username assignee",
		Priority:  2,
		Created:   time.Now(),
		Updated:   time.Now(),
		Assignees: []User{{Username: "johndoe", Email: ""}},
	}

	result := TaskToBeads(vikunjaTask, "https://vikunja.example.com", config)
	issue := result.Issue.(*types.Issue)

	if issue.Assignee != "johndoe" {
		t.Errorf("Assignee = %q, want %q (username fallback)", issue.Assignee, "johndoe")
	}
}

func TestBeadsToVikunjaTask(t *testing.T) {
	config := DefaultMappingConfig()

	now := time.Now()
	issue := &types.Issue{
		ID:          "test-123",
		Title:       "Beads Issue",
		Description: "Test description from beads",
		Priority:    1, // High
		Status:      types.StatusOpen,
		IssueType:   types.TypeBug,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	taskMap := BeadsToVikunjaTask(issue, config)

	if taskMap["title"] != "Beads Issue" {
		t.Errorf("title = %q, want %q", taskMap["title"], "Beads Issue")
	}
	if taskMap["description"] != "Test description from beads" {
		t.Errorf("description = %q, want %q", taskMap["description"], "Test description from beads")
	}
	if taskMap["priority"] != 3 { // High maps to 3 in Vikunja
		t.Errorf("priority = %d, want 3", taskMap["priority"])
	}
	if taskMap["done"] != false {
		t.Errorf("done = %v, want false", taskMap["done"])
	}
}

func TestBeadsToVikunjaTaskClosed(t *testing.T) {
	config := DefaultMappingConfig()

	now := time.Now()
	issue := &types.Issue{
		ID:        "test-456",
		Title:     "Closed Issue",
		Status:    types.StatusClosed,
		Priority:  2,
		CreatedAt: now,
		UpdatedAt: now,
		ClosedAt:  &now,
	}

	taskMap := BeadsToVikunjaTask(issue, config)

	if taskMap["done"] != true {
		t.Errorf("done = %v, want true for closed status", taskMap["done"])
	}
}

func TestBuildVikunjaExternalRef(t *testing.T) {
	tests := []struct {
		baseURL string
		taskID  int64
		want    string
	}{
		{
			baseURL: "https://vikunja.example.com/api/v1",
			taskID:  123,
			want:    "https://vikunja.example.com/tasks/123",
		},
		{
			baseURL: "https://vikunja.example.com",
			taskID:  456,
			want:    "https://vikunja.example.com/tasks/456",
		},
	}

	for _, tt := range tests {
		got := BuildVikunjaExternalRef(tt.baseURL, tt.taskID)
		if got != tt.want {
			t.Errorf("BuildVikunjaExternalRef(%q, %d) = %q, want %q",
				tt.baseURL, tt.taskID, got, tt.want)
		}
	}
}

func TestExtractVikunjaTaskID(t *testing.T) {
	tests := []struct {
		externalRef string
		wantID      int64
		wantOK      bool
	}{
		{
			externalRef: "https://vikunja.example.com/tasks/123",
			wantID:      123,
			wantOK:      true,
		},
		{
			externalRef: "https://other.com/tasks/456",
			wantID:      456,
			wantOK:      true,
		},
		{
			externalRef: "https://vikunja.example.com/projects/1",
			wantID:      0,
			wantOK:      false,
		},
		{
			externalRef: "invalid-url",
			wantID:      0,
			wantOK:      false,
		},
		{
			externalRef: "https://vikunja.example.com/tasks/notanumber",
			wantID:      0,
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		gotID, gotOK := ExtractVikunjaTaskID(tt.externalRef)
		if gotID != tt.wantID || gotOK != tt.wantOK {
			t.Errorf("ExtractVikunjaTaskID(%q) = (%d, %v), want (%d, %v)",
				tt.externalRef, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}

func TestIsVikunjaExternalRef(t *testing.T) {
	tests := []struct {
		externalRef string
		baseURL     string
		want        bool
	}{
		{
			externalRef: "https://vikunja.example.com/tasks/123",
			baseURL:     "https://vikunja.example.com/api/v1",
			want:        true,
		},
		{
			externalRef: "https://vikunja.example.com/tasks/456",
			baseURL:     "https://vikunja.example.com",
			want:        true,
		},
		{
			externalRef: "https://other.com/tasks/123",
			baseURL:     "https://vikunja.example.com/api/v1",
			want:        false,
		},
		{
			externalRef: "https://vikunja.example.com/projects/1",
			baseURL:     "https://vikunja.example.com/api/v1",
			want:        false,
		},
	}

	for _, tt := range tests {
		got := IsVikunjaExternalRef(tt.externalRef, tt.baseURL)
		if got != tt.want {
			t.Errorf("IsVikunjaExternalRef(%q, %q) = %v, want %v",
				tt.externalRef, tt.baseURL, got, tt.want)
		}
	}
}

func TestNormalizeIssueForVikunjaHash(t *testing.T) {
	now := time.Now()
	closedAt := now.Add(-time.Hour)
	externalRef := "https://vikunja.example.com/tasks/123"

	original := &types.Issue{
		ID:          "test-123",
		Title:       "Test Issue",
		Description: "Description",
		Priority:    2,
		Status:      types.StatusClosed,
		ExternalRef: &externalRef,
		CreatedAt:   now,
		UpdatedAt:   now,
		ClosedAt:    &closedAt,
	}

	normalized := NormalizeIssueForVikunjaHash(original)

	// Original should be unchanged
	if original.ID != "test-123" {
		t.Error("Original issue was modified")
	}
	if original.ExternalRef == nil {
		t.Error("Original ExternalRef was modified")
	}

	// Normalized should have cleared sync-specific fields
	if normalized.ID != "" {
		t.Errorf("Normalized ID = %q, want empty", normalized.ID)
	}
	if normalized.ExternalRef != nil {
		t.Errorf("Normalized ExternalRef = %v, want nil", normalized.ExternalRef)
	}
	if !normalized.CreatedAt.IsZero() {
		t.Errorf("Normalized CreatedAt = %v, want zero", normalized.CreatedAt)
	}
	if !normalized.UpdatedAt.IsZero() {
		t.Errorf("Normalized UpdatedAt = %v, want zero", normalized.UpdatedAt)
	}
	if normalized.ClosedAt != nil {
		t.Errorf("Normalized ClosedAt = %v, want nil", normalized.ClosedAt)
	}

	// Content fields should be preserved
	if normalized.Title != "Test Issue" {
		t.Errorf("Normalized Title = %q, want %q", normalized.Title, "Test Issue")
	}
	if normalized.Description != "Description" {
		t.Errorf("Normalized Description = %q, want %q", normalized.Description, "Description")
	}
	if normalized.Priority != 2 {
		t.Errorf("Normalized Priority = %d, want 2", normalized.Priority)
	}
	if normalized.Status != types.StatusClosed {
		t.Errorf("Normalized Status = %v, want %v", normalized.Status, types.StatusClosed)
	}
}

func TestLoadMappingConfig(t *testing.T) {
	// Test with nil loader - should return defaults
	config := LoadMappingConfig(nil)

	if config.PriorityMap["0"] != 4 {
		t.Errorf("Default PriorityMap[0] = %d, want 4", config.PriorityMap["0"])
	}
	if config.LabelTypeMap["bug"] != "bug" {
		t.Errorf("Default LabelTypeMap[bug] = %q, want %q", config.LabelTypeMap["bug"], "bug")
	}
	if config.RelationMap["blocking"] != "blocks" {
		t.Errorf("Default RelationMap[blocking] = %q, want %q", config.RelationMap["blocking"], "blocks")
	}
}

// mockConfigLoader implements ConfigLoader for testing
type mockConfigLoader struct {
	config map[string]string
}

func (m *mockConfigLoader) GetAllConfig() (map[string]string, error) {
	return m.config, nil
}

func TestLoadMappingConfigWithCustomValues(t *testing.T) {
	loader := &mockConfigLoader{
		config: map[string]string{
			"vikunja.priority_map.0":    "3",          // Override: unset -> low instead of backlog
			"vikunja.label_type_map.pr": "feature",    // Add: pr label maps to feature
			"vikunja.relation_map.copy": "discovered", // Add: copy relation maps to discovered
		},
	}

	config := LoadMappingConfig(loader)

	// Custom values should be applied
	if config.PriorityMap["0"] != 3 {
		t.Errorf("Custom PriorityMap[0] = %d, want 3", config.PriorityMap["0"])
	}
	if config.LabelTypeMap["pr"] != "feature" {
		t.Errorf("Custom LabelTypeMap[pr] = %q, want %q", config.LabelTypeMap["pr"], "feature")
	}
	if config.RelationMap["copy"] != "discovered" {
		t.Errorf("Custom RelationMap[copy] = %q, want %q", config.RelationMap["copy"], "discovered")
	}

	// Default values should still be present
	if config.PriorityMap["4"] != 0 {
		t.Errorf("Default PriorityMap[4] = %d, want 0", config.PriorityMap["4"])
	}
	if config.LabelTypeMap["bug"] != "bug" {
		t.Errorf("Default LabelTypeMap[bug] = %q, want %q", config.LabelTypeMap["bug"], "bug")
	}
}

func TestDefaultMappingConfig(t *testing.T) {
	config := DefaultMappingConfig()

	// Verify priority mappings
	expectedPriorities := map[string]int{
		"0": 4, // Unset -> Backlog
		"1": 3, // Low -> Low
		"2": 2, // Medium -> Medium
		"3": 1, // High -> High
		"4": 0, // Urgent -> Critical
	}
	for vikunja, beads := range expectedPriorities {
		if config.PriorityMap[vikunja] != beads {
			t.Errorf("PriorityMap[%s] = %d, want %d", vikunja, config.PriorityMap[vikunja], beads)
		}
	}

	// Verify label type mappings
	expectedLabelTypes := map[string]string{
		"bug":         "bug",
		"defect":      "bug",
		"feature":     "feature",
		"enhancement": "feature",
		"epic":        "epic",
		"chore":       "chore",
		"task":        "task",
	}
	for label, issueType := range expectedLabelTypes {
		if config.LabelTypeMap[label] != issueType {
			t.Errorf("LabelTypeMap[%s] = %q, want %q", label, config.LabelTypeMap[label], issueType)
		}
	}

	// Verify relation mappings
	expectedRelations := map[string]string{
		"blocking":    "blocks",
		"blocked":     "blocks",
		"subtask":     "parent-child",
		"parenttask":  "parent-child",
		"related":     "related",
		"duplicateof": "duplicates",
		"duplicates":  "duplicates",
		"precedes":    "blocks",
		"follows":     "blocks",
		"copiedfrom":  "related",
		"copiedto":    "related",
	}
	for relation, depType := range expectedRelations {
		if config.RelationMap[relation] != depType {
			t.Errorf("RelationMap[%s] = %q, want %q", relation, config.RelationMap[relation], depType)
		}
	}
}
