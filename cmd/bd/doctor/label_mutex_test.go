package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/labelmutex"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// setupMutexTestRepo creates a temp repo with a SQLite DB, issues, labels, and config.yaml.
// Returns the tmpDir path. The store is closed before returning.
func setupMutexTestRepo(t *testing.T, configYAML string, issuesAndLabels []struct {
	issue  *types.Issue
	labels []string
}) string {
	t.Helper()

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, beads.CanonicalDatabaseName)
	ctx := context.Background()

	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	for _, item := range issuesAndLabels {
		if err := store.CreateIssue(ctx, item.issue, "test"); err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}
		for _, label := range item.labels {
			if err := store.AddLabel(ctx, item.issue.ID, label, "test"); err != nil {
				t.Fatalf("Failed to add label %q to %s: %v", label, item.issue.ID, err)
			}
		}
	}

	store.Close()

	if configYAML != "" {
		configPath := filepath.Join(beadsDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
			t.Fatalf("Failed to write config.yaml: %v", err)
		}
	}

	return tmpDir
}

func TestCheckLabelMutexInvariants_NoConfig(t *testing.T) {
	tmpDir := setupMutexTestRepo(t, "", nil)

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q", check.Status, StatusOK)
	}
	if check.Message != "No label mutex rules configured" {
		t.Errorf("Message = %q, want 'No label mutex rules configured'", check.Message)
	}
}

