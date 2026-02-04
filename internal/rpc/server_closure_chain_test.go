package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestAtomicClosureChain_HappyPath verifies that AtomicClosureChain can close
// an MR and its source issue atomically in a single transaction.
func TestAtomicClosureChain_HappyPath(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Configure custom types for Gas Town (merge-request, agent, etc.)
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	// Create an MR (merge-request type)
	mrArgs := &CreateArgs{
		Title:     "Fix bug in parser",
		IssueType: "merge-request",
		Priority:  2,
	}
	mrResp, err := client.Create(mrArgs)
	if err != nil {
		t.Fatalf("Failed to create MR: %v", err)
	}
	var mr struct{ ID string }
	if err := json.Unmarshal(mrResp.Data, &mr); err != nil {
		t.Fatalf("Failed to unmarshal MR: %v", err)
	}

	// Create a source issue (task)
	sourceArgs := &CreateArgs{
		Title:     "Parser crashes on empty input",
		IssueType: "task",
		Priority:  2,
	}
	sourceResp, err := client.Create(sourceArgs)
	if err != nil {
		t.Fatalf("Failed to create source issue: %v", err)
	}
	var source struct{ ID string }
	if err := json.Unmarshal(sourceResp.Data, &source); err != nil {
		t.Fatalf("Failed to unmarshal source issue: %v", err)
	}

	// Close both atomically
	chainArgs := &AtomicClosureChainArgs{
		MRID:              mr.ID,
		MRCloseReason:     "merged",
		SourceIssueID:     source.ID,
		SourceCloseReason: "completed",
	}
	result, err := client.AtomicClosureChain(chainArgs)
	if err != nil {
		t.Fatalf("AtomicClosureChain failed: %v", err)
	}

	// Verify result flags
	if !result.MRClosed {
		t.Error("Expected MRClosed to be true")
	}
	if !result.SourceIssueClosed {
		t.Error("Expected SourceIssueClosed to be true")
	}
	if result.AgentUpdated {
		t.Error("Expected AgentUpdated to be false (no agent specified)")
	}

	// Verify MR is closed in the database
	mrStored, err := store.GetIssue(ctx, mr.ID)
	if err != nil {
		t.Fatalf("Failed to get MR from store: %v", err)
	}
	if mrStored.Status != types.StatusClosed {
		t.Errorf("Expected MR status to be closed, got %s", mrStored.Status)
	}

	// Verify source issue is closed in the database
	sourceStored, err := store.GetIssue(ctx, source.ID)
	if err != nil {
		t.Fatalf("Failed to get source issue from store: %v", err)
	}
	if sourceStored.Status != types.StatusClosed {
		t.Errorf("Expected source issue status to be closed, got %s", sourceStored.Status)
	}
}

// TestAtomicClosureChain_WithAgentUpdate verifies that AtomicClosureChain can
// close issues and update an agent bead atomically.
func TestAtomicClosureChain_WithAgentUpdate(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Configure custom types for Gas Town
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	// Create an MR
	mrArgs := &CreateArgs{
		Title:     "Add new feature",
		IssueType: "merge-request",
		Priority:  2,
	}
	mrResp, err := client.Create(mrArgs)
	if err != nil {
		t.Fatalf("Failed to create MR: %v", err)
	}
	var mr struct{ ID string }
	if err := json.Unmarshal(mrResp.Data, &mr); err != nil {
		t.Fatalf("Failed to unmarshal MR: %v", err)
	}

	// Create a source issue
	sourceArgs := &CreateArgs{
		Title:     "Feature request: new capability",
		IssueType: "task",
		Priority:  2,
	}
	sourceResp, err := client.Create(sourceArgs)
	if err != nil {
		t.Fatalf("Failed to create source issue: %v", err)
	}
	var source struct{ ID string }
	if err := json.Unmarshal(sourceResp.Data, &source); err != nil {
		t.Fatalf("Failed to unmarshal source issue: %v", err)
	}

	// Create an agent bead
	agentArgs := &CreateArgs{
		Title:     "polecat-alpha",
		IssueType: "agent",
		Priority:  2,
	}
	agentResp, err := client.Create(agentArgs)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	var agent struct{ ID string }
	if err := json.Unmarshal(agentResp.Data, &agent); err != nil {
		t.Fatalf("Failed to unmarshal agent: %v", err)
	}

	// Close issues and update agent atomically
	chainArgs := &AtomicClosureChainArgs{
		MRID:              mr.ID,
		MRCloseReason:     "merged",
		SourceIssueID:     source.ID,
		SourceCloseReason: "completed",
		AgentBeadID:       agent.ID,
		AgentUpdates: map[string]interface{}{
			"notes": "Work completed successfully",
		},
	}
	result, err := client.AtomicClosureChain(chainArgs)
	if err != nil {
		t.Fatalf("AtomicClosureChain failed: %v", err)
	}

	// Verify all result flags
	if !result.MRClosed {
		t.Error("Expected MRClosed to be true")
	}
	if !result.SourceIssueClosed {
		t.Error("Expected SourceIssueClosed to be true")
	}
	if !result.AgentUpdated {
		t.Error("Expected AgentUpdated to be true")
	}

	// Verify agent was updated
	agentStored, err := store.GetIssue(ctx, agent.ID)
	if err != nil {
		t.Fatalf("Failed to get agent from store: %v", err)
	}
	if agentStored.Notes != "Work completed successfully" {
		t.Errorf("Expected agent notes to be updated, got %q", agentStored.Notes)
	}
}

