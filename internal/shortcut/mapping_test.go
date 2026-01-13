package shortcut

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestDefaultMappingConfig(t *testing.T) {
	config := DefaultMappingConfig()

	// Check priority mappings
	if config.PriorityMap["none"] != 4 {
		t.Errorf("PriorityMap[none] = %d, want 4", config.PriorityMap["none"])
	}
	if config.PriorityMap["urgent"] != 0 {
		t.Errorf("PriorityMap[urgent] = %d, want 0", config.PriorityMap["urgent"])
	}
	if config.PriorityMap["high"] != 1 {
		t.Errorf("PriorityMap[high] = %d, want 1", config.PriorityMap["high"])
	}
	if config.PriorityMap["medium"] != 2 {
		t.Errorf("PriorityMap[medium] = %d, want 2", config.PriorityMap["medium"])
	}
	if config.PriorityMap["low"] != 3 {
		t.Errorf("PriorityMap[low] = %d, want 3", config.PriorityMap["low"])
	}

	// Check state mappings
	if config.StateMap["unstarted"] != "open" {
		t.Errorf("StateMap[unstarted] = %s, want open", config.StateMap["unstarted"])
	}
	if config.StateMap["started"] != "in_progress" {
		t.Errorf("StateMap[started] = %s, want in_progress", config.StateMap["started"])
	}
	if config.StateMap["done"] != "closed" {
		t.Errorf("StateMap[done] = %s, want closed", config.StateMap["done"])
	}

	// Check type mappings
	if config.TypeMap["feature"] != "feature" {
		t.Errorf("TypeMap[feature] = %s, want feature", config.TypeMap["feature"])
	}
	if config.TypeMap["bug"] != "bug" {
		t.Errorf("TypeMap[bug] = %s, want bug", config.TypeMap["bug"])
	}
	if config.TypeMap["chore"] != "task" {
		t.Errorf("TypeMap[chore] = %s, want task", config.TypeMap["chore"])
	}

	// Check relation mappings
	if config.RelationMap["blocks"] != "blocks" {
		t.Errorf("RelationMap[blocks] = %s, want blocks", config.RelationMap["blocks"])
	}
	if config.RelationMap["duplicates"] != "duplicates" {
		t.Errorf("RelationMap[duplicates] = %s, want duplicates", config.RelationMap["duplicates"])
	}
}

func TestPriorityToBeads(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		shortcutPriority string
		want             int
	}{
		{"", 4},        // No priority -> Backlog
		{"none", 4},    // None -> Backlog
		{"low", 3},     // Low -> Low
		{"medium", 2},  // Medium -> Medium
		{"high", 1},    // High -> High
		{"urgent", 0},  // Urgent -> Critical
		{"unknown", 2}, // Unknown -> Medium (default)
	}

	for _, tt := range tests {
		got := PriorityToBeads(tt.shortcutPriority, config)
		if got != tt.want {
			t.Errorf("PriorityToBeads(%q) = %d, want %d", tt.shortcutPriority, got, tt.want)
		}
	}
}

func TestPriorityToShortcut(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		beadsPriority int
		want          string
	}{
		{0, "urgent"}, // Critical -> Urgent
		{1, "high"},   // High -> High
		{2, "medium"}, // Medium -> Medium
		{3, "low"},    // Low -> Low
		{4, "medium"}, // Backlog -> Medium (default, as "none" and "" are excluded from inverse map)
		{5, "medium"}, // Unknown -> Medium (default)
	}

	for _, tt := range tests {
		got := PriorityToShortcut(tt.beadsPriority, config)
		if got != tt.want {
			t.Errorf("PriorityToShortcut(%d) = %q, want %q", tt.beadsPriority, got, tt.want)
		}
	}
}

func TestStateToBeadsStatus(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		state *WorkflowState
		want  types.Status
	}{
		{nil, types.StatusOpen},
		{&WorkflowState{Type: "unstarted", Name: "Backlog"}, types.StatusOpen},
		{&WorkflowState{Type: "started", Name: "In Progress"}, types.StatusInProgress},
		{&WorkflowState{Type: "done", Name: "Done"}, types.StatusClosed},
		{&WorkflowState{Type: "unknown", Name: "Unknown"}, types.StatusOpen}, // Default
	}

	for _, tt := range tests {
		got := StateToBeadsStatus(tt.state, config)
		stateName := "nil"
		if tt.state != nil {
			stateName = tt.state.Type
		}
		if got != tt.want {
			t.Errorf("StateToBeadsStatus(%s) = %v, want %v", stateName, got, tt.want)
		}
	}
}

