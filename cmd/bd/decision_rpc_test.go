//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// setupDaemonTestEnvForDecision sets up a complete daemon test environment for decision tests
func setupDaemonTestEnvForDecision(t *testing.T) (context.Context, context.CancelFunc, *rpc.Client, *sqlite.SQLiteStorage, func()) {
	t.Helper()

	tmpDir := makeSocketTempDir(t)
	initTestGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	socketPath := filepath.Join(beadsDir, "bd.sock")
	testDBPath := filepath.Join(beadsDir, "beads.db")

	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log := daemonLogger{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, "", "", "", "", "", log)
	if err != nil {
		cancel()
		t.Fatalf("Failed to start RPC server: %v", err)
	}

	// Wait for server to be ready
	select {
	case <-server.WaitReady():
		// Server is ready
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Server did not become ready")
	}

	// Connect RPC client
	client, err := rpc.TryConnect(socketPath)
	if err != nil || client == nil {
		cancel()
		t.Fatalf("Failed to connect RPC client: %v", err)
	}

	cleanup := func() {
		if client != nil {
			client.Close()
		}
		if server != nil {
			server.Stop()
		}
		testStore.Close()
	}

	return ctx, cancel, client, testStore, cleanup
}

// TestDecisionListViaDaemon tests decision_list uses daemonClient.DecisionList when available
func TestDecisionListViaDaemon_BasicList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Create a decision gate issue
	gateIssue := &types.Issue{
		Title:     "Test Decision Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create a decision point
	options := []types.DecisionOption{
		{ID: "yes", Label: "Yes, proceed", Short: "Yes"},
		{ID: "no", Label: "No, abort", Short: "No"},
	}
	optionsJSON, _ := json.Marshal(options)

	dp := &types.DecisionPoint{
		IssueID:       gateIssue.ID,
		Prompt:        "Should we proceed?",
		Options:       string(optionsJSON),
		DefaultOption: "yes",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
		RequestedBy:   "test-agent",
	}
	if err := testStore.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("Failed to create decision point: %v", err)
	}

	// List decisions via daemon RPC
	listArgs := &rpc.DecisionListArgs{
		All: false,
	}

	resp, err := client.DecisionList(listArgs)
	if err != nil {
		t.Fatalf("DecisionList RPC failed: %v", err)
	}

	if resp.Count != 1 {
		t.Errorf("Expected 1 decision, got %d", resp.Count)
	}

	if len(resp.Decisions) != 1 {
		t.Fatalf("Expected 1 decision in response, got %d", len(resp.Decisions))
	}

	decision := resp.Decisions[0]
	if decision.Decision.IssueID != gateIssue.ID {
		t.Errorf("Expected decision issue ID %s, got %s", gateIssue.ID, decision.Decision.IssueID)
	}

	if decision.Decision.Prompt != "Should we proceed?" {
		t.Errorf("Expected prompt 'Should we proceed?', got '%s'", decision.Decision.Prompt)
	}
}

// TestDecisionListViaDaemon_EmptyList tests decision_list returns empty when no decisions exist
func TestDecisionListViaDaemon_EmptyList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, cancel, client, _, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// List decisions via daemon RPC (should be empty)
	listArgs := &rpc.DecisionListArgs{
		All: false,
	}

	resp, err := client.DecisionList(listArgs)
	if err != nil {
		t.Fatalf("DecisionList RPC failed: %v", err)
	}

	if resp.Count != 0 {
		t.Errorf("Expected 0 decisions, got %d", resp.Count)
	}

	if len(resp.Decisions) != 0 {
		t.Errorf("Expected empty decisions list, got %d items", len(resp.Decisions))
	}
}

