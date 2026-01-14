package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/steveyegge/beads/internal/git"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// Multi-Clone Sync Tests
// Tests for sync operations across multiple clones of the same repository,
// BEADS_DIR collision scenarios, and repo-specific lock/state isolation.
// =============================================================================

// TestTwoClonesSync tests syncing between two clones of the same repository.
func TestTwoClonesSync(t *testing.T) {
	ctx := context.Background()

	// Create a "bare" origin repository
	originDir := t.TempDir()
	if err := exec.Command("git", "init", "--bare", "-b", "main", originDir).Run(); err != nil {
		t.Fatalf("git init --bare failed: %v", err)
	}

	// Create clone1
	clone1Dir := t.TempDir()
	if err := exec.Command("git", "clone", originDir, clone1Dir).Run(); err != nil {
		t.Fatalf("git clone to clone1 failed: %v", err)
	}
	configureGitInDirForMulticlone(t, clone1Dir)

	// Create clone2
	clone2Dir := t.TempDir()
	if err := exec.Command("git", "clone", originDir, clone2Dir).Run(); err != nil {
		t.Fatalf("git clone to clone2 failed: %v", err)
	}
	configureGitInDirForMulticlone(t, clone2Dir)

	// Initialize beads in clone1
	beadsDir1 := filepath.Join(clone1Dir, ".beads")
	if err := os.MkdirAll(beadsDir1, 0755); err != nil {
		t.Fatalf("mkdir beads1 failed: %v", err)
	}

	jsonlPath1 := filepath.Join(beadsDir1, "issues.jsonl")
	dbPath1 := filepath.Join(beadsDir1, "beads.db")

	// Create initial issue in clone1
	store1, err := sqlite.New(ctx, dbPath1)
	if err != nil {
		t.Fatalf("create store1 failed: %v", err)
	}
	defer store1.Close()
	if err := store1.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("set prefix1 failed: %v", err)
	}

	issue1 := &types.Issue{
		ID:        "test-0001",
		Title:     "Issue from Clone1",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store1.CreateIssue(ctx, issue1, "user1"); err != nil {
		t.Fatalf("create issue in clone1 failed: %v", err)
	}

	// Export to JSONL in clone1
	if err := exportToJSONLForMulticlone(ctx, store1, jsonlPath1); err != nil {
		t.Fatalf("export in clone1 failed: %v", err)
	}

	// Commit and push from clone1
	if err := exec.Command("git", "-C", clone1Dir, "add", ".beads").Run(); err != nil {
		t.Fatalf("git add in clone1 failed: %v", err)
	}
	if err := exec.Command("git", "-C", clone1Dir, "commit", "-m", "add beads").Run(); err != nil {
		t.Fatalf("git commit in clone1 failed: %v", err)
	}
	if err := exec.Command("git", "-C", clone1Dir, "push", "origin", "main").Run(); err != nil {
		t.Fatalf("git push from clone1 failed: %v", err)
	}

	// Pull in clone2
	if err := exec.Command("git", "-C", clone2Dir, "pull", "origin", "main").Run(); err != nil {
		t.Fatalf("git pull in clone2 failed: %v", err)
	}

	// Initialize database in clone2
	beadsDir2 := filepath.Join(clone2Dir, ".beads")
	jsonlPath2 := filepath.Join(beadsDir2, "issues.jsonl")
	dbPath2 := filepath.Join(beadsDir2, "beads.db")

	// Verify JSONL was pulled
	if _, err := os.Stat(jsonlPath2); os.IsNotExist(err) {
		t.Fatal("JSONL not pulled to clone2")
	}

	// Create store and import in clone2
	store2, err := sqlite.New(ctx, dbPath2)
	if err != nil {
		t.Fatalf("create store2 failed: %v", err)
	}
	defer store2.Close()
	if err := store2.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("set prefix2 failed: %v", err)
	}

	// Load issues from JSONL
	issues2, err := loadIssuesFromJSONL(jsonlPath2)
	if err != nil {
		t.Fatalf("load JSONL in clone2 failed: %v", err)
	}

	if len(issues2) != 1 {
		t.Fatalf("expected 1 issue in clone2, got %d", len(issues2))
	}
	if issues2[0].Title != "Issue from Clone1" {
		t.Errorf("issue title mismatch: expected 'Issue from Clone1', got %q", issues2[0].Title)
	}

	// Create new issue in clone2
	issue2 := &types.Issue{
		ID:        "test-0002",
		Title:     "Issue from Clone2",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeBug,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store2.CreateIssue(ctx, issue2, "user2"); err != nil {
		t.Fatalf("create issue in clone2 failed: %v", err)
	}

	// Export to JSONL in clone2
	if err := exportToJSONLForMulticlone(ctx, store2, jsonlPath2); err != nil {
		t.Fatalf("export in clone2 failed: %v", err)
	}

	// Commit and push from clone2
	if err := exec.Command("git", "-C", clone2Dir, "add", ".beads").Run(); err != nil {
		t.Fatalf("git add in clone2 failed: %v", err)
	}
	if err := exec.Command("git", "-C", clone2Dir, "commit", "-m", "add issue2").Run(); err != nil {
		t.Fatalf("git commit in clone2 failed: %v", err)
	}
	if err := exec.Command("git", "-C", clone2Dir, "push", "origin", "main").Run(); err != nil {
		t.Fatalf("git push from clone2 failed: %v", err)
	}

	// Pull in clone1
	if err := exec.Command("git", "-C", clone1Dir, "pull", "origin", "main").Run(); err != nil {
		t.Fatalf("git pull in clone1 failed: %v", err)
	}

	// Verify clone1 now has both issues
	issues1, err := loadIssuesFromJSONL(jsonlPath1)
	if err != nil {
		t.Fatalf("load JSONL in clone1 after pull failed: %v", err)
	}
	if len(issues1) != 2 {
		t.Errorf("expected 2 issues in clone1 after pull, got %d", len(issues1))
	}
}

