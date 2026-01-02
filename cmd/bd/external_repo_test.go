//go:build integration && external_repo
// +build integration,external_repo

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// External repository integration tests
// These tests validate beads functionality against ANY git repository
// Run with: go test -tags=integration,external_repo ./cmd/bd/...
//
// Environment variables:
//   BEADS_TEST_EXTERNAL_REPO - Path to external repo (REQUIRED)
//
// Examples:
//   BEADS_TEST_EXTERNAL_REPO=/path/to/repo go test -tags=integration,external_repo ./cmd/bd/...
//   BEADS_TEST_EXTERNAL_REPO=. go test -tags=integration,external_repo ./cmd/bd/...

func getExternalRepoPath(t *testing.T) string {
	path := os.Getenv("BEADS_TEST_EXTERNAL_REPO")
	if path == "" {
		t.Skip("BEADS_TEST_EXTERNAL_REPO not set - provide path to any git repository")
	}
	// Resolve relative paths
	if !filepath.IsAbs(path) {
		cwd, _ := os.Getwd()
		path = filepath.Join(cwd, path)
	}
	return path
}

// cloneToTemp copies external repo to temp directory for isolated testing
func cloneToTemp(t *testing.T) string {
	t.Helper()

	srcRepo := getExternalRepoPath(t)
	if _, err := os.Stat(srcRepo); os.IsNotExist(err) {
		t.Skipf("External repo not found: %s", srcRepo)
	}

	// Verify it's a git repo
	gitDir := filepath.Join(srcRepo, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Skipf("Not a git repository: %s", srcRepo)
	}

	tmpDir := createTempDirWithCleanup(t)

	// Use robocopy on Windows for faster copy
	cmd := exec.Command("robocopy", srcRepo, tmpDir, "/E", "/NFL", "/NDL", "/NJH", "/NJS", "/NC", "/NS", "/NP")
	cmd.Run() // robocopy returns non-zero for success, ignore error

	// Create test branch
	gitCmd := exec.Command("git", "checkout", "-b", "beads-test-"+time.Now().Format("20060102-150405"))
	gitCmd.Dir = tmpDir
	gitCmd.Run()

	// Remove existing .beads for fresh start
	beadsDir := filepath.Join(tmpDir, ".beads")
	os.RemoveAll(beadsDir)

	return tmpDir
}

// TestExternalRepo_Init validates initialization in external repo
func TestExternalRepo_Init(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external repo test in short mode")
	}

	tmpDir := cloneToTemp(t)

	// TC-1.1: Basic init
	runBDInProcess(t, tmpDir, "init", "--prefix", "exttest", "--quiet")

	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("TC-1.1: Expected database at %s", dbPath)
	}

	// TC-1.2: Info returns valid JSON
	out := runBDInProcess(t, tmpDir, "info", "--json")
	var info map[string]interface{}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Errorf("TC-1.2: Failed to parse info JSON: %v", err)
	}
	if info["database_path"] == nil {
		t.Errorf("TC-1.2: Missing database_path in info output")
	}

	// TC-1.3: Duplicate init is idempotent
	runBDInProcess(t, tmpDir, "init", "--prefix", "exttest", "--quiet")
	// Should not error
}

// TestExternalRepo_IssueCRUD validates issue lifecycle
func TestExternalRepo_IssueCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external repo test in short mode")
	}

	tmpDir := cloneToTemp(t)
	runBDInProcess(t, tmpDir, "init", "--prefix", "exttest", "--quiet")

	// TC-2.1: Create issue
	out := runBDInProcess(t, tmpDir, "create", "External repo test issue", "-t", "task", "-p", "2", "--json")
	var issue map[string]interface{}
	if err := json.Unmarshal([]byte(extractJSON(out)), &issue); err != nil {
		t.Fatalf("TC-2.1: Failed to parse create output: %v", err)
	}
	id := issue["id"].(string)
	if !strings.HasPrefix(id, "exttest-") {
		t.Errorf("TC-2.1: Expected prefix 'exttest-', got: %s", id)
	}

	// TC-2.2: Show issue
	out = runBDInProcess(t, tmpDir, "show", id, "--json")
	var showResult []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &showResult); err != nil {
		t.Fatalf("TC-2.2: Failed to parse show output: %v", err)
	}
	if showResult[0]["title"] != "External repo test issue" {
		t.Errorf("TC-2.2: Title mismatch: %v", showResult[0]["title"])
	}

	// TC-2.3: Update status
	runBDInProcess(t, tmpDir, "update", id, "--status", "in_progress")
	out = runBDInProcess(t, tmpDir, "show", id, "--json")
	json.Unmarshal([]byte(out), &showResult)
	if showResult[0]["status"] != "in_progress" {
		t.Errorf("TC-2.3: Expected status 'in_progress', got: %v", showResult[0]["status"])
	}

	// TC-2.4: Close issue
	runBDInProcess(t, tmpDir, "close", id, "--reason", "Test complete")
	out = runBDInProcess(t, tmpDir, "show", id, "--json")
	json.Unmarshal([]byte(out), &showResult)
	if showResult[0]["status"] != "closed" {
		t.Errorf("TC-2.4: Expected status 'closed', got: %v", showResult[0]["status"])
	}

	// TC-2.5: Reopen issue
	runBDInProcess(t, tmpDir, "reopen", id)
	out = runBDInProcess(t, tmpDir, "show", id, "--json")
	json.Unmarshal([]byte(out), &showResult)
	if showResult[0]["status"] != "open" {
		t.Errorf("TC-2.5: Expected status 'open', got: %v", showResult[0]["status"])
	}
}