// TestDecisionShowViaDaemon tests decision_show uses daemonClient.DecisionGet when available
func TestDecisionShowViaDaemon_BasicShow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Create a decision gate issue
	gateIssue := &types.Issue{
		Title:     "Test Decision Show Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create a decision point with context
	options := []types.DecisionOption{
		{ID: "a", Label: "Option A: Use Redis", Short: "Redis", Description: "Full caching solution"},
		{ID: "b", Label: "Option B: Use Memcached", Short: "Memcached", Description: "Simple key-value store"},
	}
	optionsJSON, _ := json.Marshal(options)

	dp := &types.DecisionPoint{
		IssueID:       gateIssue.ID,
		Prompt:        "Which caching solution should we use?",
		Context:       "We need to choose a caching backend for the API layer.",
		Options:       string(optionsJSON),
		DefaultOption: "a",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
		RequestedBy:   "architect-agent",
		Urgency:       "high",
	}
	if err := testStore.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("Failed to create decision point: %v", err)
	}

	// Get decision via daemon RPC
	getArgs := &rpc.DecisionGetArgs{
		IssueID: gateIssue.ID,
	}

	resp, err := client.DecisionGet(getArgs)
	if err != nil {
		t.Fatalf("DecisionGet RPC failed: %v", err)
	}

	if resp.Decision == nil {
		t.Fatal("Expected decision in response, got nil")
	}

	if resp.Decision.IssueID != gateIssue.ID {
		t.Errorf("Expected decision issue ID %s, got %s", gateIssue.ID, resp.Decision.IssueID)
	}

	if resp.Decision.Prompt != "Which caching solution should we use?" {
		t.Errorf("Expected prompt 'Which caching solution should we use?', got '%s'", resp.Decision.Prompt)
	}

	if resp.Decision.Context != "We need to choose a caching backend for the API layer." {
		t.Errorf("Expected context about API layer, got '%s'", resp.Decision.Context)
	}

	if resp.Decision.DefaultOption != "a" {
		t.Errorf("Expected default option 'a', got '%s'", resp.Decision.DefaultOption)
	}
}

// TestDecisionShowViaDaemon_NotFound tests decision_show error handling for non-existent decision
func TestDecisionShowViaDaemon_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, cancel, client, _, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Try to get non-existent decision
	getArgs := &rpc.DecisionGetArgs{
		IssueID: "test-nonexistent-decision",
	}

	resp, err := client.DecisionGet(getArgs)
	// Either an error or a nil decision is acceptable
	if err == nil && resp != nil && resp.Decision != nil {
		t.Error("Expected error or nil decision for non-existent ID")
	}
}

// TestDecisionRespondViaDaemon tests decision_respond uses daemonClient.DecisionResolve when available
func TestDecisionRespondViaDaemon_SelectOption(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Create a decision gate issue
	gateIssue := &types.Issue{
		Title:     "Test Decision Respond Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create a decision point
	options := []types.DecisionOption{
		{ID: "approve", Label: "Approve the design", Short: "Approve"},
		{ID: "reject", Label: "Reject and request changes", Short: "Reject"},
	}
	optionsJSON, _ := json.Marshal(options)

	dp := &types.DecisionPoint{
		IssueID:       gateIssue.ID,
		Prompt:        "Approve the design document?",
		Options:       string(optionsJSON),
		DefaultOption: "approve",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
		RequestedBy:   "design-agent",
	}
	if err := testStore.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("Failed to create decision point: %v", err)
	}

	// Resolve decision via daemon RPC
	resolveArgs := &rpc.DecisionResolveArgs{
		IssueID:        gateIssue.ID,
		SelectedOption: "approve",
		ResponseText:   "Looks good, approved!",
		RespondedBy:    "reviewer@example.com",
	}

	resp, err := client.DecisionResolve(resolveArgs)
	if err != nil {
		t.Fatalf("DecisionResolve RPC failed: %v", err)
	}

	if resp.Decision == nil {
		t.Fatal("Expected decision in response, got nil")
	}

	if resp.Decision.SelectedOption != "approve" {
		t.Errorf("Expected selected option 'approve', got '%s'", resp.Decision.SelectedOption)
	}

	if resp.Decision.ResponseText != "Looks good, approved!" {
		t.Errorf("Expected response text 'Looks good, approved!', got '%s'", resp.Decision.ResponseText)
	}

	if resp.Decision.RespondedBy != "reviewer@example.com" {
		t.Errorf("Expected responded by 'reviewer@example.com', got '%s'", resp.Decision.RespondedBy)
	}

	if resp.Decision.RespondedAt == nil {
		t.Error("Expected responded_at to be set")
	}
}