// TestBeadsDirCollision tests behavior when BEADS_DIR environment variable
// points to a shared location used by multiple repos.
func TestBeadsDirCollision(t *testing.T) {
	ctx := context.Background()

	// Create shared beads directory (simulating BEADS_DIR override)
	sharedBeadsDir := t.TempDir()
	jsonlPath := filepath.Join(sharedBeadsDir, "issues.jsonl")
	dbPath := filepath.Join(sharedBeadsDir, "beads.db")
	lockPath := filepath.Join(sharedBeadsDir, ".sync.lock")

	// Create two different "repos" that would both try to use the shared beads dir
	repo1Dir := t.TempDir()
	repo2Dir := t.TempDir()

	// Initialize both as git repos
	_ = exec.Command("git", "init", "-b", "main", repo1Dir).Run()
	_ = exec.Command("git", "init", "-b", "main", repo2Dir).Run()
	configureGitInDirForMulticlone(t, repo1Dir)
	configureGitInDirForMulticlone(t, repo2Dir)

	// Create a shared store (this simulates the collision)
	sharedStore, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("create shared store failed: %v", err)
	}
	defer sharedStore.Close()
	if err := sharedStore.SetConfig(ctx, "issue_prefix", "shared"); err != nil {
		t.Fatalf("set shared prefix failed: %v", err)
	}

	// Simulate concurrent access from two repos
	t.Run("lock prevents concurrent sync", func(t *testing.T) {
		// First "repo" acquires lock
		lock1 := flock.New(lockPath)
		locked1, err := lock1.TryLock()
		if err != nil {
			t.Fatalf("lock1 error: %v", err)
		}
		if !locked1 {
			t.Fatal("expected lock1 to succeed")
		}
		defer lock1.Unlock()

		// Second "repo" tries to sync (should fail)
		lock2 := flock.New(lockPath)
		locked2, err := lock2.TryLock()
		if err != nil {
			t.Fatalf("lock2 error: %v", err)
		}
		if locked2 {
			lock2.Unlock()
			t.Error("expected lock2 to fail (collision)")
		}
	})

	t.Run("issues from different repos get mixed", func(t *testing.T) {
		// This demonstrates the DATA CORRUPTION risk of BEADS_DIR collision
		// Create issue "from repo1"
		issue1 := &types.Issue{
			ID:        "shared-0001",
			Title:     "From Repo1",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := sharedStore.CreateIssue(ctx, issue1, "repo1"); err != nil {
			t.Fatalf("create issue1 failed: %v", err)
		}

		// Create issue "from repo2" (different repo, same shared store!)
		issue2 := &types.Issue{
			ID:        "shared-0002",
			Title:     "From Repo2",
			Status:    types.StatusOpen,
			IssueType: types.TypeBug,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := sharedStore.CreateIssue(ctx, issue2, "repo2"); err != nil {
			t.Fatalf("create issue2 failed: %v", err)
		}

		// Export to JSONL
		if err := exportToJSONLForMulticlone(ctx, sharedStore, jsonlPath); err != nil {
			t.Fatalf("export shared failed: %v", err)
		}

		// Verify JSONL contains issues from both repos (demonstrating the collision)
		data, err := os.ReadFile(jsonlPath)
		if err != nil {
			t.Fatalf("read JSONL failed: %v", err)
		}

		content := string(data)
		hasRepo1 := strings.Contains(content, "From Repo1")
		hasRepo2 := strings.Contains(content, "From Repo2")

		if !hasRepo1 || !hasRepo2 {
			t.Logf("JSONL content: %s", content)
		}

		// THIS IS THE BUG - both repos' issues are in the same JSONL
		// In a proper setup, each repo would have its own .beads/
		t.Logf("Collision demonstrated: repo1 issue present=%v, repo2 issue present=%v",
			hasRepo1, hasRepo2)
	})
}

// TestRepoSpecificLockIsolation verifies that sync locks are repo-specific.
func TestRepoSpecificLockIsolation(t *testing.T) {
	// Create two independent repos
	repo1Dir := t.TempDir()
	repo2Dir := t.TempDir()

	// Initialize both repos
	_ = exec.Command("git", "init", "-b", "main", repo1Dir).Run()
	_ = exec.Command("git", "init", "-b", "main", repo2Dir).Run()

	// Create .beads directories
	beadsDir1 := filepath.Join(repo1Dir, ".beads")
	beadsDir2 := filepath.Join(repo2Dir, ".beads")
	_ = os.MkdirAll(beadsDir1, 0755)
	_ = os.MkdirAll(beadsDir2, 0755)

	lockPath1 := filepath.Join(beadsDir1, ".sync.lock")
	lockPath2 := filepath.Join(beadsDir2, ".sync.lock")

	// Acquire lock in repo1
	lock1 := flock.New(lockPath1)
	locked1, err := lock1.TryLock()
	if err != nil {
		t.Fatalf("lock1 error: %v", err)
	}
	if !locked1 {
		t.Fatal("expected to acquire lock1")
	}
	defer lock1.Unlock()

	// Acquiring lock in repo2 should succeed (different file)
	lock2 := flock.New(lockPath2)
	locked2, err := lock2.TryLock()
	if err != nil {
		t.Fatalf("lock2 error: %v", err)
	}
	if !locked2 {
		t.Error("expected to acquire lock2 (different repo)")
	} else {
		lock2.Unlock()
	}
}

// TestRepoSpecificStateIsolation verifies that base state is repo-specific.
func TestRepoSpecificStateIsolation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// Create two repos
	repo1Dir := t.TempDir()
	repo2Dir := t.TempDir()

	beadsDir1 := filepath.Join(repo1Dir, ".beads")
	beadsDir2 := filepath.Join(repo2Dir, ".beads")
	_ = os.MkdirAll(beadsDir1, 0755)
	_ = os.MkdirAll(beadsDir2, 0755)

	// Save different base states
	issues1 := []*types.Issue{{
		ID:        "repo1-0001",
		Title:     "Repo1 Issue",
		Status:    types.StatusOpen,
		UpdatedAt: now,
		CreatedAt: now,
	}}
	issues2 := []*types.Issue{{
		ID:        "repo2-0001",
		Title:     "Repo2 Issue",
		Status:    types.StatusClosed,
		UpdatedAt: now,
		CreatedAt: now,
	}}

	if err := saveBaseState(beadsDir1, issues1); err != nil {
		t.Fatalf("save base state 1 failed: %v", err)
	}
	if err := saveBaseState(beadsDir2, issues2); err != nil {
		t.Fatalf("save base state 2 failed: %v", err)
	}

	// Verify states are independent
	loaded1, err := loadBaseState(beadsDir1)
	if err != nil {
		t.Fatalf("load base state 1 failed: %v", err)
	}
	loaded2, err := loadBaseState(beadsDir2)
	if err != nil {
		t.Fatalf("load base state 2 failed: %v", err)
	}

	if len(loaded1) != 1 || loaded1[0].ID != "repo1-0001" {
		t.Errorf("repo1 base state incorrect: %v", loaded1)
	}
	if len(loaded2) != 1 || loaded2[0].ID != "repo2-0001" {
		t.Errorf("repo2 base state incorrect: %v", loaded2)
	}

	_ = ctx // satisfy unused warning
}

