package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestPacmanAchievements(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")
	store := newTestStore(t, dbPath)
	ctx := context.Background()

	agent := "agent"
	now := time.Now().UTC()

	// Create 5 issues and close them to trigger streak-5
	var closedID string
	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			Title:     "Issue",
			Priority:  1,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			CreatedAt: now.Add(-time.Minute * time.Duration(i)),
		}
		if err := store.CreateIssue(ctx, issue, agent); err != nil {
			t.Fatalf("create issue: %v", err)
		}
		if i == 0 {
			closedID = issue.ID
		}
		if err := store.CloseIssue(ctx, issue.ID, "done", agent, ""); err != nil {
			t.Fatalf("close issue: %v", err)
		}
	}

	// Mark an issue as blocked before closing to trigger ghost-buster
	blockedIssue := &types.Issue{
		Title:     "Blocked",
		Priority:  1,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now.Add(-time.Hour),
	}
	if err := store.CreateIssue(ctx, blockedIssue, agent); err != nil {
		t.Fatalf("create blocked issue: %v", err)
	}
	updates := map[string]interface{}{"status": string(types.StatusBlocked)}
	if err := store.UpdateIssue(ctx, blockedIssue.ID, updates, agent); err != nil {
		t.Fatalf("block issue: %v", err)
	}
	if err := store.CloseIssue(ctx, blockedIssue.ID, "done", agent, ""); err != nil {
		t.Fatalf("close blocked issue: %v", err)
	}

	// Add dependency for assist-master: issue B depends on closed issue A
	dependent := &types.Issue{
		Title:     "Dependent",
		Priority:  1,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now.Add(-time.Minute),
	}
	if err := store.CreateIssue(ctx, dependent, agent); err != nil {
		t.Fatalf("create dependent issue: %v", err)
	}
	dep := &types.Dependency{IssueID: dependent.ID, DependsOnID: closedID, Type: types.DepBlocks}
	if err := store.AddDependency(ctx, dep, agent); err != nil {
		t.Fatalf("add dependency: %v", err)
	}

	pause := &pacmanPause{TS: now.Add(-time.Hour).Format(time.RFC3339)}
	achievements := computePacmanAchievementsFromDB(store.UnderlyingDB(), agent, now, pause)
	want := []string{"first-blood", "streak-5", "ghost-buster", "assist-master", "comeback"}
	for _, id := range want {
		if !hasAchievement(achievements, id) {
			t.Fatalf("missing achievement: %s", id)
		}
	}
}

func TestPacmanRenderIncludesAchievements(t *testing.T) {
	state := pacmanState{
		Agent:        "agent",
		Score:        5,
		Achievements: []pacmanAchievement{{ID: "first-blood", Label: "First Blood"}},
	}
	output := captureOutput(func() {
		renderPacmanState(state)
	})
	if !strings.Contains(output, "First Blood") {
		t.Fatalf("expected achievements in output, got: %s", output)
	}
}

func TestPacmanMazeRendersGhosts(t *testing.T) {
	state := pacmanState{Blockers: []pacmanBlocker{{ID: "bd-1"}}}
	maze := renderPacmanArtString(state)
	if !strings.Contains(maze, "◐") {
		t.Fatalf("expected ghost marker in maze, got: %s", maze)
	}
}

func TestMergeScoreboardAggregates(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	scoreboard := pacmanScoreboard{
		Scores: map[string]pacmanScore{
			"alice": {Dots: 2},
			"bob":   {Dots: 3},
		},
	}
	data, err := json.MarshalIndent(scoreboard, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "scoreboard.json"), data, 0o644); err != nil {
		t.Fatalf("write scoreboard: %v", err)
	}

	agg := map[string]pacmanScore{}
	mergeScoreboard(beadsDir, agg)

	if agg["alice"].Dots != 2 || agg["bob"].Dots != 3 {
		t.Fatalf("unexpected aggregate: %#v", agg)
	}
}

func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func hasAchievement(list []pacmanAchievement, id string) bool {
	for _, entry := range list {
		if entry.ID == id {
			return true
		}
	}
	return false
}

var _ = sqlite.ErrNotFound
