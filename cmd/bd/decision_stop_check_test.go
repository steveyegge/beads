package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// setupStopCheckTestDB creates a temp SQLite store for stop-check tests.
func setupStopCheckTestDB(t *testing.T) (*sqlite.SQLiteStorage, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "bd-stop-check-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	testDB := filepath.Join(tmpDir, "test.db")
	s, err := sqlite.New(context.Background(), testDB)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create test database: %v", err)
	}

	ctx := context.Background()
	if err := s.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		s.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	cleanup := func() {
		s.Close()
		os.RemoveAll(tmpDir)
	}

	return s, cleanup
}

// createTestDecisionAt creates a gate issue + decision point with a specific timestamp.
func createTestDecisionAt(t *testing.T, s *sqlite.SQLiteStorage, id, prompt, requestedBy, decisionContext string, createdAt time.Time) {
	t.Helper()
	ctx := context.Background()

	issue := &types.Issue{
		ID:        id,
		Title:     prompt,
		IssueType: "gate",
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		Labels:    []string{"gt:decision", "decision:pending"},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	if requestedBy == "stop-hook" {
		issue.Labels = append(issue.Labels, "stop-decision")
	}

	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue(%s): %v", id, err)
	}

	dp := &types.DecisionPoint{
		IssueID:       id,
		Prompt:        prompt,
		Context:       decisionContext,
		Options:       `[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}]`,
		Iteration:     1,
		MaxIterations: 1,
		CreatedAt:     createdAt,
		RequestedBy:   requestedBy,
	}
	if err := s.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint(%s): %v", id, err)
	}
}

// createTestDecision creates a gate issue + decision point in the store.
func createTestDecision(t *testing.T, s *sqlite.SQLiteStorage, id, prompt, requestedBy, decisionContext string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()

	issue := &types.Issue{
		ID:        id,
		Title:     prompt,
		IssueType: "gate",
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		Labels:    []string{"gt:decision", "decision:pending"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if requestedBy == "stop-hook" {
		issue.Labels = append(issue.Labels, "stop-decision")
	}

	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue(%s): %v", id, err)
	}

	dp := &types.DecisionPoint{
		IssueID:       id,
		Prompt:        prompt,
		Context:       decisionContext,
		Options:       `[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}]`,
		Iteration:     1,
		MaxIterations: 1,
		CreatedAt:     now,
		RequestedBy:   requestedBy,
	}
	if err := s.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint(%s): %v", id, err)
	}
}

