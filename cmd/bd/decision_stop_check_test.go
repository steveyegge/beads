package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/steveyegge/beads/internal/eventbus"
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
		IssueType: "task",
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
		IssueType: "task",
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
	dp, err := findPendingAgentDecision(ctx, "")
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
	dp, err := findPendingAgentDecision(ctx, "")
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
	dp, err := findPendingAgentDecision(ctx, "")
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
	dp, err := findPendingAgentDecision(ctx, "")
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
	dp, err := findPendingAgentDecision(ctx, "")
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
	dp, err := findPendingAgentDecision(ctx, "")
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

func TestFindPendingAgentDecision_SessionScoping(t *testing.T) {
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	defer func() { store = oldStore }()

	// Create decisions from different sessions
	createTestDecision(t, s, "test-sess-a", "Decision A", "session-111", "")
	createTestDecision(t, s, "test-sess-b", "Decision B", "session-222", "")

	ctx := context.Background()

	// With empty sessionTag, both are candidates (returns most recent)
	dp, err := findPendingAgentDecision(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp == nil {
		t.Fatal("expected decision with empty session tag, got nil")
	}

	// With specific sessionTag, only matching decision is returned
	dp, err = findPendingAgentDecision(ctx, "session-111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp == nil {
		t.Fatal("expected decision for session-111, got nil")
	}
	if dp.IssueID != "test-sess-a" {
		t.Fatalf("expected test-sess-a, got %s", dp.IssueID)
	}

	// With non-matching sessionTag, no decision returned
	dp, err = findPendingAgentDecision(ctx, "session-999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp != nil {
		t.Fatalf("expected nil for non-matching session, got %+v", dp)
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
	dp, err := pollForAgentDecision(ctx, "", 5*time.Second, 100*time.Millisecond)
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
	dp, err := pollForAgentDecision(ctx, "", 300*time.Millisecond, 100*time.Millisecond)
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
	dp, err := pollForAgentDecision(ctx, "", 2*time.Second, 100*time.Millisecond)
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

func TestWaitForDecisionViaEventBus_NoDaemon(t *testing.T) {
	// When daemonClient is nil, should return error (triggering polling fallback).
	oldDaemon := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldDaemon }()

	_, _, err := waitForDecisionViaEventBus(context.Background(), "test-123", 1*time.Second)
	if err == nil {
		t.Fatal("expected error when daemonClient is nil")
	}
	if !strings.Contains(err.Error(), "no daemon client") {
		t.Fatalf("expected 'no daemon client' error, got: %v", err)
	}
}

func TestPollStopDecisionLoop_FindsResponse(t *testing.T) {
	// Verify that pollStopDecisionLoop picks up a responded decision.
	s, cleanup := setupStopCheckTestDB(t)
	defer cleanup()

	oldStore := store
	store = s
	oldDaemon := daemonClient
	daemonClient = nil
	defer func() { store = oldStore; daemonClient = oldDaemon }()

	ctx := context.Background()
	createTestDecision(t, s, "test-poll1", "What next?", "agent-1", "some context")

	// Respond to the decision
	dp, err := s.GetDecisionPoint(ctx, "test-poll1")
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	now := time.Now()
	dp.RespondedAt = &now
	dp.SelectedOption = "approve"
	dp.ResponseText = "looks good"
	if err := s.UpdateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("update decision: %v", err)
	}

	// Poll should find it immediately
	selected, text, err := pollStopDecisionLoop(ctx, "test-poll1", 5*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != "approve" {
		t.Fatalf("expected selected='approve', got %q", selected)
	}
	if text != "looks good" {
		t.Fatalf("expected text='looks good', got %q", text)
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

// --- NATS Integration Tests ---
// These tests start a real embedded NATS server with JetStream to verify
// the event bus wake path end-to-end.

// startTestNATSForStopCheck starts an embedded NATS server with JetStream
// and creates the required streams. Returns the JetStream context and cleanup.
func startTestNATSForStopCheck(t *testing.T) (nats.JetStreamContext, func()) {
	t.Helper()
	dir := t.TempDir()
	opts := &natsserver.Options{
		Port:               -1, // random available port
		JetStream:          true,
		JetStreamMaxMemory: 256 << 20,
		JetStreamMaxStore:  256 << 20,
		StoreDir:           dir,
		NoLog:              true,
		NoSigs:             true,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("create test NATS server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("test NATS server failed to start")
	}

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		ns.Shutdown()
		t.Fatalf("connect to test NATS: %v", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		ns.Shutdown()
		t.Fatalf("get JetStream context: %v", err)
	}

	if err := eventbus.EnsureStreams(js); err != nil {
		nc.Close()
		ns.Shutdown()
		t.Fatalf("create streams: %v", err)
	}

	cleanup := func() {
		nc.Drain()
		nc.Close()
		ns.Shutdown()
	}
	return js, cleanup
}

func TestAwaitDecisionOnJetStream_ReceivesEvent(t *testing.T) {
	// Integration test: start real NATS, subscribe, publish event, verify wake.
	js, natsCleanup := startTestNATSForStopCheck(t)
	defer natsCleanup()

	// Set up SQLite with a pending decision.
	s, dbCleanup := setupStopCheckTestDB(t)
	defer dbCleanup()

	oldStore := store
	store = s
	oldDaemon := daemonClient
	daemonClient = nil
	defer func() { store = oldStore; daemonClient = oldDaemon }()

	ctx := context.Background()
	createTestDecision(t, s, "test-nats1", "Which approach?", "agent", "some context")

	// Pre-respond the decision in the DB (simulating bd decision respond).
	dp, err := s.GetDecisionPoint(ctx, "test-nats1")
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	now := time.Now()
	dp.RespondedAt = &now
	dp.SelectedOption = "option-a"
	dp.ResponseText = "Go with approach A"
	if err := s.UpdateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("update decision: %v", err)
	}

	// Publish DecisionResponded event to NATS (simulating what bd decision respond does).
	payload := eventbus.DecisionEventPayload{
		DecisionID:  "test-nats1",
		Question:    "Which approach?",
		ChosenLabel: "option-a",
		Rationale:   "Go with approach A",
	}
	payloadJSON, _ := json.Marshal(payload)
	subject := eventbus.SubjectForEvent(eventbus.EventDecisionResponded)

	// Launch awaitDecisionOnJetStream in a goroutine.
	type result struct {
		selected, text string
		err            error
	}
	ch := make(chan result, 1)
	go func() {
		sel, txt, err := awaitDecisionOnJetStream(ctx, js, "test-nats1", 10*time.Second)
		ch <- result{sel, txt, err}
	}()

	// Give the subscriber a moment to connect, then publish.
	time.Sleep(200 * time.Millisecond)
	if _, err := js.Publish(subject, payloadJSON); err != nil {
		t.Fatalf("publish event: %v", err)
	}

	// Wait for result.
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if r.selected != "option-a" {
			t.Errorf("expected selected='option-a', got %q", r.selected)
		}
		if r.text != "Go with approach A" {
			t.Errorf("expected text='Go with approach A', got %q", r.text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for awaitDecisionOnJetStream")
	}
}

func TestAwaitDecisionOnJetStream_IgnoresOtherDecisions(t *testing.T) {
	// Verify that events for other decision IDs are ignored.
	js, natsCleanup := startTestNATSForStopCheck(t)
	defer natsCleanup()

	s, dbCleanup := setupStopCheckTestDB(t)
	defer dbCleanup()

	oldStore := store
	store = s
	oldDaemon := daemonClient
	daemonClient = nil
	defer func() { store = oldStore; daemonClient = oldDaemon }()

	ctx := context.Background()
	createTestDecision(t, s, "test-target", "Target decision", "agent", "ctx")
	createTestDecision(t, s, "test-other", "Other decision", "agent", "ctx")

	// Respond to the target decision in DB.
	dp, _ := s.GetDecisionPoint(ctx, "test-target")
	now := time.Now()
	dp.RespondedAt = &now
	dp.SelectedOption = "correct"
	dp.ResponseText = "correct response"
	_ = s.UpdateDecisionPoint(ctx, dp)

	subject := eventbus.SubjectForEvent(eventbus.EventDecisionResponded)

	type result struct {
		selected, text string
		err            error
	}
	ch := make(chan result, 1)
	go func() {
		sel, txt, err := awaitDecisionOnJetStream(ctx, js, "test-target", 10*time.Second)
		ch <- result{sel, txt, err}
	}()

	time.Sleep(200 * time.Millisecond)

	// Publish event for the WRONG decision first.
	wrongPayload, _ := json.Marshal(eventbus.DecisionEventPayload{
		DecisionID:  "test-other",
		ChosenLabel: "wrong",
	})
	if _, err := js.Publish(subject, wrongPayload); err != nil {
		t.Fatalf("publish wrong event: %v", err)
	}

	// Then publish event for the correct decision.
	correctPayload, _ := json.Marshal(eventbus.DecisionEventPayload{
		DecisionID:  "test-target",
		ChosenLabel: "correct",
		Rationale:   "correct response",
	})
	if _, err := js.Publish(subject, correctPayload); err != nil {
		t.Fatalf("publish correct event: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if r.selected != "correct" {
			t.Errorf("expected selected='correct', got %q", r.selected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestAwaitDecisionOnJetStream_AlreadyResponded(t *testing.T) {
	// If the decision was already responded to before we subscribe,
	// the race-condition guard should catch it immediately.
	js, natsCleanup := startTestNATSForStopCheck(t)
	defer natsCleanup()

	s, dbCleanup := setupStopCheckTestDB(t)
	defer dbCleanup()

	oldStore := store
	store = s
	oldDaemon := daemonClient
	daemonClient = nil
	defer func() { store = oldStore; daemonClient = oldDaemon }()

	ctx := context.Background()
	createTestDecision(t, s, "test-preresponded", "Already done", "agent", "ctx")

	// Respond BEFORE calling await.
	dp, _ := s.GetDecisionPoint(ctx, "test-preresponded")
	now := time.Now()
	dp.RespondedAt = &now
	dp.SelectedOption = "early"
	dp.ResponseText = "responded before subscribe"
	_ = s.UpdateDecisionPoint(ctx, dp)

	// Should return immediately without waiting for NATS event.
	selected, text, err := awaitDecisionOnJetStream(ctx, js, "test-preresponded", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != "early" {
		t.Errorf("expected selected='early', got %q", selected)
	}
	if text != "responded before subscribe" {
		t.Errorf("expected text='responded before subscribe', got %q", text)
	}
}

func TestAwaitDecisionOnJetStream_Timeout(t *testing.T) {
	// Verify timeout returns empty strings without error.
	js, natsCleanup := startTestNATSForStopCheck(t)
	defer natsCleanup()

	s, dbCleanup := setupStopCheckTestDB(t)
	defer dbCleanup()

	oldStore := store
	store = s
	oldDaemon := daemonClient
	daemonClient = nil
	defer func() { store = oldStore; daemonClient = oldDaemon }()

	createTestDecision(t, s, "test-timeout", "Will timeout", "agent", "ctx")

	// Short timeout, no event published.
	selected, text, err := awaitDecisionOnJetStream(context.Background(), js, "test-timeout", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != "" {
		t.Errorf("expected empty selected on timeout, got %q", selected)
	}
	if text != "" {
		t.Errorf("expected empty text on timeout, got %q", text)
	}
}