func TestParseBeadsStatus(t *testing.T) {
	tests := []struct {
		input string
		want  types.Status
	}{
		{"open", types.StatusOpen},
		{"OPEN", types.StatusOpen},
		{"in_progress", types.StatusInProgress},
		{"in-progress", types.StatusInProgress},
		{"inprogress", types.StatusInProgress},
		{"blocked", types.StatusBlocked},
		{"closed", types.StatusClosed},
		{"CLOSED", types.StatusClosed},
		{"deferred", types.StatusDeferred},
		{"unknown", types.StatusOpen}, // Default
	}

	for _, tt := range tests {
		got := ParseBeadsStatus(tt.input)
		if got != tt.want {
			t.Errorf("ParseBeadsStatus(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestTypeToBeads(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		storyType string
		want      types.IssueType
	}{
		{"feature", types.TypeFeature},
		{"Feature", types.TypeFeature},
		{"bug", types.TypeBug},
		{"Bug", types.TypeBug},
		{"chore", types.TypeTask},
		{"Chore", types.TypeTask},
		{"unknown", types.TypeTask}, // Default
	}

	for _, tt := range tests {
		got := TypeToBeads(tt.storyType, config)
		if got != tt.want {
			t.Errorf("TypeToBeads(%q) = %v, want %v", tt.storyType, got, tt.want)
		}
	}
}

func TestTypeToShortcut(t *testing.T) {
	tests := []struct {
		issueType types.IssueType
		want      string
	}{
		{types.TypeBug, "bug"},
		{types.TypeFeature, "feature"},
		{types.TypeTask, "chore"},
		{types.TypeChore, "chore"},
		{types.TypeEpic, "feature"}, // Default to feature
	}

	for _, tt := range tests {
		got := TypeToShortcut(tt.issueType)
		if got != tt.want {
			t.Errorf("TypeToShortcut(%v) = %q, want %q", tt.issueType, got, tt.want)
		}
	}
}

func TestParseIssueType(t *testing.T) {
	tests := []struct {
		input string
		want  types.IssueType
	}{
		{"bug", types.TypeBug},
		{"BUG", types.TypeBug},
		{"feature", types.TypeFeature},
		{"task", types.TypeTask},
		{"epic", types.TypeEpic},
		{"chore", types.TypeChore},
		{"unknown", types.TypeTask}, // Default
	}

	for _, tt := range tests {
		got := ParseIssueType(tt.input)
		if got != tt.want {
			t.Errorf("ParseIssueType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestRelationToBeadsDep(t *testing.T) {
	config := DefaultMappingConfig()

	tests := []struct {
		verb string
		want string
	}{
		{"blocks", "blocks"},
		{"is blocked by", "blocks"},
		{"duplicates", "duplicates"},
		{"relates to", "related"},
		{"unknown", "related"}, // Default
	}

	for _, tt := range tests {
		got := RelationToBeadsDep(tt.verb, config)
		if got != tt.want {
			t.Errorf("RelationToBeadsDep(%q) = %q, want %q", tt.verb, got, tt.want)
		}
	}
}

func TestStoryToBeads(t *testing.T) {
	config := DefaultMappingConfig()
	stateCache := &StateCache{
		StatesByID: map[int64]WorkflowState{
			100: {ID: 100, Name: "In Progress", Type: "started"},
		},
	}

	story := &Story{
		ID:              12345,
		Name:            "Test Story",
		Description:     "Test description",
		AppURL:          "https://app.shortcut.com/org/story/12345/test-story",
		StoryType:       "bug",
		WorkflowStateID: 100,
		Priority:        "high",
		OwnerIDs:        []string{"user-uuid-123"},
		Labels:          []Label{{Name: "urgent"}, {Name: "backend"}},
		CreatedAt:       "2024-01-15T10:00:00Z",
		UpdatedAt:       "2024-01-16T12:00:00Z",
	}

	result := StoryToBeads(story, stateCache, config)
	issue := result.Issue.(*types.Issue)

	if issue.Title != "Test Story" {
		t.Errorf("Title = %q, want %q", issue.Title, "Test Story")
	}
	if issue.Description != "Test description" {
		t.Errorf("Description = %q, want %q", issue.Description, "Test description")
	}
	if issue.Priority != 1 { // High in beads
		t.Errorf("Priority = %d, want 1", issue.Priority)
	}
	if issue.Status != types.StatusInProgress {
		t.Errorf("Status = %v, want %v", issue.Status, types.StatusInProgress)
	}
	if issue.Assignee != "user-uuid-123" {
		t.Errorf("Assignee = %q, want %q", issue.Assignee, "user-uuid-123")
	}
	if issue.IssueType != types.TypeBug {
		t.Errorf("IssueType = %v, want %v", issue.IssueType, types.TypeBug)
	}
	if issue.ExternalRef == nil {
		t.Error("ExternalRef should not be nil")
	} else if *issue.ExternalRef != "https://app.shortcut.com/org/story/12345" {
		t.Errorf("ExternalRef = %q, want canonical URL", *issue.ExternalRef)
	}
	if len(issue.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(issue.Labels))
	}
}

func TestStoryToBeadsWithStoryLinks(t *testing.T) {
	config := DefaultMappingConfig()
	stateCache := &StateCache{
		StatesByID: map[int64]WorkflowState{
			100: {ID: 100, Name: "Todo", Type: "unstarted"},
		},
	}

	story := &Story{
		ID:              12345,
		Name:            "Story with links",
		Description:     "Description",
		AppURL:          "https://app.shortcut.com/org/story/12345",
		StoryType:       "feature",
		WorkflowStateID: 100,
		Priority:        "medium",
		StoryLinks: []StoryLink{
			{ID: 1, Verb: "blocks", SubjectID: 12345, ObjectID: 67890},
			{ID: 2, Verb: "is blocked by", SubjectID: 12345, ObjectID: 11111},
			{ID: 3, Verb: "relates to", SubjectID: 12345, ObjectID: 22222},
		},
		CreatedAt: "2024-01-15T10:00:00Z",
		UpdatedAt: "2024-01-16T12:00:00Z",
	}

	result := StoryToBeads(story, stateCache, config)

	if len(result.Dependencies) != 3 {
		t.Fatalf("Expected 3 dependencies, got %d", len(result.Dependencies))
	}

	// Check "blocks" dependency - this story blocks 67890
	dep0 := result.Dependencies[0]
	if dep0.Type != "blocks" {
		t.Errorf("Dependencies[0].Type = %q, want %q", dep0.Type, "blocks")
	}
	if dep0.FromStoryID != 67890 {
		t.Errorf("Dependencies[0].FromStoryID = %d, want %d", dep0.FromStoryID, 67890)
	}
	if dep0.ToStoryID != 12345 {
		t.Errorf("Dependencies[0].ToStoryID = %d, want %d", dep0.ToStoryID, 12345)
	}

	// Check "is blocked by" dependency - this story is blocked by 11111
	dep1 := result.Dependencies[1]
	if dep1.Type != "blocks" {
		t.Errorf("Dependencies[1].Type = %q, want %q", dep1.Type, "blocks")
	}
	if dep1.FromStoryID != 12345 {
		t.Errorf("Dependencies[1].FromStoryID = %d, want %d", dep1.FromStoryID, 12345)
	}
	if dep1.ToStoryID != 11111 {
		t.Errorf("Dependencies[1].ToStoryID = %d, want %d", dep1.ToStoryID, 11111)
	}

	// Check "relates to" dependency
	dep2 := result.Dependencies[2]
	if dep2.Type != "related" {
		t.Errorf("Dependencies[2].Type = %q, want %q", dep2.Type, "related")
	}
}

func TestBeadsToStoryParams(t *testing.T) {
	config := DefaultMappingConfig()
	stateCache := &StateCache{
		StatesByID: map[int64]WorkflowState{
			100: {ID: 100, Name: "Todo", Type: "unstarted"},
			200: {ID: 200, Name: "In Progress", Type: "started"},
		},
		OpenStateID: 100,
	}

	issue := &types.Issue{
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		IssueType:   types.TypeBug,
		Assignee:    "user-uuid",
		Labels:      []string{"urgent", "backend"},
	}

	params := BeadsToStoryParams(issue, stateCache, config)

	if params.Name != "Test Issue" {
		t.Errorf("Name = %q, want %q", params.Name, "Test Issue")
	}
	if params.Description != "Test description" {
		t.Errorf("Description = %q, want %q", params.Description, "Test description")
	}
	if params.StoryType != "bug" {
		t.Errorf("StoryType = %q, want %q", params.StoryType, "bug")
	}
	if params.WorkflowStateID != 100 {
		t.Errorf("WorkflowStateID = %d, want %d", params.WorkflowStateID, 100)
	}
	if len(params.OwnerIDs) != 1 || params.OwnerIDs[0] != "user-uuid" {
		t.Errorf("OwnerIDs = %v, want [user-uuid]", params.OwnerIDs)
	}
	if len(params.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(params.Labels))
	}
}

func TestBeadsToStoryUpdateParams(t *testing.T) {
	config := DefaultMappingConfig()
	stateCache := &StateCache{
		StatesByID: map[int64]WorkflowState{
			100: {ID: 100, Name: "Done", Type: "done"},
		},
		DoneStateID: 100,
	}

	issue := &types.Issue{
		Title:       "Updated Title",
		Description: "Updated description",
		Status:      types.StatusClosed,
		IssueType:   types.TypeFeature,
		Assignee:    "new-user",
	}

	params := BeadsToStoryUpdateParams(issue, stateCache, config)

	if params.Name == nil || *params.Name != "Updated Title" {
		t.Errorf("Name = %v, want %q", params.Name, "Updated Title")
	}
	if params.Description == nil || *params.Description != "Updated description" {
		t.Errorf("Description = %v, want %q", params.Description, "Updated description")
	}
	if params.StoryType == nil || *params.StoryType != "feature" {
		t.Errorf("StoryType = %v, want %q", params.StoryType, "feature")
	}
	if params.WorkflowStateID == nil || *params.WorkflowStateID != 100 {
		t.Errorf("WorkflowStateID = %v, want %d", params.WorkflowStateID, 100)
	}
}

func TestBuildShortcutToLocalUpdates(t *testing.T) {
	config := DefaultMappingConfig()
	stateCache := &StateCache{
		StatesByID: map[int64]WorkflowState{
			100: {ID: 100, Name: "Done", Type: "done"},
		},
	}

	completedAt := "2024-01-20T14:00:00Z"
	story := &Story{
		ID:              12345,
		Name:            "Updated Title",
		Description:     "Updated description",
		StoryType:       "feature",
		WorkflowStateID: 100,
		Priority:        "urgent",
		OwnerIDs:        []string{"owner-uuid"},
		Labels:          []Label{{Name: "label1"}, {Name: "label2"}},
		UpdatedAt:       "2024-01-20T15:00:00Z",
		CompletedAt:     &completedAt,
	}

	updates := BuildShortcutToLocalUpdates(story, stateCache, config)

	if updates["title"] != "Updated Title" {
		t.Errorf("title = %v, want %q", updates["title"], "Updated Title")
	}
	if updates["description"] != "Updated description" {
		t.Errorf("description = %v, want %q", updates["description"], "Updated description")
	}
	if updates["priority"] != 0 { // Urgent -> Critical
		t.Errorf("priority = %v, want 0", updates["priority"])
	}
	if updates["status"] != "closed" {
		t.Errorf("status = %v, want %q", updates["status"], "closed")
	}
	if updates["assignee"] != "owner-uuid" {
		t.Errorf("assignee = %v, want %q", updates["assignee"], "owner-uuid")
	}

	labels, ok := updates["labels"].([]string)
	if !ok || len(labels) != 2 {
		t.Errorf("labels = %v, want 2 labels", updates["labels"])
	}

	if updates["closed_at"] == nil {
		t.Error("closed_at should be set")
	}
}

func TestBuildShortcutToLocalUpdatesNoOwner(t *testing.T) {
	config := DefaultMappingConfig()
	stateCache := &StateCache{
		StatesByID: map[int64]WorkflowState{
			100: {ID: 100, Name: "Todo", Type: "unstarted"},
		},
	}

	story := &Story{
		ID:              12345,
		Name:            "No Owner",
		Description:     "Test",
		StoryType:       "chore",
		WorkflowStateID: 100,
		Priority:        "low",
		OwnerIDs:        nil,
		UpdatedAt:       "2024-01-20T15:00:00Z",
	}

	updates := BuildShortcutToLocalUpdates(story, stateCache, config)

	if updates["assignee"] != "" {
		t.Errorf("assignee = %v, want empty string", updates["assignee"])
	}
}

func TestGenerateIssueIDs(t *testing.T) {
	issues := []*types.Issue{
		{
			Title:       "First issue",
			Description: "Description 1",
			CreatedAt:   time.Now(),
		},
		{
			Title:       "Second issue",
			Description: "Description 2",
			CreatedAt:   time.Now().Add(-time.Hour),
		},
		{
			Title:       "Third issue",
			Description: "Description 3",
			CreatedAt:   time.Now().Add(-2 * time.Hour),
		},
	}

	err := GenerateIssueIDs(issues, "test", "shortcut-import", IDGenerationOptions{})
	if err != nil {
		t.Fatalf("GenerateIssueIDs failed: %v", err)
	}

	// Verify all issues have IDs
	for i, issue := range issues {
		if issue.ID == "" {
			t.Errorf("Issue %d has empty ID", i)
		}
		if !hasPrefix(issue.ID, "test-") {
			t.Errorf("Issue %d ID '%s' doesn't have prefix 'test-'", i, issue.ID)
		}
	}

	// Verify all IDs are unique
	seen := make(map[string]bool)
	for i, issue := range issues {
		if seen[issue.ID] {
			t.Errorf("Duplicate ID found: %s (issue %d)", issue.ID, i)
		}
		seen[issue.ID] = true
	}
}

func TestGenerateIssueIDsPreservesExisting(t *testing.T) {
	existingID := "test-existing"
	issues := []*types.Issue{
		{
			ID:          existingID,
			Title:       "Existing issue",
			Description: "Has an ID already",
			CreatedAt:   time.Now(),
		},
		{
			Title:       "New issue",
			Description: "Needs an ID",
			CreatedAt:   time.Now(),
		},
	}

	err := GenerateIssueIDs(issues, "test", "shortcut-import", IDGenerationOptions{})
	if err != nil {
		t.Fatalf("GenerateIssueIDs failed: %v", err)
	}

	// First issue should keep its original ID
	if issues[0].ID != existingID {
		t.Errorf("Existing ID was changed: got %s, want %s", issues[0].ID, existingID)
	}

	// Second issue should have a new ID
	if issues[1].ID == "" {
		t.Error("Second issue has empty ID")
	}
	if issues[1].ID == existingID {
		t.Error("Second issue has same ID as first (collision)")
	}
}

func TestGenerateIssueIDsNoDuplicates(t *testing.T) {
	// Create issues with identical content - should still get unique IDs
	now := time.Now()
	issues := []*types.Issue{
		{
			Title:       "Same title",
			Description: "Same description",
			CreatedAt:   now,
		},
		{
			Title:       "Same title",
			Description: "Same description",
			CreatedAt:   now,
		},
	}

	err := GenerateIssueIDs(issues, "bd", "shortcut-import", IDGenerationOptions{})
	if err != nil {
		t.Fatalf("GenerateIssueIDs failed: %v", err)
	}

	// Both should have IDs
	if issues[0].ID == "" || issues[1].ID == "" {
		t.Error("One or both issues have empty IDs")
	}

	// IDs should be different (nonce handles collision)
	if issues[0].ID == issues[1].ID {
		t.Errorf("Both issues have same ID: %s", issues[0].ID)
	}
}

// mockConfigLoader implements ConfigLoader for testing
type mockConfigLoader struct {
	config map[string]string
}

func (m *mockConfigLoader) GetAllConfig() (map[string]string, error) {
	return m.config, nil
}

func TestLoadMappingConfig(t *testing.T) {
	loader := &mockConfigLoader{
		config: map[string]string{
			"shortcut.priority_map.none":    "3",
			"shortcut.state_map.custom":     "in_progress",
			"shortcut.type_map.story":       "feature",
			"shortcut.relation_map.depends": "blocks",
			"shortcut.organization":         "myorg",
		},
	}

	config := LoadMappingConfig(loader)

	// Check custom priority mapping
	if config.PriorityMap["none"] != 3 {
		t.Errorf("PriorityMap[none] = %d, want 3", config.PriorityMap["none"])
	}

	// Check custom state mapping
	if config.StateMap["custom"] != "in_progress" {
		t.Errorf("StateMap[custom] = %s, want in_progress", config.StateMap["custom"])
	}

	// Check custom type mapping
	if config.TypeMap["story"] != "feature" {
		t.Errorf("TypeMap[story] = %s, want feature", config.TypeMap["story"])
	}

	// Check custom relation mapping
	if config.RelationMap["depends"] != "blocks" {
		t.Errorf("RelationMap[depends] = %s, want blocks", config.RelationMap["depends"])
	}

	// Check organization
	if config.Organization != "myorg" {
		t.Errorf("Organization = %s, want myorg", config.Organization)
	}

	// Check that defaults are preserved
	if config.StateMap["started"] != "in_progress" {
		t.Errorf("StateMap[started] = %s, want in_progress (default preserved)", config.StateMap["started"])
	}
}

func TestLoadMappingConfigNilLoader(t *testing.T) {
	config := LoadMappingConfig(nil)

	// Should return defaults
	if config.PriorityMap["urgent"] != 0 {
		t.Errorf("Expected default priority map with nil loader")
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
