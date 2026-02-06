package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/types"
)

func TestTeamWatch_Empty(t *testing.T) {
	// Set up in-memory store with no agents
	origStore := store
	origCtx := rootCtx
	origJSON := jsonOutput
	origRig := teamRigFilter
	defer func() {
		store = origStore
		rootCtx = origCtx
		jsonOutput = origJSON
		teamRigFilter = origRig
	}()

	store = memory.New("")
	rootCtx = context.Background()
	jsonOutput = false
	teamRigFilter = ""

	agents, err := getAgentBeads()
	if err != nil {
		t.Fatalf("getAgentBeads failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestTeamWatch_WithAgents(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origJSON := jsonOutput
	origRig := teamRigFilter
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		jsonOutput = origJSON
		teamRigFilter = origRig
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	jsonOutput = true
	teamRigFilter = ""
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create agent beads
	agent1 := &types.Issue{
		ID:         "gt-emma",
		Title:      "Agent: gt-emma",
		IssueType:  types.TypeTask,
		Status:     types.StatusOpen,
		AgentState: types.StateIdle,
		LastActivity: &now,
		HookBead:   "spec-a.md",
		Rig:        "gastown",
		CreatedAt:  now,
	}
	agent2 := &types.Issue{
		ID:         "gt-boris",
		Title:      "Agent: gt-boris",
		IssueType:  types.TypeTask,
		Status:     types.StatusOpen,
		AgentState: "working",
		LastActivity: &now,
		Rig:        "gastown",
		CreatedAt:  now,
	}

	if err := memStore.CreateIssue(ctx, agent1, "test"); err != nil {
		t.Fatalf("create agent1: %v", err)
	}
	if err := memStore.CreateIssue(ctx, agent2, "test"); err != nil {
		t.Fatalf("create agent2: %v", err)
	}

	// Add gt:agent labels
	if err := memStore.AddLabel(ctx, "gt-emma", "gt:agent", "test"); err != nil {
		t.Fatalf("add label: %v", err)
	}
	if err := memStore.AddLabel(ctx, "gt-emma", "rig:gastown", "test"); err != nil {
		t.Fatalf("add label: %v", err)
	}
	if err := memStore.AddLabel(ctx, "gt-boris", "gt:agent", "test"); err != nil {
		t.Fatalf("add label: %v", err)
	}
	if err := memStore.AddLabel(ctx, "gt-boris", "rig:gastown", "test"); err != nil {
		t.Fatalf("add label: %v", err)
	}

	// Test: get all agents
	agents, err := getAgentBeads()
	if err != nil {
		t.Fatalf("getAgentBeads failed: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// Test: filter by rig
	teamRigFilter = "gastown"
	agents, err = getAgentBeads()
	if err != nil {
		t.Fatalf("getAgentBeads with rig filter: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents for rig 'gastown', got %d", len(agents))
	}

	teamRigFilter = "nonexistent"
	agents, err = getAgentBeads()
	if err != nil {
		t.Fatalf("getAgentBeads with nonexistent rig: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents for rig 'nonexistent', got %d", len(agents))
	}
}

func TestTeamPlan_BasicWaves(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origJSON := jsonOutput
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		jsonOutput = origJSON
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	jsonOutput = false
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create an epic
	epic := &types.Issue{
		ID:        "test-epic",
		Title:     "Epic: Feature X",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	if err := memStore.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("create epic: %v", err)
	}

	// Create tasks that depend on the epic
	task1 := &types.Issue{
		ID:        "test-1",
		Title:     "Task A (no blockers)",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	task2 := &types.Issue{
		ID:        "test-2",
		Title:     "Task B (no blockers)",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	task3 := &types.Issue{
		ID:        "test-3",
		Title:     "Task C (blocked by task1)",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}

	for _, task := range []*types.Issue{task1, task2, task3} {
		if err := memStore.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("create %s: %v", task.ID, err)
		}
	}

	// Create dependencies:
	// task1 depends on epic (task1 -> epic)
	// task2 depends on epic (task2 -> epic)
	// task3 depends on epic (task3 -> epic)
	// task3 depends on task1 (task3 -> task1)
	for _, dep := range []*types.Dependency{
		{IssueID: "test-1", DependsOnID: "test-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "test-2", DependsOnID: "test-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "test-3", DependsOnID: "test-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "test-3", DependsOnID: "test-1", Type: types.DepBlocks, CreatedAt: now},
	} {
		if err := memStore.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("add dependency %s -> %s: %v", dep.IssueID, dep.DependsOnID, err)
		}
	}

	// Run planWaves
	waves, err := planWaves("test-epic")
	if err != nil {
		t.Fatalf("planWaves failed: %v", err)
	}

	if waves == nil {
		t.Fatal("expected waves, got nil")
	}

	// Expect 2 waves:
	// Wave 1: task1 and task2 (no within-set blockers)
	// Wave 2: task3 (blocked by task1)
	if len(waves) != 2 {
		t.Fatalf("expected 2 waves, got %d", len(waves))
	}

	// Wave 1 should have 2 tasks
	if len(waves[0]) != 2 {
		t.Errorf("wave 1: expected 2 tasks, got %d", len(waves[0]))
	}

	// Wave 2 should have 1 task
	if len(waves[1]) != 1 {
		t.Errorf("wave 2: expected 1 task, got %d", len(waves[1]))
	}

	// Wave 2 task should be task3
	if len(waves[1]) == 1 && waves[1][0].ID != "test-3" {
		t.Errorf("wave 2: expected test-3, got %s", waves[1][0].ID)
	}
}

func TestTeamPlan_EmptyEpic(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create an epic with no dependents
	epic := &types.Issue{
		ID:        "test-empty-epic",
		Title:     "Empty Epic",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	if err := memStore.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("create epic: %v", err)
	}

	waves, err := planWaves("test-empty-epic")
	if err != nil {
		t.Fatalf("planWaves failed: %v", err)
	}

	if waves != nil {
		t.Errorf("expected nil waves for empty epic, got %d waves", len(waves))
	}
}

func TestTeamPlan_NonexistentEpic(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		daemonClient = origDaemon
	}()

	store = memory.New("")
	rootCtx = context.Background()
	daemonClient = nil

	_, err := planWaves("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent epic")
	}
}

func TestTeamPlan_ClosedDependentsSkipped(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	epic := &types.Issue{
		ID:        "test-epic-closed",
		Title:     "Epic with closed deps",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	closedTask := &types.Issue{
		ID:        "test-closed",
		Title:     "Already done",
		IssueType: types.TypeTask,
		Status:    types.StatusClosed,
		ClosedAt:  &now,
		CreatedAt: now,
	}
	openTask := &types.Issue{
		ID:        "test-open",
		Title:     "Still todo",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}

	for _, issue := range []*types.Issue{epic, closedTask, openTask} {
		if err := memStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("create %s: %v", issue.ID, err)
		}
	}

	// Both depend on epic
	for _, dep := range []*types.Dependency{
		{IssueID: "test-closed", DependsOnID: "test-epic-closed", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "test-open", DependsOnID: "test-epic-closed", Type: types.DepBlocks, CreatedAt: now},
	} {
		if err := memStore.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("add dep: %v", err)
		}
	}

	waves, err := planWaves("test-epic-closed")
	if err != nil {
		t.Fatalf("planWaves failed: %v", err)
	}

	if waves == nil {
		t.Fatal("expected waves (1 open dep)")
	}

	// Only 1 wave with the open task
	totalTasks := 0
	for _, w := range waves {
		totalTasks += len(w)
	}
	if totalTasks != 1 {
		t.Errorf("expected 1 task (closed filtered out), got %d", totalTasks)
	}
}

func TestTeamScore_WithAssignments(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origJSON := jsonOutput
	origRig := teamRigFilter
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		jsonOutput = origJSON
		teamRigFilter = origRig
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	jsonOutput = true
	teamRigFilter = ""
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create an agent
	agent := &types.Issue{
		ID:         "gt-scorer",
		Title:      "Agent: gt-scorer",
		IssueType:  types.TypeTask,
		Status:     types.StatusOpen,
		AgentState: types.StateIdle,
		LastActivity: &now,
		CreatedAt:  now,
	}
	if err := memStore.CreateIssue(ctx, agent, "test"); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := memStore.AddLabel(ctx, "gt-scorer", "gt:agent", "test"); err != nil {
		t.Fatalf("add label: %v", err)
	}

	// Create issues assigned to the agent
	issues := []*types.Issue{
		{ID: "score-1", Title: "Closed task", IssueType: types.TypeTask, Status: types.StatusClosed, Assignee: "gt-scorer", ClosedAt: &now, CreatedAt: now},
		{ID: "score-2", Title: "Closed task 2", IssueType: types.TypeTask, Status: types.StatusClosed, Assignee: "gt-scorer", ClosedAt: &now, CreatedAt: now},
		{ID: "score-3", Title: "In progress", IssueType: types.TypeTask, Status: types.StatusInProgress, Assignee: "gt-scorer", CreatedAt: now},
		{ID: "score-4", Title: "Open task", IssueType: types.TypeTask, Status: types.StatusOpen, Assignee: "gt-scorer", CreatedAt: now},
	}
	for _, issue := range issues {
		if err := memStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("create %s: %v", issue.ID, err)
		}
	}

	// Run the score command logic directly
	agents, err := getAgentBeads()
	if err != nil {
		t.Fatalf("getAgentBeads: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	// Verify agent score by searching assigned issues
	assignee := agents[0].ID
	filter := types.IssueFilter{
		Assignee: &assignee,
	}
	assigned, err := memStore.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("search assigned: %v", err)
	}

	closed, inProgress, open := 0, 0, 0
	for _, issue := range assigned {
		switch issue.Status {
		case types.StatusClosed:
			closed++
		case types.StatusInProgress:
			inProgress++
		case types.StatusOpen:
			open++
		}
	}

	if closed != 2 {
		t.Errorf("expected 2 closed, got %d", closed)
	}
	if inProgress != 1 {
		t.Errorf("expected 1 in-progress, got %d", inProgress)
	}
	if open != 1 {
		t.Errorf("expected 1 open, got %d", open)
	}
}

func TestTeamPlanJSON(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origJSON := jsonOutput
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		jsonOutput = origJSON
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	jsonOutput = true
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create epic with one dependent
	epic := &types.Issue{
		ID:        "json-epic",
		Title:     "JSON Epic",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	task := &types.Issue{
		ID:        "json-task",
		Title:     "JSON Task",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}

	if err := memStore.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("create epic: %v", err)
	}
	if err := memStore.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("create task: %v", err)
	}

	dep := &types.Dependency{
		IssueID:     "json-task",
		DependsOnID: "json-epic",
		Type:        types.DepBlocks,
		CreatedAt:   now,
	}
	if err := memStore.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	waves, err := planWaves("json-epic")
	if err != nil {
		t.Fatalf("planWaves: %v", err)
	}

	if waves == nil {
		t.Fatal("expected waves")
	}

	// Verify JSON output structure
	type jsonTask struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	type jsonWave struct {
		Wave  int        `json:"wave"`
		Tasks []jsonTask `json:"tasks"`
	}

	var jWaves []jsonWave
	for i, w := range waves {
		jw := jsonWave{Wave: i + 1}
		for _, t := range w {
			jw.Tasks = append(jw.Tasks, jsonTask{
				ID:     t.ID,
				Title:  t.Title,
				Status: string(t.Status),
			})
		}
		jWaves = append(jWaves, jw)
	}

	result := map[string]interface{}{
		"epic_id": "json-epic",
		"waves":   jWaves,
		"summary": map[string]interface{}{
			"wave_count": len(waves),
			"task_count": 1,
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	// Verify it parses back correctly
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if parsed["epic_id"] != "json-epic" {
		t.Errorf("expected epic_id 'json-epic', got %v", parsed["epic_id"])
	}

	summary := parsed["summary"].(map[string]interface{})
	if summary["wave_count"].(float64) != 1 {
		t.Errorf("expected 1 wave, got %v", summary["wave_count"])
	}
	if summary["task_count"].(float64) != 1 {
		t.Errorf("expected 1 task, got %v", summary["task_count"])
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		name     string
		elapsed  time.Duration
		expected string
	}{
		{"seconds", 5 * time.Second, "5s ago"},
		{"minutes", 3 * time.Minute, "3m ago"},
		{"hours", 2 * time.Hour, "2h ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := relativeTime(time.Now().Add(-tt.elapsed))
			if result != tt.expected {
				t.Errorf("relativeTime(-%v) = %q, want %q", tt.elapsed, result, tt.expected)
			}
		})
	}
}

func TestTeamWatch_TombstonedFiltered(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origRig := teamRigFilter
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		teamRigFilter = origRig
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	teamRigFilter = ""
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create one active and one tombstoned agent
	active := &types.Issue{
		ID:         "gt-active",
		Title:      "Active Agent",
		IssueType:  types.TypeTask,
		Status:     types.StatusOpen,
		AgentState: types.StateIdle,
		LastActivity: &now,
		CreatedAt:  now,
	}
	tombstoned := &types.Issue{
		ID:         "gt-dead",
		Title:      "Dead Agent",
		IssueType:  types.TypeTask,
		Status:     types.StatusTombstone,
		AgentState: "dead",
		DeletedAt:  &now,
		DeletedBy:  "test",
		CreatedAt:  now,
	}

	for _, a := range []*types.Issue{active, tombstoned} {
		if err := memStore.CreateIssue(ctx, a, "test"); err != nil {
			t.Fatalf("create %s: %v", a.ID, err)
		}
		if err := memStore.AddLabel(ctx, a.ID, "gt:agent", "test"); err != nil {
			t.Fatalf("add label: %v", err)
		}
	}

	agents, err := getAgentBeads()
	if err != nil {
		t.Fatalf("getAgentBeads: %v", err)
	}

	if len(agents) != 1 {
		t.Errorf("expected 1 agent (tombstoned filtered), got %d", len(agents))
	}
	if len(agents) == 1 && agents[0].ID != "gt-active" {
		t.Errorf("expected gt-active, got %s", agents[0].ID)
	}
}

func TestTeamPlan_ThreeWaveChain(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		daemonClient = origDaemon
	}()

	memStore := memory.New("")
	store = memStore
	rootCtx = context.Background()
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create epic -> A -> B -> C (3-wave chain)
	issues := []*types.Issue{
		{ID: "chain-epic", Title: "Chain Epic", IssueType: types.TypeEpic, Status: types.StatusOpen, CreatedAt: now},
		{ID: "chain-a", Title: "Chain A", IssueType: types.TypeTask, Status: types.StatusOpen, CreatedAt: now},
		{ID: "chain-b", Title: "Chain B", IssueType: types.TypeTask, Status: types.StatusOpen, CreatedAt: now},
		{ID: "chain-c", Title: "Chain C", IssueType: types.TypeTask, Status: types.StatusOpen, CreatedAt: now},
	}
	for _, issue := range issues {
		if err := memStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("create %s: %v", issue.ID, err)
		}
	}

	// Dependencies: all depend on epic; B depends on A; C depends on B
	deps := []*types.Dependency{
		{IssueID: "chain-a", DependsOnID: "chain-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "chain-b", DependsOnID: "chain-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "chain-c", DependsOnID: "chain-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "chain-b", DependsOnID: "chain-a", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "chain-c", DependsOnID: "chain-b", Type: types.DepBlocks, CreatedAt: now},
	}
	for _, dep := range deps {
		if err := memStore.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("add dep %s -> %s: %v", dep.IssueID, dep.DependsOnID, err)
		}
	}

	waves, err := planWaves("chain-epic")
	if err != nil {
		t.Fatalf("planWaves: %v", err)
	}

	if waves == nil {
		t.Fatal("expected waves")
	}

	// Expect 3 waves: [A] -> [B] -> [C]
	if len(waves) != 3 {
		// Print wave details for debugging
		for i, w := range waves {
			ids := make([]string, len(w))
			for j, task := range w {
				ids[j] = task.ID
			}
			t.Logf("wave %d: %v", i+1, ids)
		}
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}

	if len(waves[0]) != 1 {
		t.Errorf("wave 1: expected 1 task, got %d", len(waves[0]))
	}
	if len(waves[1]) != 1 {
		t.Errorf("wave 2: expected 1 task, got %d", len(waves[1]))
	}
	if len(waves[2]) != 1 {
		t.Errorf("wave 3: expected 1 task, got %d", len(waves[2]))
	}
}