// TestAtomicClosureChain_MRNotFound verifies that the operation fails when
// the MR doesn't exist.
func TestAtomicClosureChain_MRNotFound(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Configure custom types for Gas Town
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	// Create only a source issue (no MR)
	sourceArgs := &CreateArgs{
		Title:     "Source issue",
		IssueType: "task",
		Priority:  2,
	}
	sourceResp, err := client.Create(sourceArgs)
	if err != nil {
		t.Fatalf("Failed to create source issue: %v", err)
	}
	var source struct{ ID string }
	if err := json.Unmarshal(sourceResp.Data, &source); err != nil {
		t.Fatalf("Failed to unmarshal source issue: %v", err)
	}

	// Try to close with non-existent MR
	chainArgs := &AtomicClosureChainArgs{
		MRID:              "bd-nonexistent",
		MRCloseReason:     "merged",
		SourceIssueID:     source.ID,
		SourceCloseReason: "completed",
	}
	result, err := client.AtomicClosureChain(chainArgs)

	// Should fail
	if err == nil && result != nil && result.MRClosed {
		t.Error("Expected AtomicClosureChain to fail for non-existent MR")
	}

	// Verify source issue was NOT closed (transaction should have rolled back)
	sourceStored, err := store.GetIssue(ctx, source.ID)
	if err != nil {
		t.Fatalf("Failed to get source issue from store: %v", err)
	}
	if sourceStored.Status == types.StatusClosed {
		t.Error("Source issue should NOT be closed when MR closure fails (atomicity violated)")
	}
}

// TestAtomicClosureChain_SourceNotFound verifies that the operation fails when
// the source issue doesn't exist.
func TestAtomicClosureChain_SourceNotFound(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Configure custom types for Gas Town
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	// Create only an MR (no source issue)
	mrArgs := &CreateArgs{
		Title:     "MR for testing",
		IssueType: "merge-request",
		Priority:  2,
	}
	mrResp, err := client.Create(mrArgs)
	if err != nil {
		t.Fatalf("Failed to create MR: %v", err)
	}
	var mr struct{ ID string }
	if err := json.Unmarshal(mrResp.Data, &mr); err != nil {
		t.Fatalf("Failed to unmarshal MR: %v", err)
	}

	// Try to close with non-existent source issue
	chainArgs := &AtomicClosureChainArgs{
		MRID:              mr.ID,
		MRCloseReason:     "merged",
		SourceIssueID:     "bd-nonexistent-source",
		SourceCloseReason: "completed",
	}
	result, err := client.AtomicClosureChain(chainArgs)

	// Should fail
	if err == nil && result != nil && result.SourceIssueClosed {
		t.Error("Expected AtomicClosureChain to fail for non-existent source issue")
	}

	// Verify MR was NOT closed (transaction should have rolled back)
	mrStored, err := store.GetIssue(ctx, mr.ID)
	if err != nil {
		t.Fatalf("Failed to get MR from store: %v", err)
	}
	if mrStored.Status == types.StatusClosed {
		t.Error("MR should NOT be closed when source closure fails (atomicity violated)")
	}
}