func TestCheckLabelMutexInvariants_NoViolations(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: false
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			issue:  &types.Issue{Title: "Task A", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"inbox"},
		},
		{
			issue:  &types.Issue{Title: "Task B", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"accepted"},
		},
		{
			issue:  &types.Issue{Title: "Task C", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{}, // no labels — OK for optional mutex
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		t.Logf("Detail: %s", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_ConflictDetected(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: false
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			issue:  &types.Issue{Title: "Conflicting", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"inbox", "accepted"},
		},
		{
			issue:  &types.Issue{Title: "Clean", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"inbox"},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
	if !strings.Contains(check.Message, "1 label mutex violation") {
		t.Errorf("Message = %q, want it to contain '1 label mutex violation'", check.Message)
	}
	if !strings.Contains(check.Detail, "triage conflict") {
		t.Errorf("Detail = %q, want it to contain 'triage conflict'", check.Detail)
	}
	if !strings.Contains(check.Detail, "inbox, accepted") {
		t.Errorf("Detail = %q, want it to contain 'inbox, accepted'", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_MissingRequired(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: work_status
        labels: [status:todo, status:doing, status:done]
        required: true
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			issue:  &types.Issue{Title: "Has status", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"status:todo"},
		},
		{
			issue:  &types.Issue{Title: "Missing status", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"unrelated"},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
	if !strings.Contains(check.Detail, "work_status missing") {
		t.Errorf("Detail = %q, want it to contain 'work_status missing'", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_ExcludesTombstones(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: true
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			// Tombstone has no labels — should NOT be flagged for missing required.
			issue:  &types.Issue{Title: "Deleted", Status: types.StatusTombstone, Priority: 2, IssueType: types.TypeTask},
			labels: []string{},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q (tombstones should be excluded)", check.Status, StatusOK)
		t.Logf("Detail: %s", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_ExcludesTemplates(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: true
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			issue:  &types.Issue{Title: "Template", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, IsTemplate: true},
			labels: []string{},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q (templates should be excluded)", check.Status, StatusOK)
		t.Logf("Detail: %s", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_ExcludesEphemeral(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: true
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			issue:  &types.Issue{Title: "Wisp", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: true},
			labels: []string{},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q (ephemeral should be excluded)", check.Status, StatusOK)
		t.Logf("Detail: %s", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_ExcludesPinned(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: true
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			issue:  &types.Issue{Title: "Pinned", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Pinned: true},
			labels: []string{},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q (pinned should be excluded)", check.Status, StatusOK)
		t.Logf("Detail: %s", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_InvalidConfig(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: bad
        labels: [only_one]
`
	tmpDir := setupMutexTestRepo(t, config, nil)

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
	if !strings.Contains(check.Detail, "at least 2 labels") {
		t.Errorf("Detail = %q, want it to mention 'at least 2 labels'", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_MultipleGroups(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: false
      - name: work_status
        labels: [status:todo, status:doing, status:done]
        required: true
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			// Conflict on triage + missing work_status
			issue:  &types.Issue{Title: "Double trouble", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"inbox", "accepted"},
		},
		{
			// Clean on both
			issue:  &types.Issue{Title: "Clean", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{"inbox", "status:todo"},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
	// Should have 2 violations: one conflict + one missing
	if !strings.Contains(check.Message, "2 label mutex violation") {
		t.Errorf("Message = %q, want it to contain '2 label mutex violation'", check.Message)
	}
	if !strings.Contains(check.Detail, "triage conflict") {
		t.Errorf("Detail = %q, want it to contain 'triage conflict'", check.Detail)
	}
	if !strings.Contains(check.Detail, "work_status missing") {
		t.Errorf("Detail = %q, want it to contain 'work_status missing'", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_ScopedQuery(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: task_triage
        labels: [inbox, accepted]
        required: true
        scope:
          query: "type=task"
`
	tmpDir := setupMutexTestRepo(t, config, []struct {
		issue  *types.Issue
		labels []string
	}{
		{
			// Task with no triage label — should be flagged (required, scoped to tasks)
			issue:  &types.Issue{Title: "A task", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			labels: []string{},
		},
		{
			// Bug with no triage label — should NOT be flagged (scope=type=task)
			issue:  &types.Issue{Title: "A bug", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeBug},
			labels: []string{},
		},
	})

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
	if !strings.Contains(check.Message, "1 label mutex violation") {
		t.Errorf("Message = %q, want '1 label mutex violation'", check.Message)
	}
	if !strings.Contains(check.Detail, "task_triage missing") {
		t.Errorf("Detail = %q, want it to contain 'task_triage missing'", check.Detail)
	}
}

func TestCheckLabelMutexInvariants_NoDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
`
	configPath := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	check := CheckLabelMutexInvariants(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q (no database should be OK)", check.Status, StatusOK)
	}
	if check.Message != "N/A (no database)" {
		t.Errorf("Message = %q, want 'N/A (no database)'", check.Message)
	}
}

func TestParseMutexGroups_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `
validation:
  labels:
    mutex:
      - labels: [a, b, c]
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	groups, err := labelmutex.ParseMutexGroups(configPath)
	if err != nil {
		t.Fatalf("ParseMutexGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	g := groups[0]
	if g.Required {
		t.Error("expected required=false by default")
	}
	if g.Name != "labels: a,b,c" {
		t.Errorf("synthesized name = %q, want 'labels: a,b,c'", g.Name)
	}
	if len(g.Labels) != 3 {
		t.Errorf("expected 3 labels, got %d", len(g.Labels))
	}
}

func TestParseMutexGroups_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `
validation:
  labels:
    mutex:
      - labels: [a, b, a, c, b]
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	groups, err := labelmutex.ParseMutexGroups(configPath)
	if err != nil {
		t.Fatalf("ParseMutexGroups: %v", err)
	}

	g := groups[0]
	if len(g.Labels) != 3 {
		t.Errorf("expected 3 labels after dedup, got %d: %v", len(g.Labels), g.Labels)
	}
}

func TestParseMutexGroups_MissingFile(t *testing.T) {
	groups, err := labelmutex.ParseMutexGroups("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if groups != nil {
		t.Errorf("expected nil groups for missing file, got: %v", groups)
	}
}

func TestParseMutexGroups_NoMutexKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `
validation:
  on-create: warn
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	groups, err := labelmutex.ParseMutexGroups(configPath)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if groups != nil {
		t.Errorf("expected nil groups, got: %v", groups)
	}
}
