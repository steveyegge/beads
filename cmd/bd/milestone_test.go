//go:build cgo

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestMilestoneCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)

	targetDate := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	ms := &types.Milestone{
		Name:        "v1.0-beta",
		TargetDate:  &targetDate,
		Description: "Beta release",
	}

	t.Run("CreateAndGet", func(t *testing.T) {
		if err := s.CreateMilestone(ctx, ms, "test"); err != nil {
			t.Fatalf("CreateMilestone: %v", err)
		}
		got, err := s.GetMilestone(ctx, "v1.0-beta")
		if err != nil {
			t.Fatalf("GetMilestone: %v", err)
		}
		if got.Name != "v1.0-beta" {
			t.Errorf("Name = %q, want %q", got.Name, "v1.0-beta")
		}
		if got.Description != "Beta release" {
			t.Errorf("Description = %q, want %q", got.Description, "Beta release")
		}
	})

	t.Run("List", func(t *testing.T) {
		milestones, err := s.ListMilestones(ctx)
		if err != nil {
			t.Fatalf("ListMilestones: %v", err)
		}
		if len(milestones) != 1 {
			t.Errorf("len = %d, want 1", len(milestones))
		}
	})

	t.Run("LinkIssueToMilestone", func(t *testing.T) {
		issue := &types.Issue{
			Title: "Milestone task", Status: types.StatusOpen,
			Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now(),
		}
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		if err := s.UpdateIssue(ctx, issue.ID, map[string]interface{}{
			"milestone": "v1.0-beta",
		}, "test"); err != nil {
			t.Fatalf("UpdateIssue: %v", err)
		}

		// Filter by milestone
		msName := "v1.0-beta"
		filter := types.IssueFilter{Milestone: &msName}
		issues, err := s.SearchIssues(ctx, "", filter)
		if err != nil {
			t.Fatalf("SearchIssues: %v", err)
		}
		if len(issues) != 1 {
			t.Errorf("got %d issues, want 1", len(issues))
		}
	})

	t.Run("DeleteMilestone", func(t *testing.T) {
		if err := s.DeleteMilestone(ctx, "v1.0-beta", "test"); err != nil {
			t.Fatalf("DeleteMilestone: %v", err)
		}

		// Linked issue should have milestone cleared
		msName := "v1.0-beta"
		filter := types.IssueFilter{Milestone: &msName}
		issues, err := s.SearchIssues(ctx, "", filter)
		if err != nil {
			t.Fatalf("SearchIssues: %v", err)
		}
		if len(issues) != 0 {
			t.Errorf("got %d issues, want 0 (milestone should be cleared)", len(issues))
		}
	})
}