func TestFindPendingAgentDecision_NoDecisions(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	ctx := context.Background()
	dp, err := findPendingAgentDecision(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp != nil {
		t.Fatalf("expected nil, got %+v", dp)
	}
}

func TestFindPendingAgentDecision_SkipsStopHook(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	// Create a stop-hook decision — should be skipped
	createTestDecision(t, s, "test-stop1", "Stop decision", "stop-hook", "")

	ctx := context.Background()
	dp, err := findPendingAgentDecision(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp != nil {
		t.Fatalf("expected nil (stop-hook should be skipped), got %+v", dp)
	}
}

func TestFindPendingAgentDecision_SkipsEmptyRequestedBy(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	// Create a decision with empty RequestedBy — should be skipped
	createTestDecision(t, s, "test-empty1", "Anonymous decision", "", "")

	ctx := context.Background()
	dp, err := findPendingAgentDecision(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp != nil {
		t.Fatalf("expected nil (empty RequestedBy should be skipped), got %+v", dp)
	}
}

func TestFindPendingAgentDecision_FindsAgentDecision(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	createTestDecision(t, s, "test-agent1", "Agent decision", "agent", "some context")

	ctx := context.Background()
	dp, err := findPendingAgentDecision(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp == nil {
		t.Fatal("expected agent decision, got nil")
	}
	if dp.IssueID != "test-agent1" {
		t.Fatalf("expected test-agent1, got %s", dp.IssueID)
	}
	if dp.Context != "some context" {
		t.Fatalf("expected 'some context', got %q", dp.Context)
	}
}

func TestFindPendingAgentDecision_ReturnsOneOfMultiple(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	// Create two agent decisions — should return one of them
	createTestDecision(t, s, "test-a", "Decision A", "agent", "")
	createTestDecision(t, s, "test-b", "Decision B", "agent", "")

	ctx := context.Background()
	dp, err := findPendingAgentDecision(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp == nil {
		t.Fatal("expected agent decision, got nil")
	}
	if dp.IssueID != "test-a" && dp.IssueID != "test-b" {
		t.Fatalf("expected test-a or test-b, got %s", dp.IssueID)
	}
}

func TestFindPendingAgentDecision_IgnoresStopHookAmongAgent(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	// Mix of stop-hook and agent decisions
	createTestDecision(t, s, "test-stop", "Stop", "stop-hook", "")
	createTestDecision(t, s, "test-agent", "Agent", "agent", "")

	ctx := context.Background()
	dp, err := findPendingAgentDecision(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp == nil {
		t.Fatal("expected agent decision, got nil")
	}
	if dp.IssueID != "test-agent" {
		t.Fatalf("expected test-agent, got %s", dp.IssueID)
	}
}

func TestFindUnclosedStopDecisions_NoDecisions(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	ctx := context.Background()
	unclosed, err := findUnclosedStopDecisions(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unclosed) != 0 {
		t.Fatalf("expected 0 unclosed, got %d", len(unclosed))
	}
}

func TestFindUnclosedStopDecisions_FindsOpenStopHook(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	createTestDecision(t, s, "test-stop1", "Stop decision", "stop-hook", "")

	ctx := context.Background()
	unclosed, err := findUnclosedStopDecisions(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unclosed) != 1 {
		t.Fatalf("expected 1 unclosed, got %d", len(unclosed))
	}
	if unclosed[0].IssueID != "test-stop1" {
		t.Fatalf("expected test-stop1, got %s", unclosed[0].IssueID)
	}
}

func TestFindUnclosedStopDecisions_IgnoresClosedIssues(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	createTestDecision(t, s, "test-stop-closed", "Closed stop", "stop-hook", "")

	// Close the issue
	ctx := context.Background()
	if err := s.UpdateIssue(ctx, "test-stop-closed", map[string]interface{}{
		"status": string(types.StatusClosed),
	}, "test"); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	unclosed, err := findUnclosedStopDecisions(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unclosed) != 0 {
		t.Fatalf("expected 0 unclosed (issue is closed), got %d", len(unclosed))
	}
}

func TestFindUnclosedStopDecisions_IgnoresAgentDecisions(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	createTestDecision(t, s, "test-agent1", "Agent decision", "agent", "")

	ctx := context.Background()
	unclosed, err := findUnclosedStopDecisions(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unclosed) != 0 {
		t.Fatalf("expected 0 unclosed (agent decisions should be ignored), got %d", len(unclosed))
	}
}

func TestPollForAgentDecision_FindsExisting(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	createTestDecision(t, s, "test-agent-poll", "Agent poll test", "agent", "context")

	ctx := context.Background()
	dp, err := pollForAgentDecision(ctx, 5*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp == nil {
		t.Fatal("expected decision, got nil")
	}
	if dp.IssueID != "test-agent-poll" {
		t.Fatalf("expected test-agent-poll, got %s", dp.IssueID)
	}
}

func TestPollForAgentDecision_TimesOut(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	// No decisions — should timeout
	ctx := context.Background()
	dp, err := pollForAgentDecision(ctx, 300*time.Millisecond, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp != nil {
		t.Fatalf("expected nil on timeout, got %+v", dp)
	}
}

func TestPollForAgentDecision_FindsDelayedDecision(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	// Create decision after a delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		createTestDecision(t, s, "test-delayed", "Delayed", "agent", "context")
	}()

	ctx := context.Background()
	dp, err := pollForAgentDecision(ctx, 2*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp == nil {
		t.Fatal("expected delayed decision, got nil (timed out)")
	}
	if dp.IssueID != "test-delayed" {
		t.Fatalf("expected test-delayed, got %s", dp.IssueID)
	}
}

func TestDecisionResponseText_PreferResponseText(t *testing.T) {
	dp := &types.DecisionPoint{
		ResponseText: "response",
		Rationale:    "rationale",
		Guidance:     "guidance",
	}
	got := decisionResponseText(dp)
	if got != "response" {
		t.Fatalf("expected 'response', got %q", got)
	}
}

func TestDecisionResponseText_FallbackToRationale(t *testing.T) {
	dp := &types.DecisionPoint{
		Rationale: "rationale",
		Guidance:  "guidance",
	}
	got := decisionResponseText(dp)
	if got != "rationale" {
		t.Fatalf("expected 'rationale', got %q", got)
	}
}

func TestDecisionResponseText_FallbackToGuidance(t *testing.T) {
	dp := &types.DecisionPoint{
		Guidance: "guidance",
	}
	got := decisionResponseText(dp)
	if got != "guidance" {
		t.Fatalf("expected 'guidance', got %q", got)
	}
}

func TestDecisionResponseText_Empty(t *testing.T) {
	dp := &types.DecisionPoint{}
	got := decisionResponseText(dp)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestStopDecisionConfig_DefaultsAreFalse(t *testing.T) {
	cfg := stopDecisionConfig{}
	if cfg.RequireAgentDecision {
		t.Fatal("RequireAgentDecision should default to false")
	}
	if cfg.RequireCloseOld {
		t.Fatal("RequireCloseOld should default to false")
	}
	if cfg.RequireContext {
		t.Fatal("RequireContext should default to false")
	}
}

func TestStopDecisionConfig_JSONRoundTrip(t *testing.T) {
	// Verify the config struct can round-trip through JSON (as it does from the config bead)
	input := `{
		"enabled": true,
		"timeout": "5m",
		"require_agent_decision": true,
		"require_close_old": true,
		"require_context": true,
		"agent_decision_prompt": "Create a decision",
		"agent_context_prompt": "Add context",
		"agent_close_old_prompt": "Close old ones"
	}`

	var cfg stopDecisionConfig
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !cfg.Enabled {
		t.Fatal("expected Enabled=true")
	}
	if cfg.Timeout != "5m" {
		t.Fatalf("expected timeout '5m', got %q", cfg.Timeout)
	}
	if !cfg.RequireAgentDecision {
		t.Fatal("expected RequireAgentDecision=true")
	}
	if !cfg.RequireCloseOld {
		t.Fatal("expected RequireCloseOld=true")
	}
	if !cfg.RequireContext {
		t.Fatal("expected RequireContext=true")
	}
	if cfg.AgentDecisionPrompt != "Create a decision" {
		t.Fatalf("expected AgentDecisionPrompt, got %q", cfg.AgentDecisionPrompt)
	}
	if cfg.AgentContextPrompt != "Add context" {
		t.Fatalf("expected AgentContextPrompt, got %q", cfg.AgentContextPrompt)
	}
	if cfg.AgentCloseOldPrompt != "Close old ones" {
		t.Fatalf("expected AgentCloseOldPrompt, got %q", cfg.AgentCloseOldPrompt)
	}
}

func TestJoinIDs(t *testing.T) {
	tests := []struct {
		ids  []string
		want string
	}{
		{[]string{}, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a, b, c"},
	}
	for _, tt := range tests {
		got := joinIDs(tt.ids)
		if got != tt.want {
			t.Errorf("joinIDs(%v) = %q, want %q", tt.ids, got, tt.want)
		}
	}
}

func TestParseDurationOrDefault(t *testing.T) {
	tests := []struct {
		input    string
		def      time.Duration
		expected time.Duration
	}{
		{"", 5 * time.Minute, 5 * time.Minute},
		{"10s", 5 * time.Minute, 10 * time.Second},
		{"invalid", 5 * time.Minute, 5 * time.Minute},
		{"30m", time.Hour, 30 * time.Minute},
	}
	for _, tt := range tests {
		got := parseDurationOrDefault(tt.input, tt.def)
		if got != tt.expected {
			t.Errorf("parseDurationOrDefault(%q, %v) = %v, want %v", tt.input, tt.def, got, tt.expected)
		}
	}
}