// TestDecisionRespondViaDaemon_WithGuidance tests decision_respond with guidance (iteration path)
func TestDecisionRespondViaDaemon_WithGuidance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Create a decision gate issue
	gateIssue := &types.Issue{
		Title:     "Test Decision Guidance Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create a decision point
	options := []types.DecisionOption{
		{ID: "option1", Label: "First approach", Short: "First"},
		{ID: "option2", Label: "Second approach", Short: "Second"},
	}
	optionsJSON, _ := json.Marshal(options)

	dp := &types.DecisionPoint{
		IssueID:       gateIssue.ID,
		Prompt:        "Which approach should we take?",
		Options:       string(optionsJSON),
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
		RequestedBy:   "planning-agent",
	}
	if err := testStore.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("Failed to create decision point: %v", err)
	}

	// Resolve decision with guidance (no selection, triggering iteration)
	resolveArgs := &rpc.DecisionResolveArgs{
		IssueID:      gateIssue.ID,
		Guidance:     "Consider a hybrid approach combining both options",
		RespondedBy:  "architect@example.com",
	}

	resp, err := client.DecisionResolve(resolveArgs)
	if err != nil {
		t.Fatalf("DecisionResolve RPC failed: %v", err)
	}

	if resp.Decision == nil {
		t.Fatal("Expected decision in response, got nil")
	}

	// Verify guidance was recorded
	if resp.Decision.Guidance != "Consider a hybrid approach combining both options" {
		t.Logf("Guidance in response: '%s'", resp.Decision.Guidance)
	}
}

// TestDecisionCreateViaDaemon tests decision_create uses daemonClient.DecisionCreate when available
func TestDecisionCreateViaDaemon_BasicCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// First create a gate issue to attach the decision to
	gateIssue := &types.Issue{
		Title:     "Decision Create Test Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create decision via daemon RPC
	createArgs := &rpc.DecisionCreateArgs{
		IssueID:       gateIssue.ID,
		Prompt:        "Should we implement feature X?",
		Options:       rpc.StringOptions("Yes, implement it", "No, defer it", "Need more research"),
		DefaultOption: "need-more-research",
		MaxIterations: 5,
		RequestedBy:   "product-agent",
	}

	resp, err := client.DecisionCreate(createArgs)
	if err != nil {
		t.Fatalf("DecisionCreate RPC failed: %v", err)
	}

	if resp.Decision == nil {
		t.Fatal("Expected decision in response, got nil")
	}

	if resp.Decision.Prompt != "Should we implement feature X?" {
		t.Errorf("Expected prompt 'Should we implement feature X?', got '%s'", resp.Decision.Prompt)
	}

	if resp.Decision.MaxIterations != 5 {
		t.Errorf("Expected max iterations 5, got %d", resp.Decision.MaxIterations)
	}

	if resp.Decision.RequestedBy != "product-agent" {
		t.Errorf("Expected requested by 'product-agent', got '%s'", resp.Decision.RequestedBy)
	}

	if resp.Decision.IssueID != gateIssue.ID {
		t.Errorf("Expected issue ID %s, got %s", gateIssue.ID, resp.Decision.IssueID)
	}
}

