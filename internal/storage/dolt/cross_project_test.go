// Package dolt provides cross-project data isolation tests for shared Dolt servers.
//
// These tests validate that when multiple projects share a single Dolt server
// (whether by design in shared-server mode, or by accident via port collision),
// each project's data remains fully isolated. This is the core concern raised
// in GH#2372: cross-project data leakage when two projects connect to the same
// Dolt server.
//
// The test matrix covers:
//   - Different issue prefixes (the common case)
//   - Same issue prefix (worst case for collision detection)
//   - Concurrent writes from both projects simultaneously
//   - Read isolation (project A never sees project B's issues)
package dolt

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// setupTwoProjectStores creates two DoltStore instances on the same server,
// each with its own database and issue prefix — simulating two beads projects
// sharing a Dolt server (either intentionally or via port collision).
func setupTwoProjectStores(t *testing.T, prefixA, prefixB string) (storeA, storeB *DoltStore, cleanup func()) {
	t.Helper()
	skipIfNoDolt(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	tmpDirA, err := os.MkdirTemp("", "dolt-project-a-*")
	if err != nil {
		t.Fatalf("failed to create temp dir for project A: %v", err)
	}
	tmpDirB, err := os.MkdirTemp("", "dolt-project-b-*")
	if err != nil {
		os.RemoveAll(tmpDirA)
		t.Fatalf("failed to create temp dir for project B: %v", err)
	}

	dbNameA := uniqueTestDBName(t) + "_a"
	dbNameB := uniqueTestDBName(t) + "_b"

	cfgA := &Config{
		Path:            tmpDirA,
		CommitterName:   "project-a",
		CommitterEmail:  "a@test.com",
		Database:        dbNameA,
		CreateIfMissing: true,
	}
	cfgB := &Config{
		Path:            tmpDirB,
		CommitterName:   "project-b",
		CommitterEmail:  "b@test.com",
		Database:        dbNameB,
		CreateIfMissing: true,
	}

	storeA, err = New(ctx, cfgA)
	if err != nil {
		os.RemoveAll(tmpDirA)
		os.RemoveAll(tmpDirB)
		t.Fatalf("failed to create store A: %v", err)
	}

	if err := storeA.SetConfig(ctx, "issue_prefix", prefixA); err != nil {
		storeA.Close()
		os.RemoveAll(tmpDirA)
		os.RemoveAll(tmpDirB)
		t.Fatalf("failed to set prefix for project A: %v", err)
	}

	storeB, err = New(ctx, cfgB)
	if err != nil {
		storeA.Close()
		os.RemoveAll(tmpDirA)
		os.RemoveAll(tmpDirB)
		t.Fatalf("failed to create store B: %v", err)
	}

	if err := storeB.SetConfig(ctx, "issue_prefix", prefixB); err != nil {
		storeA.Close()
		storeB.Close()
		os.RemoveAll(tmpDirA)
		os.RemoveAll(tmpDirB)
		t.Fatalf("failed to set prefix for project B: %v", err)
	}

	cleanup = func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_, _ = storeA.db.ExecContext(dropCtx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbNameA))
		_, _ = storeB.db.ExecContext(dropCtx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbNameB))
		storeA.Close()
		storeB.Close()
		os.RemoveAll(tmpDirA)
		os.RemoveAll(tmpDirB)
	}

	return storeA, storeB, cleanup
}

// =============================================================================
// Test 1: Basic Read Isolation — Different Prefixes
// Two projects with different prefixes write issues, then each reads.
// Verify: Project A sees only its issues, project B sees only its issues.
// =============================================================================

