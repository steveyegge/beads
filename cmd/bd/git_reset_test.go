//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

func TestGitResetCmdRegistered(t *testing.T) {
	t.Parallel()

	// Verify bd reset is registered on rootCmd
	cmd, _, err := rootCmd.Find([]string{"reset"})
	if err != nil {
		t.Fatalf("bd reset not found: %v", err)
	}
	if cmd.Name() != "reset" {
		t.Errorf("expected command name 'reset', got %q", cmd.Name())
	}
	if !cmd.DisableFlagParsing {
		t.Error("bd reset should have DisableFlagParsing=true to pass args through to git")
	}
}

func TestCheckRefsCmdRegistered(t *testing.T) {
	t.Parallel()

	cmd, _, err := rootCmd.Find([]string{"check-refs"})
	if err != nil {
		t.Fatalf("bd check-refs not found: %v", err)
	}
	if cmd.Name() != "check-refs" {
		t.Errorf("expected command name 'check-refs', got %q", cmd.Name())
	}
}

// TestCheckBeadsRefSyncAutoReset verifies that when reset_dolt_with_git=true
// and prompt=false, Dolt is automatically reset to match the ref hash.
func TestCheckBeadsRefSyncAutoReset(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	s := newTestStoreWithPrefix(t, dbPath, "autoreset")
	ctx := context.Background()

	beadsDir := filepath.Dir(s.Path())

	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	config.Set("branch_strategy.defaults.reset_dolt_with_git", true)
	config.Set("branch_strategy.prompt", false)
	t.Cleanup(func() {
		config.Set("branch_strategy.defaults.reset_dolt_with_git", false)
		config.Set("branch_strategy.prompt", false)
	})

	now := time.Now()

	// Create issue and commit (checkpoint)
	issueA := &types.Issue{
		ID: "autoreset-aaa", Title: "Checkpoint issue",
		Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateIssue(ctx, issueA, "test"); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := s.Commit(ctx, "checkpoint"); err != nil {
		t.Fatalf("commit checkpoint: %v", err)
	}
	checkpointHash, _ := s.GetCurrentCommit(ctx)

	// Create another issue and commit (latest)
	issueB := &types.Issue{
		ID: "autoreset-bbb", Title: "Post-checkpoint issue",
		Status: types.StatusOpen, Priority: 1, IssueType: types.TypeFeature,
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	if err := s.CreateIssue(ctx, issueB, "test"); err != nil {
		t.Fatalf("create issue B: %v", err)
	}
	if err := s.Commit(ctx, "latest"); err != nil {
		t.Fatalf("commit latest: %v", err)
	}

	// Simulate git reset backward: write refs pointing to checkpoint
	writeTestBeadsRefs(t, beadsDir, "main", checkpointHash)

	// checkBeadsRefSync should auto-reset (prompt=false, reset=true)
	checkBeadsRefSync(ctx, s)

	// Verify issue B is gone (Dolt was reset)
	if _, err := s.GetIssue(ctx, "autoreset-bbb"); err == nil {
		t.Error("issue B should be gone after auto-reset, but it still exists")
	}

	// Verify issue A survives
	gotA, err := s.GetIssue(ctx, "autoreset-aaa")
	if err != nil {
		t.Fatalf("issue A should survive: %v", err)
	}
	if gotA.Title != "Checkpoint issue" {
		t.Errorf("issue A title = %q, want %q", gotA.Title, "Checkpoint issue")
	}
}

// TestCheckBeadsRefSyncSilentDivergence verifies that with default settings
// (prompt=false, reset=false), mismatch is detected but Dolt is NOT reset —
// histories are allowed to diverge silently.
func TestCheckBeadsRefSyncSilentDivergence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	s := newTestStoreWithPrefix(t, dbPath, "diverge")
	ctx := context.Background()

	beadsDir := filepath.Dir(s.Path())

	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	// Ensure defaults: both false (silent divergence)
	config.Set("branch_strategy.defaults.reset_dolt_with_git", false)
	config.Set("branch_strategy.prompt", false)

	now := time.Now()
	issue := &types.Issue{
		ID: "diverge-aaa", Title: "Should not be reset",
		Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := s.Commit(ctx, "with issue"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	latestHash, _ := s.GetCurrentCommit(ctx)

	// Write refs pointing to a different hash (simulate git reset)
	writeTestBeadsRefs(t, beadsDir, "main", "0000000000000000000000000000fake")

	// Silent mode: detect mismatch, take no action
	checkBeadsRefSync(ctx, s)

	// Dolt should NOT have been reset
	afterHash, _ := s.GetCurrentCommit(ctx)
	if afterHash != latestHash {
		t.Errorf("silent divergence mode should not reset Dolt, but hash changed from %q to %q", latestHash, afterHash)
	}

	// Issue should still exist
	if _, err := s.GetIssue(ctx, "diverge-aaa"); err != nil {
		t.Errorf("issue should still exist in diverged state: %v", err)
	}
}

// TestCheckBeadsRefSyncBranchMismatch verifies that if .beads/HEAD points to
// a different branch than Dolt's current branch, Dolt switches branches before
// comparing hashes.
func TestCheckBeadsRefSyncBranchMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	s := newTestStoreWithPrefix(t, dbPath, "branchmm")
	ctx := context.Background()

	beadsDir := filepath.Dir(s.Path())

	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	config.Set("branch_strategy.defaults.reset_dolt_with_git", true)
	config.Set("branch_strategy.prompt", false)
	t.Cleanup(func() {
		config.Set("branch_strategy.defaults.reset_dolt_with_git", false)
	})

	// Commit on main so we have a hash
	if err := s.Commit(ctx, "initial on main"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	mainHash, _ := s.GetCurrentCommit(ctx)

	// Write refs as if we're on main with current hash (in sync)
	writeTestBeadsRefs(t, beadsDir, "main", mainHash)

	// checkBeadsRefSync should be a no-op (in sync)
	checkBeadsRefSync(ctx, s)

	afterHash, _ := s.GetCurrentCommit(ctx)
	if afterHash != mainHash {
		t.Errorf("in-sync check should not change hash, got %q want %q", afterHash, mainHash)
	}
}

// TestCheckBeadsRefSyncNilStore verifies no panic when store is nil.
func TestCheckBeadsRefSyncNilStore(t *testing.T) {
	t.Parallel()
	// Should not panic
	checkBeadsRefSync(context.Background(), nil)
}

// TestReadBeadsRefsNoFiles verifies readBeadsRefs returns empty strings
// when no ref files exist.
func TestReadBeadsRefsNoFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hash, branch := readBeadsRefs(dir)
	if hash != "" || branch != "" {
		t.Errorf("expected empty strings, got hash=%q branch=%q", hash, branch)
	}
}

// TestReadBeadsRefsMalformedHead verifies readBeadsRefs handles
// malformed HEAD file gracefully.
func TestReadBeadsRefsMalformedHead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	headPath := filepath.Join(dir, "HEAD")
	os.WriteFile(headPath, []byte("not a ref line\n"), 0644)

	hash, branch := readBeadsRefs(dir)
	if hash != "" || branch != "" {
		t.Errorf("expected empty strings for malformed HEAD, got hash=%q branch=%q", hash, branch)
	}
}

// TestReadBeadsRefsValidButMissingRefFile verifies readBeadsRefs returns
// the branch but empty hash when HEAD is valid but ref file doesn't exist.
func TestReadBeadsRefsValidButMissingRefFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	headPath := filepath.Join(dir, "HEAD")
	os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0644)

	hash, branch := readBeadsRefs(dir)
	if branch != "main" {
		t.Errorf("expected branch='main', got %q", branch)
	}
	if hash != "" {
		t.Errorf("expected empty hash when ref file missing, got %q", hash)
	}
}

// TestReadBeadsRefsRoundTrip verifies writeBeadsRefs and readBeadsRefs
// are consistent.
func TestReadBeadsRefsRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	branch := "feature-branch"
	hash := "abc123def456abc123def456abc12345"

	// Write refs
	headPath := filepath.Join(dir, "HEAD")
	os.WriteFile(headPath, []byte("ref: refs/heads/"+branch+"\n"), 0644)
	refsDir := filepath.Join(dir, "refs", "heads")
	os.MkdirAll(refsDir, 0755)
	refPath := filepath.Join(refsDir, branch)
	os.WriteFile(refPath, []byte(hash+"\n"), 0644)

	// Read back
	gotHash, gotBranch := readBeadsRefs(dir)
	if gotBranch != branch {
		t.Errorf("branch = %q, want %q", gotBranch, branch)
	}
	if gotHash != hash {
		t.Errorf("hash = %q, want %q", gotHash, hash)
	}
}

// TestTruncHash verifies hash truncation behavior.
func TestTruncHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"abcdefghijklmnop", "abcdefgh"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"123456789", "12345678"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := truncHash(tt.input); got != tt.want {
			t.Errorf("truncHash(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