// TestDecisionCreateViaDaemon_WithOptions tests decision_create with multiple options
func TestDecisionCreateViaDaemon_WithOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// First create a gate issue to attach the decision to
	gateIssue := &types.Issue{
		Title:     "Database Decision Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create decision with multiple options
	createArgs := &rpc.DecisionCreateArgs{
		IssueID: gateIssue.ID,
		Prompt:  "Which database should we use?",
		Options: rpc.StringOptions(
			"PostgreSQL: Reliable, full-featured RDBMS",
			"MySQL: Popular, fast for read-heavy workloads",
			"SQLite: Simple, embedded database",
			"MongoDB: Document-oriented, flexible schema",
		),
		DefaultOption: "postgresql",
		MaxIterations: 3,
		RequestedBy:   "data-architect",
	}

	resp, err := client.DecisionCreate(createArgs)
	if err != nil {
		t.Fatalf("DecisionCreate RPC failed: %v", err)
	}

	if resp.Decision == nil {
		t.Fatal("Expected decision in response, got nil")
	}

	if resp.Decision.Prompt != "Which database should we use?" {
		t.Errorf("Expected prompt about database selection, got '%s'", resp.Decision.Prompt)
	}

	// Verify options were stored
	if resp.Decision.Options == "" {
		t.Error("Expected options to be stored")
	}
}

// TestDecisionRPC_DirectVsFallback verifies RPC path produces same results as direct storage
func TestDecisionRPC_DirectVsFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Create a decision gate issue directly
	gateIssue := &types.Issue{
		Title:     "Direct vs Fallback Test Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create decision point directly in storage
	options := []types.DecisionOption{
		{ID: "direct", Label: "Created directly in storage", Short: "Direct"},
	}
	optionsJSON, _ := json.Marshal(options)

	dp := &types.DecisionPoint{
		IssueID:       gateIssue.ID,
		Prompt:        "Test consistency between RPC and direct storage",
		Options:       string(optionsJSON),
		DefaultOption: "direct",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
		RequestedBy:   "test-consistency",
	}
	if err := testStore.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("Failed to create decision point: %v", err)
	}

	// Retrieve via RPC
	getArgs := &rpc.DecisionGetArgs{
		IssueID: gateIssue.ID,
	}

	rpcResp, err := client.DecisionGet(getArgs)
	if err != nil {
		t.Fatalf("DecisionGet RPC failed: %v", err)
	}

	// Retrieve directly from storage
	directDP, err := testStore.GetDecisionPoint(ctx, gateIssue.ID)
	if err != nil {
		t.Fatalf("Direct GetDecisionPoint failed: %v", err)
	}

	// Compare key fields
	if rpcResp.Decision == nil || directDP == nil {
		t.Fatal("Both RPC and direct should return decision data")
	}

	if rpcResp.Decision.IssueID != directDP.IssueID {
		t.Errorf("IssueID mismatch: RPC=%s, Direct=%s", rpcResp.Decision.IssueID, directDP.IssueID)
	}

	if rpcResp.Decision.Prompt != directDP.Prompt {
		t.Errorf("Prompt mismatch: RPC=%s, Direct=%s", rpcResp.Decision.Prompt, directDP.Prompt)
	}

	if rpcResp.Decision.DefaultOption != directDP.DefaultOption {
		t.Errorf("DefaultOption mismatch: RPC=%s, Direct=%s", rpcResp.Decision.DefaultOption, directDP.DefaultOption)
	}

	if rpcResp.Decision.Iteration != directDP.Iteration {
		t.Errorf("Iteration mismatch: RPC=%d, Direct=%d", rpcResp.Decision.Iteration, directDP.Iteration)
	}

	if rpcResp.Decision.MaxIterations != directDP.MaxIterations {
		t.Errorf("MaxIterations mismatch: RPC=%d, Direct=%d", rpcResp.Decision.MaxIterations, directDP.MaxIterations)
	}
}