func TestCrossProject_ReadIsolation_DifferentPrefixes(t *testing.T) {
	storeA, storeB, cleanup := setupTwoProjectStores(t, "alpha", "beta")
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Project A creates issues
	const numIssuesPerProject = 5
	aIDs := make([]string, numIssuesPerProject)
	for i := 0; i < numIssuesPerProject; i++ {
		issue := &types.Issue{
			Title:       fmt.Sprintf("Alpha Issue %d", i),
			Description: fmt.Sprintf("Created by project A, issue %d", i),
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := storeA.CreateIssue(ctx, issue, "project-a"); err != nil {
			t.Fatalf("project A failed to create issue %d: %v", i, err)
		}
		aIDs[i] = issue.ID
	}

	// Project B creates issues
	bIDs := make([]string, numIssuesPerProject)
	for i := 0; i < numIssuesPerProject; i++ {
		issue := &types.Issue{
			Title:       fmt.Sprintf("Beta Issue %d", i),
			Description: fmt.Sprintf("Created by project B, issue %d", i),
			Status:      types.StatusOpen,
			Priority:    3,
			IssueType:   types.TypeTask,
		}
		if err := storeB.CreateIssue(ctx, issue, "project-b"); err != nil {
			t.Fatalf("project B failed to create issue %d: %v", i, err)
		}
		bIDs[i] = issue.ID
	}

	// Verify: project A sees only its issues
	allA, err := storeA.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("project A SearchIssues failed: %v", err)
	}
	if len(allA) != numIssuesPerProject {
		t.Errorf("project A: expected %d issues, got %d", numIssuesPerProject, len(allA))
	}
	for _, issue := range allA {
		if issue.Description == "" {
			continue
		}
		if issue.Description[0:10] != "Created by" {
			continue
		}
		if issue.Description != fmt.Sprintf("Created by project A, issue %s", issue.Description[len(issue.Description)-1:]) {
			// Simpler check: no issue should mention project B
			for _, bID := range bIDs {
				if issue.ID == bID {
					t.Errorf("project A sees project B's issue: %s", bID)
				}
			}
		}
	}

	// Verify: project B sees only its issues
	allB, err := storeB.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("project B SearchIssues failed: %v", err)
	}
	if len(allB) != numIssuesPerProject {
		t.Errorf("project B: expected %d issues, got %d", numIssuesPerProject, len(allB))
	}

	// Cross-check: A's issues must not appear in B, B's in A
	aIDSet := make(map[string]bool)
	for _, id := range aIDs {
		aIDSet[id] = true
	}
	bIDSet := make(map[string]bool)
	for _, id := range bIDs {
		bIDSet[id] = true
	}

	for _, issue := range allA {
		if bIDSet[issue.ID] {
			t.Errorf("DATA LEAKAGE: project A contains project B's issue %s", issue.ID)
		}
	}
	for _, issue := range allB {
		if aIDSet[issue.ID] {
			t.Errorf("DATA LEAKAGE: project B contains project A's issue %s", issue.ID)
		}
	}

	t.Logf("Isolation verified: A has %d issues, B has %d issues, zero cross-contamination", len(allA), len(allB))
}

// =============================================================================
// Test 2: Read Isolation — Same Prefix
// Two projects with the SAME prefix (worst case for collision detection).
// Verify: Even with identical prefixes, each project sees only its own data.
// =============================================================================

func TestCrossProject_ReadIsolation_SamePrefix(t *testing.T) {
	storeA, storeB, cleanup := setupTwoProjectStores(t, "beads", "beads")
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Both projects create issues with the same prefix
	issueA := &types.Issue{
		Title:       "Project A Issue",
		Description: "Belongs to project A only",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeBug,
	}
	if err := storeA.CreateIssue(ctx, issueA, "project-a"); err != nil {
		t.Fatalf("project A create failed: %v", err)
	}

	issueB := &types.Issue{
		Title:       "Project B Issue",
		Description: "Belongs to project B only",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeBug,
	}
	if err := storeB.CreateIssue(ctx, issueB, "project-b"); err != nil {
		t.Fatalf("project B create failed: %v", err)
	}

	// Project A should see exactly 1 issue
	allA, err := storeA.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("project A SearchIssues failed: %v", err)
	}
	if len(allA) != 1 {
		t.Errorf("project A: expected 1 issue, got %d", len(allA))
		for _, issue := range allA {
			t.Logf("  project A sees: %s %q", issue.ID, issue.Title)
		}
	}

	// Project B should see exactly 1 issue
	allB, err := storeB.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("project B SearchIssues failed: %v", err)
	}
	if len(allB) != 1 {
		t.Errorf("project B: expected 1 issue, got %d", len(allB))
		for _, issue := range allB {
			t.Logf("  project B sees: %s %q", issue.ID, issue.Title)
		}
	}

	// Cross-check: IDs must differ
	if issueA.ID == issueB.ID {
		t.Errorf("COLLISION: both projects generated the same issue ID: %s", issueA.ID)
	}

	// Verify each project cannot fetch the other's issue by ID
	ghostB, err := storeA.GetIssue(ctx, issueB.ID)
	if err == nil && ghostB != nil {
		t.Errorf("DATA LEAKAGE: project A can fetch project B's issue %s", issueB.ID)
	}
	ghostA, err := storeB.GetIssue(ctx, issueA.ID)
	if err == nil && ghostA != nil {
		t.Errorf("DATA LEAKAGE: project B can fetch project A's issue %s", issueA.ID)
	}

	t.Logf("Same-prefix isolation verified: A=%s, B=%s", issueA.ID, issueB.ID)
}