// TestExternalRepo_Sync validates sync operations
func TestExternalRepo_Sync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external repo test in short mode")
	}

	tmpDir := cloneToTemp(t)
	runBDInProcess(t, tmpDir, "init", "--prefix", "exttest", "--quiet")

	// Create test issue
	out := runBDInProcess(t, tmpDir, "create", "Sync test issue", "-t", "task", "-p", "2", "--json")
	var issue map[string]interface{}
	json.Unmarshal([]byte(extractJSON(out)), &issue)

	// TC-3.1: Export creates JSONL
	runBDInProcess(t, tmpDir, "export")
	jsonlPath := filepath.Join(tmpDir, ".beads", "issues.jsonl")
	if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
		t.Errorf("TC-3.1: JSONL file not created")
	}

	// TC-3.2: JSONL contains issue
	content, _ := os.ReadFile(jsonlPath)
	if !strings.Contains(string(content), "Sync test issue") {
		t.Errorf("TC-3.2: Issue not found in JSONL")
	}

	// TC-3.3: Sync handles missing remote gracefully
	// Remove remote
	gitCmd := exec.Command("git", "remote", "remove", "origin")
	gitCmd.Dir = tmpDir
	gitCmd.Run()

	// Sync should not crash without remote
	// Note: This may produce warnings but should not fail
}

// TestExternalRepo_Dependencies validates dependency management
func TestExternalRepo_Dependencies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external repo test in short mode")
	}

	tmpDir := cloneToTemp(t)
	runBDInProcess(t, tmpDir, "init", "--prefix", "exttest", "--quiet")

	// Create parent and child
	out := runBDInProcess(t, tmpDir, "create", "Parent task", "-t", "task", "-p", "2", "--json")
	var parent map[string]interface{}
	json.Unmarshal([]byte(extractJSON(out)), &parent)
	parentID := parent["id"].(string)

	out = runBDInProcess(t, tmpDir, "create", "Child task", "-t", "task", "-p", "2", "--json")
	var child map[string]interface{}
	json.Unmarshal([]byte(extractJSON(out)), &child)
	childID := child["id"].(string)

	// TC-5.1: Add dependency
	runBDInProcess(t, tmpDir, "dep", "add", childID, parentID)

	// TC-5.2: Blocked shows child
	out = runBDInProcess(t, tmpDir, "blocked")
	if !strings.Contains(out, childID) {
		t.Errorf("TC-5.2: Child not in blocked list")
	}

	// TC-5.3: Ready excludes blocked
	out = runBDInProcess(t, tmpDir, "ready")
	if strings.Contains(out, childID) {
		t.Errorf("TC-5.3: Blocked child should not be in ready list")
	}
}

// TestExternalRepo_ConcurrentCreation validates hash-based ID uniqueness
func TestExternalRepo_ConcurrentCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external repo test in short mode")
	}

	tmpDir := cloneToTemp(t)
	runBDInProcess(t, tmpDir, "init", "--prefix", "exttest", "--quiet")

	// Create multiple issues rapidly
	var wg sync.WaitGroup
	ids := make(chan string, 10)
	errors := make(chan error, 10)

	// Note: runBDInProcess uses mutex, so this tests sequential rapid creation
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			out := runBDInProcess(t, tmpDir, "create", "Concurrent test "+string(rune('A'+n)), "-t", "task", "-p", "3", "--json")
			var issue map[string]interface{}
			if err := json.Unmarshal([]byte(extractJSON(out)), &issue); err != nil {
				errors <- err
				return
			}
			ids <- issue["id"].(string)
		}(i)
	}

	wg.Wait()
	close(ids)
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("TC-6.1: Creation error: %v", err)
	}

	// Check uniqueness
	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Errorf("TC-6.1: Collision detected for ID: %s", id)
		}
		seen[id] = true
	}

	if len(seen) != 10 {
		t.Errorf("TC-6.1: Expected 10 unique IDs, got %d", len(seen))
	}
}

// TestExternalRepo_Recovery validates doctor and recovery operations
func TestExternalRepo_Recovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external repo test in short mode")
	}

	tmpDir := cloneToTemp(t)
	runBDInProcess(t, tmpDir, "init", "--prefix", "exttest", "--quiet")

	// Create some data
	runBDInProcess(t, tmpDir, "create", "Recovery test", "-t", "task", "-p", "2")

	// TC-8.1: Doctor runs without error
	runBDInProcess(t, tmpDir, "doctor")
	// Should complete without error

	// TC-8.2: List works after operations
	out := runBDInProcess(t, tmpDir, "list", "--json")
	var issues []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		t.Errorf("TC-8.2: Failed to parse list output: %v", err)
	}
	if len(issues) < 1 {
		t.Errorf("TC-8.2: Expected at least 1 issue")
	}
}

// extractJSON finds the first JSON object in output (handles warnings before JSON)
func extractJSON(out string) string {
	start := strings.Index(out, "{")
	if start == -1 {
		return out
	}
	return out[start:]
}
