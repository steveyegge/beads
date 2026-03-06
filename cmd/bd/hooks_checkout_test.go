//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestRunPostCheckoutHookArgFiltering(t *testing.T) {
	t.Parallel()

	t.Run("flag=0 skips sync", func(t *testing.T) {
		t.Parallel()
		// flag=0 means file-level checkout — should return 0 immediately
		exitCode := runPostCheckoutHook([]string{"oldsha", "newsha", "0"})
		if exitCode != 0 {
			t.Errorf("exitCode = %d, want 0", exitCode)
		}
	})

	t.Run("short args skips sync", func(t *testing.T) {
		t.Parallel()
		exitCode := runPostCheckoutHook([]string{"oldsha", "newsha"})
		if exitCode != 0 {
			t.Errorf("exitCode = %d, want 0", exitCode)
		}
	})

	t.Run("empty args skips sync", func(t *testing.T) {
		t.Parallel()
		exitCode := runPostCheckoutHook([]string{})
		if exitCode != 0 {
			t.Errorf("exitCode = %d, want 0", exitCode)
		}
	})
}

func TestIsRebaseInProgress(t *testing.T) {
	t.Parallel()

	t.Run("no sentinel dirs", func(t *testing.T) {
		t.Parallel()
		// Save and restore working directory
		origDir, _ := os.Getwd()
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".git"), 0755)
		os.Chdir(dir)
		t.Cleanup(func() { os.Chdir(origDir) })

		if isRebaseInProgress() {
			t.Error("expected false with no sentinel dirs")
		}
	})

	t.Run("rebase-merge exists", func(t *testing.T) {
		t.Parallel()
		origDir, _ := os.Getwd()
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".git", "rebase-merge"), 0755)
		os.Chdir(dir)
		t.Cleanup(func() { os.Chdir(origDir) })

		if !isRebaseInProgress() {
			t.Error("expected true with .git/rebase-merge")
		}
	})

	t.Run("rebase-apply exists", func(t *testing.T) {
		t.Parallel()
		origDir, _ := os.Getwd()
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".git", "rebase-apply"), 0755)
		os.Chdir(dir)
		t.Cleanup(func() { os.Chdir(origDir) })

		if !isRebaseInProgress() {
			t.Error("expected true with .git/rebase-apply")
		}
	})
}