// TestAtomicClosureChain_AtomicRollback verifies that if one close fails,
// nothing is closed (transaction atomicity).
func TestAtomicClosureChain_AtomicRollback(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Configure custom types for Gas Town
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	// Create a valid MR
	mrArgs := &CreateArgs{
		Title:     "Valid MR",
		IssueType: "merge-request",
		Priority:  2,
	}
	mrResp, err := client.Create(mrArgs)
	if err != nil {
		t.Fatalf("Failed to create MR: %v", err)
	}
	var mr struct{ ID string }
	if err := json.Unmarshal(mrResp.Data, &mr); err != nil {
		t.Fatalf("Failed to unmarshal MR: %v", err)
	}

	// Create a valid agent
	agentArgs := &CreateArgs{
		Title:     "Test agent",
		IssueType: "agent",
		Priority:  2,
	}
	agentResp, err := client.Create(agentArgs)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	var agent struct{ ID string }
	if err := json.Unmarshal(agentResp.Data, &agent); err != nil {
		t.Fatalf("Failed to unmarshal agent: %v", err)
	}

	// Record initial agent notes
	agentBefore, err := store.GetIssue(ctx, agent.ID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	originalNotes := agentBefore.Notes

	// Attempt closure with non-existent source - should fail and rollback
	chainArgs := &AtomicClosureChainArgs{
		MRID:              mr.ID,
		MRCloseReason:     "merged",
		SourceIssueID:     "bd-does-not-exist",
		SourceCloseReason: "completed",
		AgentBeadID:       agent.ID,
		AgentUpdates: map[string]interface{}{
			"notes": "This should NOT be saved",
		},
	}
	_, _ = client.AtomicClosureChain(chainArgs)

	// Verify MR was NOT closed
	mrStored, err := store.GetIssue(ctx, mr.ID)
	if err != nil {
		t.Fatalf("Failed to get MR from store: %v", err)
	}
	if mrStored.Status == types.StatusClosed {
		t.Error("MR should NOT be closed after failed transaction")
	}

	// Verify agent was NOT updated
	agentAfter, err := store.GetIssue(ctx, agent.ID)
	if err != nil {
		t.Fatalf("Failed to get agent from store: %v", err)
	}
	if agentAfter.Notes != originalNotes {
		t.Errorf("Agent notes should NOT be updated after failed transaction, got %q", agentAfter.Notes)
	}
}

// TestAtomicClosureChain_WithoutAgentUpdate verifies that the operation works
// correctly when AgentBeadID is empty (no agent update).
func TestAtomicClosureChain_WithoutAgentUpdate(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Configure custom types for Gas Town
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	// Create an MR
	mrArgs := &CreateArgs{
		Title:     "Simple MR",
		IssueType: "merge-request",
		Priority:  2,
	}
	mrResp, err := client.Create(mrArgs)
	if err != nil {
		t.Fatalf("Failed to create MR: %v", err)
	}
	var mr struct{ ID string }
	if err := json.Unmarshal(mrResp.Data, &mr); err != nil {
		t.Fatalf("Failed to unmarshal MR: %v", err)
	}

	// Create a source issue
	sourceArgs := &CreateArgs{
		Title:     "Simple task",
		IssueType: "task",
		Priority:  2,
	}
	sourceResp, err := client.Create(sourceArgs)
	if err != nil {
		t.Fatalf("Failed to create source issue: %v", err)
	}
	var source struct{ ID string }
	if err := json.Unmarshal(sourceResp.Data, &source); err != nil {
		t.Fatalf("Failed to unmarshal source issue: %v", err)
	}

	// Close without agent update (empty AgentBeadID)
	chainArgs := &AtomicClosureChainArgs{
		MRID:              mr.ID,
		MRCloseReason:     "merged",
		SourceIssueID:     source.ID,
		SourceCloseReason: "completed",
		AgentBeadID:       "", // Empty - no agent update
		AgentUpdates:      nil,
	}
	result, err := client.AtomicClosureChain(chainArgs)
	if err != nil {
		t.Fatalf("AtomicClosureChain failed: %v", err)
	}

	// Verify closures succeeded
	if !result.MRClosed {
		t.Error("Expected MRClosed to be true")
	}
	if !result.SourceIssueClosed {
		t.Error("Expected SourceIssueClosed to be true")
	}
	if result.AgentUpdated {
		t.Error("Expected AgentUpdated to be false (no agent specified)")
	}

	// Verify both issues are closed
	mrStored, err := store.GetIssue(ctx, mr.ID)
	if err != nil {
		t.Fatalf("Failed to get MR from store: %v", err)
	}
	if mrStored.Status != types.StatusClosed {
		t.Errorf("Expected MR to be closed, got %s", mrStored.Status)
	}

	sourceStored, err := store.GetIssue(ctx, source.ID)
	if err != nil {
		t.Fatalf("Failed to get source issue from store: %v", err)
	}
	if sourceStored.Status != types.StatusClosed {
		t.Errorf("Expected source issue to be closed, got %s", sourceStored.Status)
	}
}

// TestAtomicClosureChain_ClosureTimestamps verifies that timestamps are set
// correctly on both closed issues.
func TestAtomicClosureChain_ClosureTimestamps(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Configure custom types for Gas Town
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	// Create an MR
	mrArgs := &CreateArgs{
		Title:     "MR with timestamps",
		IssueType: "merge-request",
		Priority:  2,
	}
	mrResp, err := client.Create(mrArgs)
	if err != nil {
		t.Fatalf("Failed to create MR: %v", err)
	}
	var mr struct{ ID string }
	if err := json.Unmarshal(mrResp.Data, &mr); err != nil {
		t.Fatalf("Failed to unmarshal MR: %v", err)
	}

	// Create a source issue
	sourceArgs := &CreateArgs{
		Title:     "Task with timestamps",
		IssueType: "task",
		Priority:  2,
	}
	sourceResp, err := client.Create(sourceArgs)
	if err != nil {
		t.Fatalf("Failed to create source issue: %v", err)
	}
	var source struct{ ID string }
	if err := json.Unmarshal(sourceResp.Data, &source); err != nil {
		t.Fatalf("Failed to unmarshal source issue: %v", err)
	}

	// Record time before closure (truncate to second precision for RFC3339 comparison)
	beforeClose := time.Now().Truncate(time.Second)

	// Close both atomically
	chainArgs := &AtomicClosureChainArgs{
		MRID:              mr.ID,
		MRCloseReason:     "merged",
		SourceIssueID:     source.ID,
		SourceCloseReason: "completed",
	}
	result, err := client.AtomicClosureChain(chainArgs)
	if err != nil {
		t.Fatalf("AtomicClosureChain failed: %v", err)
	}

	// Record time after closure (add 1 second buffer for second-precision timestamps)
	afterClose := time.Now().Add(time.Second).Truncate(time.Second)

	// Verify result timestamps are set
	if result.MRCloseTime == "" {
		t.Error("Expected MRCloseTime to be set")
	}
	if result.SourceCloseTime == "" {
		t.Error("Expected SourceCloseTime to be set")
	}

	// Parse and verify result timestamps are in valid RFC3339 format
	mrCloseTime, err := time.Parse(time.RFC3339, result.MRCloseTime)
	if err != nil {
		t.Errorf("MRCloseTime is not valid RFC3339: %v", err)
	} else {
		if mrCloseTime.Before(beforeClose) || mrCloseTime.After(afterClose) {
			t.Errorf("MRCloseTime %v is outside expected range [%v, %v]", mrCloseTime, beforeClose, afterClose)
		}
	}

	sourceCloseTime, err := time.Parse(time.RFC3339, result.SourceCloseTime)
	if err != nil {
		t.Errorf("SourceCloseTime is not valid RFC3339: %v", err)
	} else {
		if sourceCloseTime.Before(beforeClose) || sourceCloseTime.After(afterClose) {
			t.Errorf("SourceCloseTime %v is outside expected range [%v, %v]", sourceCloseTime, beforeClose, afterClose)
		}
	}

	// Verify database timestamps are set on MR
	mrStored, err := store.GetIssue(ctx, mr.ID)
	if err != nil {
		t.Fatalf("Failed to get MR from store: %v", err)
	}
	if mrStored.ClosedAt == nil {
		t.Error("Expected MR ClosedAt timestamp to be set in database")
	} else {
		if mrStored.ClosedAt.Before(beforeClose) || mrStored.ClosedAt.After(afterClose) {
			t.Errorf("MR ClosedAt %v is outside expected range [%v, %v]", mrStored.ClosedAt, beforeClose, afterClose)
		}
	}

	// Verify database timestamps are set on source issue
	sourceStored, err := store.GetIssue(ctx, source.ID)
	if err != nil {
		t.Fatalf("Failed to get source issue from store: %v", err)
	}
	if sourceStored.ClosedAt == nil {
		t.Error("Expected source issue ClosedAt timestamp to be set in database")
	} else {
		if sourceStored.ClosedAt.Before(beforeClose) || sourceStored.ClosedAt.After(afterClose) {
			t.Errorf("Source ClosedAt %v is outside expected range [%v, %v]", sourceStored.ClosedAt, beforeClose, afterClose)
		}
	}
}