// =============================================================================
// Test 3: Concurrent Cross-Project Writes
// Both projects write issues simultaneously via goroutines.
// Verify: No cross-contamination under concurrent load.
// =============================================================================

func TestCrossProject_ConcurrentWrites(t *testing.T) {
	storeA, storeB, cleanup := setupTwoProjectStores(t, "proj-a", "proj-b")
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	const numIssuesPerProject = 10
	const maxRetries = 5

	var wg sync.WaitGroup
	aIDs := make(chan string, numIssuesPerProject)
	bIDs := make(chan string, numIssuesPerProject)
	errs := make(chan error, numIssuesPerProject*2)

	// Project A writes concurrently
	for i := 0; i < numIssuesPerProject; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for attempt := 0; attempt <= maxRetries; attempt++ {
				issue := &types.Issue{
					Title:       fmt.Sprintf("A-concurrent-%d", n),
					Description: fmt.Sprintf("Project A goroutine %d", n),
					Status:      types.StatusOpen,
					Priority:    2,
					IssueType:   types.TypeTask,
				}
				err := storeA.CreateIssue(ctx, issue, fmt.Sprintf("a-worker-%d", n))
				if err == nil {
					aIDs <- issue.ID
					return
				}
				if isSerializationError(err) && attempt < maxRetries {
					time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
					continue
				}
				errs <- fmt.Errorf("project A goroutine %d: %w", n, err)
				return
			}
		}(i)
	}

	// Project B writes concurrently (at the same time as A)
	for i := 0; i < numIssuesPerProject; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for attempt := 0; attempt <= maxRetries; attempt++ {
				issue := &types.Issue{
					Title:       fmt.Sprintf("B-concurrent-%d", n),
					Description: fmt.Sprintf("Project B goroutine %d", n),
					Status:      types.StatusOpen,
					Priority:    3,
					IssueType:   types.TypeTask,
				}
				err := storeB.CreateIssue(ctx, issue, fmt.Sprintf("b-worker-%d", n))
				if err == nil {
					bIDs <- issue.ID
					return
				}
				if isSerializationError(err) && attempt < maxRetries {
					time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
					continue
				}
				errs <- fmt.Errorf("project B goroutine %d: %w", n, err)
				return
			}
		}(i)
	}

	// Wait with deadlock detection
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-ctx.Done():
		t.Fatal("timeout — possible deadlock during concurrent cross-project writes")
	}

	close(aIDs)
	close(bIDs)
	close(errs)

	// Report errors
	var errCount int
	for err := range errs {
		t.Errorf("write error: %v", err)
		errCount++
	}
	if errCount > 0 {
		t.Fatalf("%d goroutines failed", errCount)
	}

	// Collect IDs
	aIDSet := make(map[string]bool)
	for id := range aIDs {
		aIDSet[id] = true
	}
	bIDSet := make(map[string]bool)
	for id := range bIDs {
		bIDSet[id] = true
	}

	t.Logf("Created: %d issues in A, %d issues in B", len(aIDSet), len(bIDSet))

	// Verify isolation after concurrent writes
	allA, err := storeA.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("project A SearchIssues failed: %v", err)
	}
	allB, err := storeB.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("project B SearchIssues failed: %v", err)
	}

	if len(allA) != numIssuesPerProject {
		t.Errorf("project A: expected %d issues, got %d", numIssuesPerProject, len(allA))
	}
	if len(allB) != numIssuesPerProject {
		t.Errorf("project B: expected %d issues, got %d", numIssuesPerProject, len(allB))
	}

	// Cross-contamination check
	var leakCount int
	for _, issue := range allA {
		if bIDSet[issue.ID] {
			t.Errorf("DATA LEAKAGE: project A contains project B's issue %s", issue.ID)
			leakCount++
		}
	}
	for _, issue := range allB {
		if aIDSet[issue.ID] {
			t.Errorf("DATA LEAKAGE: project B contains project A's issue %s", issue.ID)
			leakCount++
		}
	}

	if leakCount == 0 {
		t.Logf("Concurrent isolation verified: %d+%d issues, zero leakage", len(allA), len(allB))
	}
}

