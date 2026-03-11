//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestSummaryEpicMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	epic := &types.Issue{Title: "Auth System", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic, CreatedAt: time.Now().Add(-5 * 24 * time.Hour)}
	if err := s.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	child1 := &types.Issue{Title: "DB schema", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now().Add(-4 * 24 * time.Hour)}
	child2 := &types.Issue{Title: "API endpoints", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now().Add(-3 * 24 * time.Hour)}
	for _, child := range []*types.Issue{child1, child2} {
		if err := s.CreateIssue(ctx, child, "test"); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		if err := s.AddDependency(ctx, &types.Dependency{IssueID: child.ID, DependsOnID: epic.ID, Type: types.DepParentChild}, "test"); err != nil {
			t.Fatalf("AddDependency: %v", err)
		}
	}
	for _, child := range []*types.Issue{child1, child2} {
		if err := s.CloseIssue(ctx, child.ID, "done", "test", "sess-1"); err != nil {
			t.Fatalf("CloseIssue: %v", err)
		}
	}
	t.Run("EpicSummaryShowsChildren", func(t *testing.T) {
		result, err := buildEpicSummary(ctx, s, epic.ID)
		if err != nil {
			t.Fatalf("buildEpicSummary: %v", err)
		}
		if result.EpicID != epic.ID {
			t.Errorf("EpicID = %q, want %q", result.EpicID, epic.ID)
		}
		if len(result.Children) != 2 {
			t.Errorf("len(Children) = %d, want 2", len(result.Children))
		}
		if result.ClosedCount != 2 {
			t.Errorf("ClosedCount = %d, want 2", result.ClosedCount)
		}
	})
}

func TestSummarySinceMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	issue := &types.Issue{Title: "Fix login bug", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now()}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := s.CloseIssue(ctx, issue.ID, "done", "test", "sess-1"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	t.Run("SinceFilterFindsClosedIssue", func(t *testing.T) {
		since := time.Now().Add(-1 * time.Hour)
		result, err := buildSinceSummary(ctx, s, since)
		if err != nil {
			t.Fatalf("buildSinceSummary: %v", err)
		}
		if result.TotalClosed != 1 {
			t.Errorf("TotalClosed = %d, want 1", result.TotalClosed)
		}
	})
}

func TestSummarySessionMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	sessionID := "test-session-abc"
	issue := &types.Issue{Title: "Session work", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now()}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := s.CloseIssue(ctx, issue.ID, "done", "test", sessionID); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	t.Run("SessionFindsClosedBySession", func(t *testing.T) {
		result, err := buildSessionSummary(ctx, s, sessionID)
		if err != nil {
			t.Fatalf("buildSessionSummary: %v", err)
		}
		if len(result.Closed) != 1 {
			t.Errorf("len(Closed) = %d, want 1", len(result.Closed))
		}
	})
	t.Run("SessionErrorsWithoutSessionID", func(t *testing.T) {
		os.Unsetenv("CLAUDE_SESSION_ID")
		_, err := buildSessionSummary(ctx, s, "")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
	})
}

func TestSummaryEpicDecisionComments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)

	epic := &types.Issue{Title: "Auth System", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic, CreatedAt: time.Now()}
	if err := s.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := s.AddIssueComment(ctx, epic.ID, "test", "DECISION: Use JWT for auth"); err != nil {
		t.Fatalf("AddIssueComment: %v", err)
	}

	result, err := buildEpicSummary(ctx, s, epic.ID)
	if err != nil {
		t.Fatalf("buildEpicSummary: %v", err)
	}
	if len(result.Decisions) != 1 {
		t.Errorf("len(Decisions) = %d, want 1", len(result.Decisions))
	}
	if len(result.Decisions) > 0 && result.Decisions[0] != "DECISION: Use JWT for auth" {
		t.Errorf("Decisions[0] = %q, want %q", result.Decisions[0], "DECISION: Use JWT for auth")
	}
}

func TestSummaryJSONOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	issue := &types.Issue{Title: "JSON test", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now()}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := s.CloseIssue(ctx, issue.ID, "done", "test", "sess-json"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	result, err := buildSessionSummary(ctx, s, "sess-json")
	if err != nil {
		t.Fatalf("buildSessionSummary: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var parsed SessionSummaryResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if parsed.SessionID != "sess-json" {
		t.Errorf("SessionID = %q, want %q", parsed.SessionID, "sess-json")
	}
}
