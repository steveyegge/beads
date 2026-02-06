package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestRunValidation_AllClean(t *testing.T) {
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

	// Use prefix "val" to avoid test pollution false positives (ids like test-* trigger detection)
	if err := store.SetConfig(ctx, "issue_prefix", "val"); err != nil {
		store.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	// Create a few non-duplicate, non-test issues
	// Note: titles must NOT match test pollution patterns (test-*, Test Issue*)
	issues := []*types.Issue{
		{Title: "Fix login bug", Description: "Login fails on Safari", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeBug},
		{Title: "Add search feature", Description: "Full-text search", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}
	for _, issue := range issues {
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}
	}

	// Write empty JSONL so git conflicts check has a file to scan
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create JSONL: %v", err)
	}

	store.Close()

	result := runValidation(tmpDir)

	if !result.OverallOK {
		t.Errorf("OverallOK = false, want true for clean database")
		for _, check := range result.Checks {
			if check.Status != statusOK {
				t.Logf("  %s: %s (%s)", check.Name, check.Message, check.Status)
			}
		}
	}

	if len(result.Checks) != 4 {
		t.Errorf("Expected 4 checks, got %d", len(result.Checks))
	}
}

func TestRunValidation_DetectsDuplicates(t *testing.T) {
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
		store.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	// Create duplicate open issues
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			Title:       "Duplicate task",
			Description: "Same description",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}
	}
	store.Close()

	result := runValidation(tmpDir)

	if result.OverallOK {
		t.Error("OverallOK = true, want false when duplicates exist")
	}

	// Find the duplicate check
	found := false
	for _, check := range result.Checks {
		if check.Name == "Duplicate Issues" {
			found = true
			if check.Status != statusWarning {
				t.Errorf("Duplicate Issues status = %q, want %q", check.Status, statusWarning)
			}
		}
	}
	if !found {
		t.Error("Duplicate Issues check not found in results")
	}
}

func TestRunValidation_DetectsOrphanedDeps(t *testing.T) {
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
		store.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	// Create one issue
	issue := &types.Issue{
		Title:     "Real issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Insert an orphaned dependency directly into the DB
	db := store.UnderlyingDB()
	_, err = db.Exec("INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES (?, ?, ?, ?)",
		issue.ID, "test-nonexistent", "blocks", "test")
	if err != nil {
		store.Close()
		t.Fatalf("Failed to insert orphaned dep: %v", err)
	}
	store.Close()

	result := runValidation(tmpDir)

	if result.OverallOK {
		t.Error("OverallOK = true, want false when orphaned deps exist")
	}

	for _, check := range result.Checks {
		if check.Name == "Orphaned Dependencies" {
			if check.Status != statusWarning {
				t.Errorf("Orphaned Dependencies status = %q, want %q", check.Status, statusWarning)
			}
			return
		}
	}
	t.Error("Orphaned Dependencies check not found in results")
}

func TestRunValidation_DetectsGitConflicts(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write JSONL with conflict markers
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	conflictContent := `{"id":"test-1","title":"Issue 1","status":"open"}
<<<<<<< HEAD
{"id":"test-2","title":"Issue 2 local","status":"open"}
=======
{"id":"test-2","title":"Issue 2 remote","status":"open"}
>>>>>>> origin/main
`
	if err := os.WriteFile(jsonlPath, []byte(conflictContent), 0644); err != nil {
		t.Fatalf("Failed to write JSONL: %v", err)
	}

	result := runValidation(tmpDir)

	if result.OverallOK {
		t.Error("OverallOK = true, want false when git conflicts exist")
	}

	for _, check := range result.Checks {
		if check.Name == "Git Conflicts" {
			if check.Status != statusError {
				t.Errorf("Git Conflicts status = %q, want %q", check.Status, statusError)
			}
			return
		}
	}
	t.Error("Git Conflicts check not found in results")
}

func TestRunValidation_DetectsTestPollution(t *testing.T) {
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
		store.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	// Create test-like issues
	testIssues := []*types.Issue{
		{Title: "test-pollution-check", Description: "A test issue", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{Title: "Test Issue 1", Description: "Another test", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}
	for _, issue := range testIssues {
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}
	}
	store.Close()

	result := runValidation(tmpDir)

	if result.OverallOK {
		t.Error("OverallOK = true, want false when test pollution exists")
	}

	for _, check := range result.Checks {
		if check.Name == "Test Pollution" {
			if check.Status != statusWarning {
				t.Errorf("Test Pollution status = %q, want %q", check.Status, statusWarning)
			}
			return
		}
	}
	t.Error("Test Pollution check not found in results")
}

func TestRunValidation_NoBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	// No .beads/ directory

	result := runValidation(tmpDir)

	// All checks should be OK (N/A)
	if !result.OverallOK {
		t.Error("OverallOK = false, want true when no .beads/ exists")
		for _, check := range result.Checks {
			if check.Status != statusOK {
				t.Logf("  %s: %s (%s)", check.Name, check.Message, check.Status)
			}
		}
	}
}

func TestRunValidation_FixAllOrphanedDeps(t *testing.T) {
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
		store.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	issue := &types.Issue{
		Title:     "Real issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	db := store.UnderlyingDB()
	_, err = db.Exec("INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES (?, ?, ?, ?)",
		issue.ID, "test-nonexistent", "blocks", "test")
	if err != nil {
		store.Close()
		t.Fatalf("Failed to insert orphaned dep: %v", err)
	}
	store.Close()

	// Verify orphan is detected
	result := runValidation(tmpDir)
	if result.OverallOK {
		t.Fatal("Pre-condition: expected orphaned deps to be detected")
	}

	// Apply fixes (set --yes to skip confirmation in test)
	origYes := validateYes
	validateYes = true
	defer func() { validateYes = origYes }()
	applyValidationFixes(tmpDir, result)

	// Verify fix was applied
	result = runValidation(tmpDir)
	for _, check := range result.Checks {
		if check.Name == "Orphaned Dependencies" {
			if check.Status != statusOK {
				t.Errorf("After fix: Orphaned Dependencies status = %q, want %q", check.Status, statusOK)
			}
			return
		}
	}
	t.Error("Orphaned Dependencies check not found after fix")
}