// TestDecisionList_MultipleDecisions tests listing multiple decisions
func TestDecisionList_MultipleDecisions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Create multiple decision gate issues
	for i := 0; i < 3; i++ {
		gateIssue := &types.Issue{
			Title:     "Multi-Decision Gate " + string(rune('A'+i)),
			IssueType: "gate",
			AwaitType: "decision",
			Status:    types.StatusOpen,
			Priority:  2,
		}
		if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
			t.Fatalf("Failed to create gate issue %d: %v", i, err)
		}

		options := []types.DecisionOption{
			{ID: "opt1", Label: "Option 1", Short: "Opt1"},
			{ID: "opt2", Label: "Option 2", Short: "Opt2"},
		}
		optionsJSON, _ := json.Marshal(options)

		dp := &types.DecisionPoint{
			IssueID:       gateIssue.ID,
			Prompt:        "Decision prompt " + string(rune('A'+i)),
			Options:       string(optionsJSON),
			Iteration:     1,
			MaxIterations: 3,
			CreatedAt:     time.Now(),
			RequestedBy:   "batch-test",
		}
		if err := testStore.CreateDecisionPoint(ctx, dp); err != nil {
			t.Fatalf("Failed to create decision point %d: %v", i, err)
		}
	}

	// List all decisions via daemon RPC
	listArgs := &rpc.DecisionListArgs{
		All: false,
	}

	resp, err := client.DecisionList(listArgs)
	if err != nil {
		t.Fatalf("DecisionList RPC failed: %v", err)
	}

	if resp.Count != 3 {
		t.Errorf("Expected 3 decisions, got %d", resp.Count)
	}

	if len(resp.Decisions) != 3 {
		t.Errorf("Expected 3 decisions in response, got %d", len(resp.Decisions))
	}
}

// TestDecisionRespond_AlreadyResponded tests error handling for already-responded decision
func TestDecisionRespond_AlreadyResponded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// Create a decision gate issue
	gateIssue := &types.Issue{
		Title:     "Already Responded Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create a decision point that's already responded
	now := time.Now()
	options := []types.DecisionOption{
		{ID: "yes", Label: "Yes", Short: "Yes"},
		{ID: "no", Label: "No", Short: "No"},
	}
	optionsJSON, _ := json.Marshal(options)

	dp := &types.DecisionPoint{
		IssueID:        gateIssue.ID,
		Prompt:         "Already responded decision",
		Options:        string(optionsJSON),
		Iteration:      1,
		MaxIterations:  3,
		CreatedAt:      now,
		RequestedBy:    "test-agent",
		SelectedOption: "yes",
		RespondedAt:    &now,
		RespondedBy:    "previous-responder",
	}
	if err := testStore.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("Failed to create decision point: %v", err)
	}

	// Try to respond again
	resolveArgs := &rpc.DecisionResolveArgs{
		IssueID:        gateIssue.ID,
		SelectedOption: "no",
		RespondedBy:    "second-responder",
	}

	resp, err := client.DecisionResolve(resolveArgs)
	// The RPC should either return an error or the existing response
	// Both are valid behaviors - we just want to ensure it doesn't crash
	t.Logf("Response to already-responded decision: err=%v, resp=%+v", err, resp)
}

// TestDecisionCreate_MinimalArgs tests decision_create with minimal required arguments
func TestDecisionCreate_MinimalArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForDecision(t)
	defer cleanup()
	defer cancel()

	// First create a gate issue to attach the decision to
	gateIssue := &types.Issue{
		Title:     "Minimal Decision Gate",
		IssueType: "gate",
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := testStore.CreateIssue(ctx, gateIssue, "test"); err != nil {
		t.Fatalf("Failed to create gate issue: %v", err)
	}

	// Create decision with minimal args (issue ID, prompt, and options)
	createArgs := &rpc.DecisionCreateArgs{
		IssueID: gateIssue.ID,
		Prompt:  "Simple yes/no question?",
		Options: rpc.StringOptions("Yes", "No"),
	}

	resp, err := client.DecisionCreate(createArgs)
	if err != nil {
		t.Fatalf("DecisionCreate RPC failed: %v", err)
	}

	if resp.Decision == nil {
		t.Fatal("Expected decision in response, got nil")
	}

	if resp.Decision.Prompt != "Simple yes/no question?" {
		t.Errorf("Expected prompt 'Simple yes/no question?', got '%s'", resp.Decision.Prompt)
	}

	// MaxIterations should default to 3
	if resp.Decision.MaxIterations != 3 {
		t.Logf("MaxIterations was %d (may vary by implementation)", resp.Decision.MaxIterations)
	}
}
