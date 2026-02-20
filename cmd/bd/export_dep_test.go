//go:build cgo

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestExportDependencyPopulation(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	ctx := context.Background()

	// Create an epic tree: epic -> task1, epic -> task2, task1 -> subtask
	issues := []*types.Issue{
		{ID: "test-epic", Title: "Epic", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic, CreatedAt: time.Now()},
		{ID: "test-task1", Title: "Task 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask, CreatedAt: time.Now()},
		{ID: "test-task2", Title: "Task 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask, CreatedAt: time.Now()},
		{ID: "test-subtask", Title: "Subtask", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask, CreatedAt: time.Now()},
	}
	for _, issue := range issues {
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("CreateIssue(%s) failed: %v", issue.ID, err)
		}
	}

	// Dependencies: task1 blocks epic, task2 blocks epic, subtask blocks task1
	// (IssueID depends on DependsOnID, so DependsOnID is the parent)
	deps := []*types.Dependency{
		{IssueID: "test-task1", DependsOnID: "test-epic", Type: types.DepBlocks, CreatedAt: time.Now()},
		{IssueID: "test-task2", DependsOnID: "test-epic", Type: types.DepBlocks, CreatedAt: time.Now()},
		{IssueID: "test-subtask", DependsOnID: "test-task1", Type: types.DepBlocks, CreatedAt: time.Now()},
	}
	for _, dep := range deps {
		if err := s.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("AddDependency(%s -> %s) failed: %v", dep.IssueID, dep.DependsOnID, err)
		}
	}

	t.Run("parent_sees_child_dependencies", func(t *testing.T) {
		allDeps, err := s.GetAllDependencyRecords(ctx)
		if err != nil {
			t.Fatalf("GetAllDependencyRecords failed: %v", err)
		}

		// Build reverse index (same logic as export.go)
		parentDeps := make(map[string][]*types.Dependency)
		for _, depList := range allDeps {
			for _, dep := range depList {
				parentDeps[dep.DependsOnID] = append(parentDeps[dep.DependsOnID], dep)
			}
		}

		// Populate both directions (same logic as export.go)
		issueMap := make(map[string]*types.Issue)
		for _, issue := range issues {
			issue.Dependencies = nil // reset
			issueMap[issue.ID] = issue
		}
		for _, issue := range issues {
			if childDeps, ok := allDeps[issue.ID]; ok {
				issue.Dependencies = append(issue.Dependencies, childDeps...)
			}
			if parentChildDeps, ok := parentDeps[issue.ID]; ok {
				issue.Dependencies = append(issue.Dependencies, parentChildDeps...)
			}
		}

		// Epic: no child deps (it's never an IssueID), but 2 parent deps (task1, task2 depend on it)
		epic := issueMap["test-epic"]
		if len(epic.Dependencies) != 2 {
			t.Errorf("epic: got %d dependencies, want 2", len(epic.Dependencies))
		}

		// Task1: 1 child dep (on epic) + 1 parent dep (subtask depends on it) = 2
		task1 := issueMap["test-task1"]
		if len(task1.Dependencies) != 2 {
			t.Errorf("task1: got %d dependencies, want 2", len(task1.Dependencies))
		}

		// Task2: 1 child dep (on epic), no parent deps = 1
		task2 := issueMap["test-task2"]
		if len(task2.Dependencies) != 1 {
			t.Errorf("task2: got %d dependencies, want 1", len(task2.Dependencies))
		}

		// Subtask: 1 child dep (on task1), no parent deps = 1
		subtask := issueMap["test-subtask"]
		if len(subtask.Dependencies) != 1 {
			t.Errorf("subtask: got %d dependencies, want 1", len(subtask.Dependencies))
		}
	})

	t.Run("old_logic_loses_parent_deps", func(t *testing.T) {
		// Demonstrate the bug: the old code only populated child direction
		allDeps, err := s.GetAllDependencyRecords(ctx)
		if err != nil {
			t.Fatalf("GetAllDependencyRecords failed: %v", err)
		}

		// Old logic: only allDeps[issue.ID] (child direction)
		for _, issue := range issues {
			issue.Dependencies = nil // reset
		}
		issueMap := make(map[string]*types.Issue)
		for _, issue := range issues {
			issueMap[issue.ID] = issue
			issue.Dependencies = allDeps[issue.ID]
		}

		// With old logic, epic gets 0 deps (it's never an IssueID in the dependency table)
		epic := issueMap["test-epic"]
		if len(epic.Dependencies) != 0 {
			t.Errorf("old logic: epic got %d dependencies, want 0 (demonstrates the bug is real)", len(epic.Dependencies))
		}

		// task1 gets only 1 dep (its child dep on epic), missing the subtask parent dep
		task1 := issueMap["test-task1"]
		if len(task1.Dependencies) != 1 {
			t.Errorf("old logic: task1 got %d dependencies, want 1 (missing parent dep)", len(task1.Dependencies))
		}
	})

	t.Run("no_duplicate_deps_for_leaf_nodes", func(t *testing.T) {
		allDeps, err := s.GetAllDependencyRecords(ctx)
		if err != nil {
			t.Fatalf("GetAllDependencyRecords failed: %v", err)
		}

		parentDeps := make(map[string][]*types.Dependency)
		for _, depList := range allDeps {
			for _, dep := range depList {
				parentDeps[dep.DependsOnID] = append(parentDeps[dep.DependsOnID], dep)
			}
		}

		// Subtask is a leaf node: only appears as IssueID, never as DependsOnID
		// Should get exactly 1 dep, no duplicates from both directions
		subtask := &types.Issue{ID: "test-subtask"}
		subtask.Dependencies = nil
		if childDeps, ok := allDeps[subtask.ID]; ok {
			subtask.Dependencies = append(subtask.Dependencies, childDeps...)
		}
		if parentChildDeps, ok := parentDeps[subtask.ID]; ok {
			subtask.Dependencies = append(subtask.Dependencies, parentChildDeps...)
		}

		if len(subtask.Dependencies) != 1 {
			t.Errorf("leaf node: got %d dependencies, want 1 (no duplicates)", len(subtask.Dependencies))
		}
	})
}
