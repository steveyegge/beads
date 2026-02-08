package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestCheckLabelMutexPolicy_NothingConfigured(t *testing.T) {
	tmpDir := setupMutexTestRepo(t, "", nil)

	check := CheckLabelMutexPolicy(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q", check.Status, StatusOK)
	}
	if !strings.Contains(check.Message, "No label mutex policy configured") {
		t.Errorf("Message = %q, want 'No label mutex policy configured'", check.Message)
	}
}

func TestCheckLabelMutexPolicy_DriftDetected(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
`
	tmpDir := setupMutexTestRepo(t, config, nil)

	check := CheckLabelMutexPolicy(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
	if !strings.Contains(check.Message, "drift") {
		t.Errorf("Message = %q, want it to contain 'drift'", check.Message)
	}
	if !strings.Contains(check.Detail, "YAML defines 1 group(s), DB has 0 group(s)") {
		t.Errorf("Detail = %q, want it to describe the mismatch", check.Detail)
	}
	if check.Fix == "" {
		t.Error("Fix should not be empty for drift warning")
	}
}

func TestCheckLabelMutexPolicy_InSync(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
        required: false
`
	tmpDir := setupMutexTestRepo(t, config, nil)

	// Manually populate DB tables to match YAML.
	beadsDir := filepath.Join(tmpDir, ".beads")
	dbPath := filepath.Join(beadsDir, beads.CanonicalDatabaseName)
	ctx := context.Background()

	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	db := store.UnderlyingDB()

	_, err = db.Exec(`INSERT INTO label_mutex_groups (name, required) VALUES ('triage', 0)`)
	if err != nil {
		t.Fatalf("Failed to insert group: %v", err)
	}
	_, err = db.Exec(`INSERT INTO label_mutex_members (group_id, label) VALUES (1, 'accepted')`)
	if err != nil {
		t.Fatalf("Failed to insert member: %v", err)
	}
	_, err = db.Exec(`INSERT INTO label_mutex_members (group_id, label) VALUES (1, 'inbox')`)
	if err != nil {
		t.Fatalf("Failed to insert member: %v", err)
	}
	store.Close()

	check := CheckLabelMutexPolicy(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		t.Logf("Message: %s", check.Message)
		t.Logf("Detail: %s", check.Detail)
	}
	if !strings.Contains(check.Message, "DB policy matches YAML") {
		t.Errorf("Message = %q, want 'DB policy matches YAML'", check.Message)
	}
}

func TestCheckLabelMutexPolicy_NoDatabase(t *testing.T) {
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

	check := CheckLabelMutexPolicy(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q (YAML groups but no DB)", check.Status, StatusWarning)
	}
}

func TestTriggerEnforcement_ConflictBlocked(t *testing.T) {
	config := `
validation:
  labels:
    mutex:
      - name: triage
        labels: [inbox, accepted]
`
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

	issue := &types.Issue{Title: "Test issue", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Populate policy tables to activate the trigger.
	db := store.UnderlyingDB()
	_, err = db.Exec(`INSERT INTO label_mutex_groups (name, required) VALUES ('triage', 0)`)
	if err != nil {
		t.Fatalf("Failed to insert group: %v", err)
	}
	_, err = db.Exec(`INSERT INTO label_mutex_members (group_id, label) VALUES (1, 'inbox')`)
	if err != nil {
		t.Fatalf("Failed to insert member: %v", err)
	}
	_, err = db.Exec(`INSERT INTO label_mutex_members (group_id, label) VALUES (1, 'accepted')`)
	if err != nil {
		t.Fatalf("Failed to insert member: %v", err)
	}

	// Add first label — should succeed.
	if err := store.AddLabel(ctx, issue.ID, "inbox", "test"); err != nil {
		t.Fatalf("AddLabel('inbox') should succeed, got: %v", err)
	}

	// Add conflicting label — should fail.
	err = store.AddLabel(ctx, issue.ID, "accepted", "test")
	if err == nil {
		t.Fatal("AddLabel('accepted') should have failed with mutex violation")
	}
	if !strings.Contains(err.Error(), "label mutex violation") {
		t.Errorf("Error = %q, want it to contain 'label mutex violation'", err.Error())
	}

	// Write config for completeness
	configPath := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config.yaml: %v", err)
	}

	store.Close()
}

func TestTriggerEnforcement_EmptyPolicyAllowsAll(t *testing.T) {
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

	issue := &types.Issue{Title: "Test issue", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// With empty policy tables, trigger "fails open" — any labels allowed.
	if err := store.AddLabel(ctx, issue.ID, "inbox", "test"); err != nil {
		t.Fatalf("AddLabel('inbox') with empty policy should succeed, got: %v", err)
	}
	if err := store.AddLabel(ctx, issue.ID, "accepted", "test"); err != nil {
		t.Fatalf("AddLabel('accepted') with empty policy should succeed, got: %v", err)
	}

	store.Close()
}

func TestTriggerEnforcement_NonMutexLabelsAllowed(t *testing.T) {
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

	issue := &types.Issue{Title: "Test issue", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Populate policy: only inbox/accepted are mutex.
	db := store.UnderlyingDB()
	_, err = db.Exec(`INSERT INTO label_mutex_groups (name, required) VALUES ('triage', 0)`)
	if err != nil {
		t.Fatalf("Failed to insert group: %v", err)
	}
	_, err = db.Exec(`INSERT INTO label_mutex_members (group_id, label) VALUES (1, 'inbox')`)
	if err != nil {
		t.Fatalf("Failed to insert member: %v", err)
	}
	_, err = db.Exec(`INSERT INTO label_mutex_members (group_id, label) VALUES (1, 'accepted')`)
	if err != nil {
		t.Fatalf("Failed to insert member: %v", err)
	}

	// Add inbox (in mutex group).
	if err := store.AddLabel(ctx, issue.ID, "inbox", "test"); err != nil {
		t.Fatalf("AddLabel('inbox') should succeed, got: %v", err)
	}

	// Add unrelated labels — should succeed (not in any mutex group).
	if err := store.AddLabel(ctx, issue.ID, "bug", "test"); err != nil {
		t.Fatalf("AddLabel('bug') should succeed, got: %v", err)
	}
	if err := store.AddLabel(ctx, issue.ID, "priority:high", "test"); err != nil {
		t.Fatalf("AddLabel('priority:high') should succeed, got: %v", err)
	}

	store.Close()
}