// TestTeamPlan_SQLiteIntegration uses a real SQLite store for end-to-end testing.
func TestTeamPlan_SQLiteIntegration(t *testing.T) {
	origStore := store
	origCtx := rootCtx
	origDaemon := daemonClient
	defer func() {
		store = origStore
		rootCtx = origCtx
		daemonClient = origDaemon
	}()

	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	defer s.Close()

	store = s
	rootCtx = context.Background()
	daemonClient = nil

	ctx := context.Background()
	now := time.Now()

	// Create epic with dependents (prefix must be "test" to match newTestStore)
	epic := &types.Issue{
		ID:        "test-epic",
		Title:     "SQLite Epic",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	task1 := &types.Issue{
		ID:        "test-1",
		Title:     "SQL Task 1",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}
	task2 := &types.Issue{
		ID:        "test-2",
		Title:     "SQL Task 2",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: now,
	}

	for _, issue := range []*types.Issue{epic, task1, task2} {
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("create %s: %v", issue.ID, err)
		}
	}

	// Both tasks depend on epic; task2 depends on task1
	for _, dep := range []*types.Dependency{
		{IssueID: "test-1", DependsOnID: "test-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "test-2", DependsOnID: "test-epic", Type: types.DepBlocks, CreatedAt: now},
		{IssueID: "test-2", DependsOnID: "test-1", Type: types.DepBlocks, CreatedAt: now},
	} {
		if err := s.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("add dep: %v", err)
		}
	}

	waves, err := planWaves("test-epic")
	if err != nil {
		t.Fatalf("planWaves: %v", err)
	}

	if len(waves) != 2 {
		t.Fatalf("expected 2 waves, got %d", len(waves))
	}
	if len(waves[0]) != 1 || waves[0][0].ID != "test-1" {
		t.Errorf("wave 1: expected [test-1], got %v", waves[0])
	}
	if len(waves[1]) != 1 || waves[1][0].ID != "test-2" {
		t.Errorf("wave 2: expected [test-2], got %v", waves[1])
	}
}