func TestPostCheckoutResetCorrectness(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := newTestStoreWithPrefix(t, dbPath, "ttravel")
	ctx := context.Background()

	ensureCleanGlobalState(t)
	enableTestModeGlobals()
	setStore(store)
	setStoreActive(true)
	rootCtx = ctx

	now := time.Now()
	beadsDir := filepath.Dir(store.Path())

	// === Phase 1: Create issue A with label, dependency stub, and comment ===
	issueA := &types.Issue{
		ID:          "ttravel-aaa",
		Title:       "Issue A (pre-checkpoint)",
		Description: "Should survive reset",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.CreateIssue(ctx, issueA, "test"); err != nil {
		t.Fatalf("create issue A: %v", err)
	}
	if err := store.AddLabel(ctx, "ttravel-aaa", "phase1", "test"); err != nil {
		t.Fatalf("add label A: %v", err)
	}
	if err := store.AddComment(ctx, "ttravel-aaa", "test", "Comment on A"); err != nil {
		t.Fatalf("add comment A: %v", err)
	}

	// Commit and record checkpoint hash
	if err := store.Commit(ctx, "checkpoint: issue A created"); err != nil {
		t.Fatalf("dolt commit checkpoint: %v", err)
	}
	checkpointHash, err := store.GetCurrentCommit(ctx)
	if err != nil {
		t.Fatalf("get checkpoint hash: %v", err)
	}

	// === Phase 2: Create issue B with label, dependency, and comment ===
	issueB := &types.Issue{
		ID:          "ttravel-bbb",
		Title:       "Issue B (post-checkpoint)",
		Description: "Should disappear after reset",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeFeature,
		CreatedAt:   now.Add(time.Second),
		UpdatedAt:   now.Add(time.Second),
	}
	if err := store.CreateIssue(ctx, issueB, "test"); err != nil {
		t.Fatalf("create issue B: %v", err)
	}
	if err := store.AddLabel(ctx, "ttravel-bbb", "phase2", "test"); err != nil {
		t.Fatalf("add label B: %v", err)
	}
	if err := store.AddComment(ctx, "ttravel-bbb", "test", "Comment on B"); err != nil {
		t.Fatalf("add comment B: %v", err)
	}
	dep := &types.Dependency{
		IssueID:     "ttravel-bbb",
		DependsOnID: "ttravel-aaa",
		Type:        types.DepBlocks,
		CreatedAt:   now.Add(time.Second),
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("add dependency B->A: %v", err)
	}

	// Commit and record latest hash
	if err := store.Commit(ctx, "latest: issue B created"); err != nil {
		t.Fatalf("dolt commit latest: %v", err)
	}
	latestHash, err := store.GetCurrentCommit(ctx)
	if err != nil {
		t.Fatalf("get latest hash: %v", err)
	}

	if checkpointHash == latestHash {
		t.Fatal("checkpoint and latest hashes should differ")
	}

	// Verify both issues exist before reset
	if _, err := store.GetIssue(ctx, "ttravel-aaa"); err != nil {
		t.Fatalf("issue A should exist before reset: %v", err)
	}
	if _, err := store.GetIssue(ctx, "ttravel-bbb"); err != nil {
		t.Fatalf("issue B should exist before reset: %v", err)
	}

	// === Reset backward: simulate git checkout to checkpoint ===
	// Write refs pointing to the checkpoint hash
	writeTestBeadsRefs(t, beadsDir, "main", checkpointHash)

	// Set BD_AUTO_SYNC=1 for non-interactive reset
	t.Setenv("BD_AUTO_SYNC", "1")

	postCheckoutBeadsSync()

	// === Verify backward reset ===

	// Issue B must be GONE
	if got, err := store.GetIssue(ctx, "ttravel-bbb"); err == nil {
		t.Errorf("issue B should be gone after reset, but found: %+v", got)
	}

	// Issue A must be PRESENT with correct fields
	gotA, err := store.GetIssue(ctx, "ttravel-aaa")
	if err != nil {
		t.Fatalf("issue A should survive reset: %v", err)
	}
	if gotA.Title != "Issue A (pre-checkpoint)" {
		t.Errorf("issue A title = %q, want %q", gotA.Title, "Issue A (pre-checkpoint)")
	}
	if gotA.Description != "Should survive reset" {
		t.Errorf("issue A description = %q, want %q", gotA.Description, "Should survive reset")
	}
	if gotA.Priority != 2 {
		t.Errorf("issue A priority = %d, want 2", gotA.Priority)
	}
	if gotA.IssueType != types.TypeTask {
		t.Errorf("issue A type = %q, want %q", gotA.IssueType, types.TypeTask)
	}
	if gotA.Status != types.StatusOpen {
		t.Errorf("issue A status = %q, want %q", gotA.Status, types.StatusOpen)
	}

	// Issue A's label must survive
	labelsA, err := store.GetLabels(ctx, "ttravel-aaa")
	if err != nil {
		t.Fatalf("get labels A after reset: %v", err)
	}
	if !containsString(labelsA, "phase1") {
		t.Errorf("issue A labels = %v, want to contain %q", labelsA, "phase1")
	}

	// Issue A's comment must survive
	commentsA, err := store.GetIssueComments(ctx, "ttravel-aaa")
	if err != nil {
		t.Fatalf("get comments A after reset: %v", err)
	}
	if len(commentsA) != 1 || commentsA[0].Text != "Comment on A" {
		t.Errorf("issue A comments after reset: got %d comments, want 1 with text %q", len(commentsA), "Comment on A")
	}

	// Issue B's label must be GONE
	labelsB, _ := store.GetLabels(ctx, "ttravel-bbb")
	if len(labelsB) > 0 {
		t.Errorf("issue B labels should be gone after reset, got %v", labelsB)
	}

	// Issue B's comment must be GONE
	commentsB, _ := store.GetIssueComments(ctx, "ttravel-bbb")
	if len(commentsB) > 0 {
		t.Errorf("issue B comments should be gone after reset, got %d", len(commentsB))
	}

	// Ref file should contain checkpoint hash
	refHash, refBranch := readBeadsRefs(beadsDir)
	if refHash != checkpointHash {
		t.Errorf("ref hash after backward reset = %q, want checkpoint %q", refHash, checkpointHash)
	}
	if refBranch != "main" {
		t.Errorf("ref branch = %q, want %q", refBranch, "main")
	}

	// === Reset forward: simulate git checkout back to latest ===
	writeTestBeadsRefs(t, beadsDir, "main", latestHash)
	postCheckoutBeadsSync()

	// === Verify forward reset (round-trip integrity) ===

	// Both issues must be present
	gotA2, err := store.GetIssue(ctx, "ttravel-aaa")
	if err != nil {
		t.Fatalf("issue A should exist after forward reset: %v", err)
	}
	gotB2, err := store.GetIssue(ctx, "ttravel-bbb")
	if err != nil {
		t.Fatalf("issue B should exist after forward reset: %v", err)
	}

	// Verify no field corruption on round-trip
	if gotA2.Title != "Issue A (pre-checkpoint)" {
		t.Errorf("round-trip: issue A title = %q", gotA2.Title)
	}
	if gotB2.Title != "Issue B (post-checkpoint)" {
		t.Errorf("round-trip: issue B title = %q", gotB2.Title)
	}
	if gotB2.Priority != 1 {
		t.Errorf("round-trip: issue B priority = %d, want 1", gotB2.Priority)
	}
	if gotB2.IssueType != types.TypeFeature {
		t.Errorf("round-trip: issue B type = %q, want %q", gotB2.IssueType, types.TypeFeature)
	}

	// All labels restored
	labelsA2, _ := store.GetLabels(ctx, "ttravel-aaa")
	labelsB2, _ := store.GetLabels(ctx, "ttravel-bbb")
	if !containsString(labelsA2, "phase1") {
		t.Errorf("round-trip: issue A labels = %v, missing %q", labelsA2, "phase1")
	}
	if !containsString(labelsB2, "phase2") {
		t.Errorf("round-trip: issue B labels = %v, missing %q", labelsB2, "phase2")
	}

	// All comments restored (no duplicates)
	commentsA2, _ := store.GetIssueComments(ctx, "ttravel-aaa")
	commentsB2, _ := store.GetIssueComments(ctx, "ttravel-bbb")
	if len(commentsA2) != 1 {
		t.Errorf("round-trip: issue A has %d comments, want 1 (no duplicates)", len(commentsA2))
	}
	if len(commentsB2) != 1 {
		t.Errorf("round-trip: issue B has %d comments, want 1 (no duplicates)", len(commentsB2))
	}

	// Dependency restored
	depsB2, _ := store.GetDependencies(ctx, "ttravel-bbb")
	if len(depsB2) != 1 {
		t.Errorf("round-trip: issue B has %d dependencies, want 1", len(depsB2))
	}

	// Ref file should contain latest hash
	refHash2, _ := readBeadsRefs(beadsDir)
	if refHash2 != latestHash {
		t.Errorf("ref hash after forward reset = %q, want latest %q", refHash2, latestHash)
	}
}

func TestPostCheckoutBeadsSyncInSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := newTestStoreWithPrefix(t, dbPath, "sync")
	ctx := context.Background()

	ensureCleanGlobalState(t)
	enableTestModeGlobals()
	setStore(store)
	setStoreActive(true)
	rootCtx = ctx

	beadsDir := filepath.Dir(store.Path())

	// Commit something so we have a hash
	if err := store.Commit(ctx, "initial"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	currentHash, err := store.GetCurrentCommit(ctx)
	if err != nil {
		t.Fatalf("get hash: %v", err)
	}

	// Write refs matching current state
	writeTestBeadsRefs(t, beadsDir, "main", currentHash)

	t.Setenv("BD_AUTO_SYNC", "1")
	postCheckoutBeadsSync()

	// Hash should be unchanged — no reset occurred
	afterHash, err := store.GetCurrentCommit(ctx)
	if err != nil {
		t.Fatalf("get hash after sync: %v", err)
	}
	if afterHash != currentHash {
		t.Errorf("hash changed from %q to %q — should not reset when in sync", currentHash, afterHash)
	}
}

func TestPostCheckoutBeadsSyncNoRefs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := newTestStoreWithPrefix(t, dbPath, "norefs")
	ctx := context.Background()

	ensureCleanGlobalState(t)
	enableTestModeGlobals()
	setStore(store)
	setStoreActive(true)
	rootCtx = ctx

	// Don't write any ref files — postCheckoutBeadsSync should skip gracefully
	t.Setenv("BD_AUTO_SYNC", "1")
	postCheckoutBeadsSync() // should not panic
}

// writeTestBeadsRefs writes .beads/HEAD and .beads/refs/heads/<branch> for testing.
func writeTestBeadsRefs(t *testing.T, beadsDir, branch, hash string) {
	t.Helper()
	headPath := filepath.Join(beadsDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/"+branch+"\n"), 0644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
	refsDir := filepath.Join(beadsDir, "refs", "heads")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatalf("mkdir refs: %v", err)
	}
	refPath := filepath.Join(refsDir, branch)
	if err := os.WriteFile(refPath, []byte(hash+"\n"), 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
}

// containsString checks if a string slice contains a value.
func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
