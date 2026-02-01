//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestImportWarnsUncommittedChanges tests bd-u4f5
// Import should warn when database matches working tree but not git HEAD
func TestImportWarnsUncommittedChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git-dependent test in short mode")
	}

	// Create temporary directory with git repo
	tmpDir, err := os.MkdirTemp("", "beads-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	gitInit := exec.Command("git", "init")
	gitInit.Dir = tmpDir
	if err := gitInit.Run(); err != nil {
		t.Fatalf("Failed to init git: %v", err)
	}

	// Configure git user
	gitConfig1 := exec.Command("git", "config", "user.email", "test@example.com")
	gitConfig1.Dir = tmpDir
	if err := gitConfig1.Run(); err != nil {
		t.Fatalf("Failed to configure git: %v", err)
	}
	gitConfig2 := exec.Command("git", "config", "user.name", "Test User")
	gitConfig2.Dir = tmpDir
	if err := gitConfig2.Run(); err != nil {
		t.Fatalf("Failed to configure git: %v", err)
	}

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	// Initialize database
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	// Step 1: Create initial issue and export
	issue1 := &types.Issue{
		ID:          "test-1",
		Title:       "Original Issue",
		Description: "Original description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Export to JSONL
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("Failed to create JSONL: %v", err)
	}
	encoder := json.NewEncoder(f)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			f.Close()
			t.Fatalf("Failed to encode issue: %v", err)
		}
	}
	f.Close()

	// Commit the initial JSONL to git
	gitAdd := exec.Command("git", "add", ".beads/issues.jsonl")
	gitAdd.Dir = tmpDir
	if err := gitAdd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	gitCommit := exec.Command("git", "commit", "-m", "Initial commit")
	gitCommit.Dir = tmpDir
	if err := gitCommit.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Step 2: Add a new issue to database and export (creating uncommitted change)
	issue2 := &types.Issue{
		ID:          "test-2",
		Title:       "New Issue",
		Description: "New description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeBug,
	}

	if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("Failed to create second issue: %v", err)
	}

	// Export again (now JSONL has 2 issues, but git HEAD has 1)
	issues, err = store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	f, err = os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("Failed to recreate JSONL: %v", err)
	}
	encoder = json.NewEncoder(f)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			f.Close()
			t.Fatalf("Failed to encode issue: %v", err)
		}
	}
	f.Close()

	// Step 3: Run import and capture output
	// Database already matches working tree, so import will report 0 created, 0 updated
	// But working tree differs from git HEAD, so we should see a warning

	opts := ImportOptions{
		DryRun:     false,
		SkipUpdate: false,
		Strict:     false,
	}

	// Read JSONL for import
	importData, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("Failed to read JSONL: %v", err)
	}

	var importIssues []*types.Issue
	lines := bytes.Split(importData, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var issue types.Issue
		if err := json.Unmarshal(line, &issue); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}
		importIssues = append(importIssues, &issue)
	}

	result, err := importIssuesCore(ctx, dbPath, store, importIssues, opts)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Verify no changes (database already synced)
	if result.Created != 0 || result.Updated != 0 {
		t.Errorf("Expected 0 created, 0 updated, got created=%d updated=%d", result.Created, result.Updated)
	}

	// Now test the warning detection function directly
	// Capture stderr to check for warning
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkUncommittedChanges(jsonlPath, result)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify warning is present
	if !strings.Contains(output, "Warning") {
		t.Errorf("Expected warning about uncommitted changes, got: %s", output)
	}
	if !strings.Contains(output, "uncommitted changes") {
		t.Errorf("Expected warning to mention 'uncommitted changes', got: %s", output)
	}
	if !strings.Contains(output, "Working tree:") {
		t.Errorf("Expected warning to show working tree line count, got: %s", output)
	}
	if !strings.Contains(output, "database already synced with working tree") {
		t.Errorf("Expected warning to clarify sync status, got: %s", output)
	}
	// Git HEAD line count is optional - may not show if git command fails
	// The important part is that we detect uncommitted changes at all
}

// TestImportNoWarningWhenClean tests that import doesn't warn when working tree matches git HEAD
func TestImportNoWarningWhenClean(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git-dependent test in short mode")
	}

	// Create temporary directory with git repo
	tmpDir, err := os.MkdirTemp("", "beads-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	gitInit := exec.Command("git", "init")
	gitInit.Dir = tmpDir
	if err := gitInit.Run(); err != nil {
		t.Fatalf("Failed to init git: %v", err)
	}

	// Configure git user
	gitConfig1 := exec.Command("git", "config", "user.email", "test@example.com")
	gitConfig1.Dir = tmpDir
	if err := gitConfig1.Run(); err != nil {
		t.Fatalf("Failed to configure git: %v", err)
	}
	gitConfig2 := exec.Command("git", "config", "user.name", "Test User")
	gitConfig2.Dir = tmpDir
	if err := gitConfig2.Run(); err != nil {
		t.Fatalf("Failed to configure git: %v", err)
	}

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	// Initialize database
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	// Create and export issue
	issue := &types.Issue{
		ID:          "test-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Export to JSONL
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("Failed to create JSONL: %v", err)
	}
	encoder := json.NewEncoder(f)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			f.Close()
			t.Fatalf("Failed to encode issue: %v", err)
		}
	}
	f.Close()

	// Commit to git (now working tree matches HEAD)
	gitAdd := exec.Command("git", "add", ".beads/issues.jsonl")
	gitAdd.Dir = tmpDir
	if err := gitAdd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	gitCommit := exec.Command("git", "commit", "-m", "Commit JSONL")
	gitCommit.Dir = tmpDir
	if err := gitCommit.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Run import
	opts := ImportOptions{
		DryRun:     false,
		SkipUpdate: false,
		Strict:     false,
	}

	importData, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("Failed to read JSONL: %v", err)
	}

	var importIssues []*types.Issue
	lines := bytes.Split(importData, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var iss types.Issue
		if err := json.Unmarshal(line, &iss); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}
		importIssues = append(importIssues, &iss)
	}

	result, err := importIssuesCore(ctx, dbPath, store, importIssues, opts)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkUncommittedChanges(jsonlPath, result)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify NO warning when clean
	if strings.Contains(output, "Warning") || strings.Contains(output, "uncommitted") {
		t.Errorf("Expected no warning when working tree matches git HEAD, got: %s", output)
	}
}