// =============================================================================
// Test 4: Concurrent Read-Write Mix Across Projects
// Project A writes while project B reads (and vice versa).
// Verify: Reads never return the other project's data.
// =============================================================================

func TestCrossProject_ConcurrentReadWriteMix(t *testing.T) {
	storeA, storeB, cleanup := setupTwoProjectStores(t, "reader", "writer")
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Seed both projects with initial issues
	const seedIssues = 3
	for i := 0; i < seedIssues; i++ {
		issueA := &types.Issue{
			Title:       fmt.Sprintf("A-seed-%d", i),
			Description: "Seeded by project A",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := storeA.CreateIssue(ctx, issueA, "seed-a"); err != nil {
			t.Fatalf("seed A issue %d: %v", i, err)
		}
		issueB := &types.Issue{
			Title:       fmt.Sprintf("B-seed-%d", i),
			Description: "Seeded by project B",
			Status:      types.StatusOpen,
			Priority:    3,
			IssueType:   types.TypeTask,
		}
		if err := storeB.CreateIssue(ctx, issueB, "seed-b"); err != nil {
			t.Fatalf("seed B issue %d: %v", i, err)
		}
	}

	const iterations = 20
	var wg sync.WaitGroup
	var leakDetected atomic.Int32

	// A writes, B reads simultaneously
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			issue := &types.Issue{
				Title:       fmt.Sprintf("A-concurrent-write-%d", i),
				Description: "Written by A during concurrent read-write",
				Status:      types.StatusOpen,
				Priority:    2,
				IssueType:   types.TypeTask,
			}
			if err := storeA.CreateIssue(ctx, issue, "a-writer"); err != nil {
				if !isSerializationError(err) {
					t.Logf("A write error (non-serialization): %v", err)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			issues, err := storeB.SearchIssues(ctx, "", types.IssueFilter{})
			if err != nil {
				continue
			}
			for _, issue := range issues {
				if issue.Description == "Written by A during concurrent read-write" ||
					issue.Description == "Seeded by project A" {
					leakDetected.Add(1)
					t.Errorf("DATA LEAKAGE: project B read contains project A's issue: %s %q",
						issue.ID, issue.Title)
				}
			}
		}
	}()

	// B writes, A reads simultaneously
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			issue := &types.Issue{
				Title:       fmt.Sprintf("B-concurrent-write-%d", i),
				Description: "Written by B during concurrent read-write",
				Status:      types.StatusOpen,
				Priority:    3,
				IssueType:   types.TypeTask,
			}
			if err := storeB.CreateIssue(ctx, issue, "b-writer"); err != nil {
				if !isSerializationError(err) {
					t.Logf("B write error (non-serialization): %v", err)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			issues, err := storeA.SearchIssues(ctx, "", types.IssueFilter{})
			if err != nil {
				continue
			}
			for _, issue := range issues {
				if issue.Description == "Written by B during concurrent read-write" ||
					issue.Description == "Seeded by project B" {
					leakDetected.Add(1)
					t.Errorf("DATA LEAKAGE: project A read contains project B's issue: %s %q",
						issue.ID, issue.Title)
				}
			}
		}
	}()

	// Wait with timeout
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		// OK
	case <-ctx.Done():
		t.Fatal("timeout — possible deadlock during concurrent read-write mix")
	}

	if leakDetected.Load() == 0 {
		t.Log("Concurrent read-write isolation verified: zero cross-project leakage")
	} else {
		t.Errorf("TOTAL LEAKAGE EVENTS: %d", leakDetected.Load())
	}
}