// TestWorktreeSyncIsolation tests sync isolation between git worktrees.
func TestWorktreeSyncIsolation(t *testing.T) {
	ctx := context.Background()

	// Create main repo
	mainRepoDir := t.TempDir()
	if err := exec.Command("git", "init", "-b", "main", mainRepoDir).Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	configureGitInDirForMulticlone(t, mainRepoDir)

	// Create initial commit
	testFile := filepath.Join(mainRepoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("write test.txt failed: %v", err)
	}
	_ = exec.Command("git", "-C", mainRepoDir, "add", "test.txt").Run()
	_ = exec.Command("git", "-C", mainRepoDir, "commit", "-m", "initial").Run()

	// Create beads dir in main repo
	mainBeadsDir := filepath.Join(mainRepoDir, ".beads")
	if err := os.MkdirAll(mainBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir beads failed: %v", err)
	}
	mainJsonlPath := filepath.Join(mainBeadsDir, "issues.jsonl")
	if err := os.WriteFile(mainJsonlPath, []byte(`{"id":"main-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write main JSONL failed: %v", err)
	}
	_ = exec.Command("git", "-C", mainRepoDir, "add", ".beads").Run()
	_ = exec.Command("git", "-C", mainRepoDir, "commit", "-m", "add beads").Run()

	// Create a branch for worktree
	_ = exec.Command("git", "-C", mainRepoDir, "branch", "worktree-branch").Run()

	// Create worktree
	worktreeDir := t.TempDir()
	if err := exec.Command("git", "-C", mainRepoDir, "worktree", "add", worktreeDir, "worktree-branch").Run(); err != nil {
		t.Fatalf("git worktree add failed: %v", err)
	}
	git.ResetCaches()

	// Verify worktree has .beads from the branch
	worktreeBeadsDir := filepath.Join(worktreeDir, ".beads")
	worktreeJsonlPath := filepath.Join(worktreeBeadsDir, "issues.jsonl")

	if _, err := os.Stat(worktreeJsonlPath); os.IsNotExist(err) {
		t.Log("worktree does not have .beads (expected if branch didn't have it)")
	}

	// Test that sync locks are separate for main and worktree
	mainLockPath := filepath.Join(mainBeadsDir, ".sync.lock")
	worktreeLockPath := filepath.Join(worktreeBeadsDir, ".sync.lock")

	// Create worktree beads dir if it doesn't exist
	_ = os.MkdirAll(worktreeBeadsDir, 0755)

	mainLock := flock.New(mainLockPath)
	locked, err := mainLock.TryLock()
	if err != nil {
		t.Fatalf("main lock error: %v", err)
	}
	if !locked {
		t.Fatal("expected to acquire main lock")
	}
	defer mainLock.Unlock()

	// Worktree lock should be independent
	wtLock := flock.New(worktreeLockPath)
	wtLocked, err := wtLock.TryLock()
	if err != nil {
		t.Fatalf("worktree lock error: %v", err)
	}
	// whether this succeeds depends on if worktree shares .beads with main
	t.Logf("worktree lock independent: %v", wtLocked)
	if wtLocked {
		wtLock.Unlock()
	}

	_ = ctx
}

// TestConcurrentSyncsInDifferentRepos tests that syncs in different repos don't interfere.
func TestConcurrentSyncsInDifferentRepos(t *testing.T) {
	ctx := context.Background()

	// Create two repos
	repo1Dir := t.TempDir()
	repo2Dir := t.TempDir()

	// Initialize both
	_ = exec.Command("git", "init", "-b", "main", repo1Dir).Run()
	_ = exec.Command("git", "init", "-b", "main", repo2Dir).Run()
	configureGitInDirForMulticlone(t, repo1Dir)
	configureGitInDirForMulticlone(t, repo2Dir)

	// Create beads dirs
	beadsDir1 := filepath.Join(repo1Dir, ".beads")
	beadsDir2 := filepath.Join(repo2Dir, ".beads")
	_ = os.MkdirAll(beadsDir1, 0755)
	_ = os.MkdirAll(beadsDir2, 0755)

	// Create stores
	store1, err := sqlite.New(ctx, filepath.Join(beadsDir1, "beads.db"))
	if err != nil {
		t.Fatalf("create store1 failed: %v", err)
	}
	defer store1.Close()
	_ = store1.SetConfig(ctx, "issue_prefix", "r1")

	store2, err := sqlite.New(ctx, filepath.Join(beadsDir2, "beads.db"))
	if err != nil {
		t.Fatalf("create store2 failed: %v", err)
	}
	defer store2.Close()
	_ = store2.SetConfig(ctx, "issue_prefix", "r2")

	// Create issues in both repos concurrently
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 10; i++ {
			issue := &types.Issue{
				ID:        "r1-" + string(rune('a'+i)),
				Title:     "Repo1 Issue",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			_ = store1.CreateIssue(ctx, issue, "user1")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 10; i++ {
			issue := &types.Issue{
				ID:        "r2-" + string(rune('a'+i)),
				Title:     "Repo2 Issue",
				Status:    types.StatusOpen,
				IssueType: types.TypeBug,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			_ = store2.CreateIssue(ctx, issue, "user2")
		}
		done <- true
	}()

	// Wait for both
	<-done
	<-done

	// Verify isolation - repo1 should only have r1- issues
	issues1, _ := store1.SearchIssues(ctx, "", types.IssueFilter{})
	for _, issue := range issues1 {
		if issue.ID[:2] != "r1" {
			t.Errorf("repo1 contains non-r1 issue: %s", issue.ID)
		}
	}

	// repo2 should only have r2- issues
	issues2, _ := store2.SearchIssues(ctx, "", types.IssueFilter{})
	for _, issue := range issues2 {
		if issue.ID[:2] != "r2" {
			t.Errorf("repo2 contains non-r2 issue: %s", issue.ID)
		}
	}
}

// TestSyncAfterRepoMove tests sync behavior after repository is moved/renamed.
func TestSyncAfterRepoMove(t *testing.T) {
	ctx := context.Background()

	// Create and initialize repo
	originalDir := t.TempDir()
	_ = exec.Command("git", "init", "-b", "main", originalDir).Run()
	configureGitInDirForMulticlone(t, originalDir)

	// Create initial commit
	testFile := filepath.Join(originalDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("test"), 0644)
	_ = exec.Command("git", "-C", originalDir, "add", "test.txt").Run()
	_ = exec.Command("git", "-C", originalDir, "commit", "-m", "initial").Run()

	// Create beads dir with JSONL
	beadsDir := filepath.Join(originalDir, ".beads")
	_ = os.MkdirAll(beadsDir, 0755)
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	_ = os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0644)
	_ = exec.Command("git", "-C", originalDir, "add", ".beads").Run()
	_ = exec.Command("git", "-C", originalDir, "commit", "-m", "add beads").Run()

	// Create store
	dbPath := filepath.Join(beadsDir, "beads.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("create store failed: %v", err)
	}
	_ = store.SetConfig(ctx, "issue_prefix", "test")
	store.Close()

	// "Move" repo by renaming directory (simulated via copy since we can't rename temp dirs)
	newDir := t.TempDir()
	newBeadsDir := filepath.Join(newDir, ".beads")

	// Copy .git and .beads
	if err := copyDirForMulticlone(filepath.Join(originalDir, ".git"), filepath.Join(newDir, ".git")); err != nil {
		t.Fatalf("copy .git failed: %v", err)
	}
	if err := copyDirForMulticlone(beadsDir, newBeadsDir); err != nil {
		t.Fatalf("copy .beads failed: %v", err)
	}

	git.ResetCaches()

	// Verify beads still works in new location
	newDbPath := filepath.Join(newBeadsDir, "beads.db")
	newStore, err := sqlite.New(ctx, newDbPath)
	if err != nil {
		t.Fatalf("open store in new location failed: %v", err)
	}
	defer newStore.Close()

	// Should be able to read issues
	issues, err := newStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("search issues in new location failed: %v", err)
	}
	_ = issues // may be empty since we didn't import

	// Load JSONL should work
	newJsonlPath := filepath.Join(newBeadsDir, "issues.jsonl")
	loadedIssues, err := loadIssuesFromJSONL(newJsonlPath)
	if err != nil {
		t.Fatalf("load JSONL in new location failed: %v", err)
	}
	if len(loadedIssues) != 1 {
		t.Errorf("expected 1 issue from JSONL, got %d", len(loadedIssues))
	}
}

// Helper functions for multiclone tests (use unique names to avoid conflicts)

// configureGitInDirForMulticlone configures git user.email and user.name in a directory.
func configureGitInDirForMulticlone(t *testing.T, dir string) {
	t.Helper()
	_ = exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()
}

// exportToJSONLForMulticlone exports issues from store to JSONL file.
func exportToJSONLForMulticlone(ctx context.Context, store *sqlite.SQLiteStorage, jsonlPath string) error {
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
	if err != nil {
		return err
	}

	f, err := os.Create(jsonlPath)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetEscapeHTML(false)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			return err
		}
	}

	return nil
}

// copyDirForMulticlone copies a directory recursively.
func copyDirForMulticlone(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// compute destination path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}
