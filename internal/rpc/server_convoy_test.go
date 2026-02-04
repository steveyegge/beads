package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// convoyType returns a pointer to the "convoy" issue type for filtering.
func convoyType() *types.IssueType {
	t := types.IssueType("convoy")
	return &t
}

// setupCustomTypesForConvoy configures the store to accept the "convoy" issue type.
func setupCustomTypesForConvoy(t *testing.T, store interface {
	SetConfig(ctx context.Context, key, value string) error
}) {
	t.Helper()
	ctx := context.Background()
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		t.Fatalf("Failed to set types.custom: %v", err)
	}
}

// createTestIssueForConvoy creates a test issue and returns its ID.
func createTestIssueForConvoy(t *testing.T, client *Client, title string) string {
	t.Helper()
	createArgs := &CreateArgs{
		Title:     title,
		IssueType: "task",
		Priority:  2,
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create issue %q: %v", title, err)
	}
	if !resp.Success {
		t.Fatalf("Failed to create issue %q: %s", title, resp.Error)
	}
	var issue struct{ ID string }
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}
	return issue.ID
}

// TestCreateConvoyWithTracking_HappyPath verifies that a convoy can be created
// with multiple tracked issues and all tracking dependencies are added.
func TestCreateConvoyWithTracking_HappyPath(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Create test issues to track
	issue1 := createTestIssueForConvoy(t, client, "Issue 1 for convoy")
	issue2 := createTestIssueForConvoy(t, client, "Issue 2 for convoy")
	issue3 := createTestIssueForConvoy(t, client, "Issue 3 for convoy")

	// Create convoy with tracking
	args := &CreateConvoyWithTrackingArgs{
		Name:          "Test Convoy",
		TrackedIssues: []string{issue1, issue2, issue3},
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err != nil {
		t.Fatalf("CreateConvoyWithTracking failed: %v", err)
	}

	// Verify result
	if result.ConvoyID == "" {
		t.Error("Expected convoy ID to be set")
	}
	if result.TrackedCount != 3 {
		t.Errorf("Expected tracked_count=3, got %d", result.TrackedCount)
	}
	if len(result.TrackedIDs) != 3 {
		t.Errorf("Expected 3 tracked IDs, got %d", len(result.TrackedIDs))
	}

	// Verify convoy was created in store
	ctx := context.Background()
	convoy, err := store.GetIssue(ctx, result.ConvoyID)
	if err != nil {
		t.Fatalf("Failed to get convoy from store: %v", err)
	}
	if convoy == nil {
		t.Fatal("Convoy not found in store")
	}
	if convoy.Title != "Test Convoy" {
		t.Errorf("Expected convoy title='Test Convoy', got %q", convoy.Title)
	}
	if convoy.IssueType != "convoy" {
		t.Errorf("Expected convoy type='convoy', got %q", convoy.IssueType)
	}

	// Verify tracking dependencies exist using GetDependenciesWithMetadata
	deps, err := store.GetDependenciesWithMetadata(ctx, result.ConvoyID)
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}
	if len(deps) != 3 {
		t.Errorf("Expected 3 tracking dependencies, got %d", len(deps))
	}

	// Verify all tracked issues are in the dependency list
	trackedSet := make(map[string]bool)
	for _, dep := range deps {
		if dep.DependencyType != types.DepTracks {
			t.Errorf("Expected dependency type 'tracks', got %q", dep.DependencyType)
		}
		trackedSet[dep.ID] = true
	}
	for _, id := range []string{issue1, issue2, issue3} {
		if !trackedSet[id] {
			t.Errorf("Expected issue %s to be tracked, but not found in dependencies", id)
		}
	}
}

// TestCreateConvoyWithTracking_AutoGenerateID verifies that convoy ID is
// auto-generated when not provided.
func TestCreateConvoyWithTracking_AutoGenerateID(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Create a test issue to track
	issue := createTestIssueForConvoy(t, client, "Issue for auto-ID convoy")

	// Create convoy without specifying ID
	args := &CreateConvoyWithTrackingArgs{
		Name:          "Auto-ID Convoy",
		TrackedIssues: []string{issue},
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err != nil {
		t.Fatalf("CreateConvoyWithTracking failed: %v", err)
	}

	// Verify ID was generated
	if result.ConvoyID == "" {
		t.Error("Expected auto-generated convoy ID")
	}

	// Verify the convoy exists in store with the generated ID
	ctx := context.Background()
	convoy, err := store.GetIssue(ctx, result.ConvoyID)
	if err != nil {
		t.Fatalf("Failed to get convoy: %v", err)
	}
	if convoy == nil {
		t.Fatalf("Convoy with auto-generated ID %q not found", result.ConvoyID)
	}

	// Verify ID follows expected pattern (should contain the prefix)
	// The prefix is "bd" set in setupTestServerWithStore
	if !strings.HasPrefix(result.ConvoyID, "bd-") {
		t.Errorf("Expected convoy ID to have prefix 'bd-', got %q", result.ConvoyID)
	}
}

// TestCreateConvoyWithTracking_EmptyTrackedIssues verifies that an error is
// returned when no tracked issues are provided.
func TestCreateConvoyWithTracking_EmptyTrackedIssues(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Try to create convoy with no tracked issues
	args := &CreateConvoyWithTrackingArgs{
		Name:          "Empty Convoy",
		TrackedIssues: []string{},
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err == nil && result != nil {
		t.Errorf("Expected error for empty tracked issues, got success with result: %+v", result)
	}

	// Also test with nil TrackedIssues
	args2 := &CreateConvoyWithTrackingArgs{
		Name:          "Nil Tracked Issues Convoy",
		TrackedIssues: nil,
	}

	result2, err2 := client.CreateConvoyWithTracking(args2)
	if err2 == nil && result2 != nil {
		t.Errorf("Expected error for nil tracked issues, got success with result: %+v", result2)
	}
}

// TestCreateConvoyWithTracking_InvalidTrackedIssue verifies that an error is
// returned when a tracked issue doesn't exist.
func TestCreateConvoyWithTracking_InvalidTrackedIssue(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Create one valid issue
	validIssue := createTestIssueForConvoy(t, client, "Valid issue for convoy")

	// Try to create convoy with a non-existent issue
	args := &CreateConvoyWithTrackingArgs{
		Name:          "Convoy with invalid issue",
		TrackedIssues: []string{validIssue, "nonexistent-issue-id"},
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err == nil && result != nil {
		t.Errorf("Expected error for non-existent tracked issue, got success with result: %+v", result)
	}

	// Verify the error message mentions the invalid issue
	if err != nil && !strings.Contains(err.Error(), "nonexistent") {
		t.Logf("Error: %v (expected to mention nonexistent issue)", err)
	}

	// Verify no convoy was created (transaction should have rolled back)
	ctx := context.Background()
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{IssueType: convoyType()})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	if len(issues) > 0 {
		t.Errorf("Found %d convoys that should not exist after failed creation: %v", len(issues), issues)
	}
}

// TestCreateConvoyWithTracking_AtomicRollback verifies that if tracking fails,
// the convoy is not created (atomic transaction rollback).
func TestCreateConvoyWithTracking_AtomicRollback(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Create two valid issues
	issue1 := createTestIssueForConvoy(t, client, "Valid issue 1")
	issue2 := createTestIssueForConvoy(t, client, "Valid issue 2")

	ctx := context.Background()

	// Count existing convoys
	convoysBefore, err := store.SearchIssues(ctx, "", types.IssueFilter{IssueType: convoyType()})
	if err != nil {
		t.Fatalf("Failed to search convoys before: %v", err)
	}
	convoyCountBefore := len(convoysBefore)

	// Try to create convoy with valid issues first, then a non-existent one
	// The transaction should fail when adding the dependency to nonexistent-id
	args := &CreateConvoyWithTrackingArgs{
		Name:          "Convoy for rollback test",
		TrackedIssues: []string{issue1, issue2, "nonexistent-id"},
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err == nil && result != nil {
		t.Errorf("Expected error for non-existent tracked issue, got success")
	}

	// Verify no new convoy was created
	convoysAfter, err := store.SearchIssues(ctx, "", types.IssueFilter{IssueType: convoyType()})
	if err != nil {
		t.Fatalf("Failed to search convoys after: %v", err)
	}
	convoyCountAfter := len(convoysAfter)

	if convoyCountAfter != convoyCountBefore {
		t.Errorf("Expected no new convoys after failed creation (before=%d, after=%d)",
			convoyCountBefore, convoyCountAfter)
	}

	// Also verify no partial dependencies were created
	// The valid issues should not have any 'tracked-by' reverse dependencies
	for _, id := range []string{issue1, issue2} {
		reverseDeps, err := store.GetDependentsWithMetadata(ctx, id)
		if err != nil {
			t.Fatalf("Failed to get reverse dependencies for %s: %v", id, err)
		}
		for _, dep := range reverseDeps {
			if dep.DependencyType == types.DepTracks {
				t.Errorf("Found unexpected tracking dependency on %s - transaction did not roll back", id)
			}
		}
	}
}

// TestCreateConvoyWithTracking_WithOwnerAndNotify verifies that owner and
// notify_address fields are properly set on the created convoy.
func TestCreateConvoyWithTracking_WithOwnerAndNotify(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Create a test issue to track
	issue := createTestIssueForConvoy(t, client, "Issue for owner/notify test")

	// Create convoy with owner and notify address
	args := &CreateConvoyWithTrackingArgs{
		Name:          "Convoy with owner and notify",
		TrackedIssues: []string{issue},
		Owner:         "test-owner@example.com",
		NotifyAddress: "mayor/inbox",
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err != nil {
		t.Fatalf("CreateConvoyWithTracking failed: %v", err)
	}

	// Verify result
	if result.ConvoyID == "" {
		t.Error("Expected convoy ID")
	}

	// Verify convoy was created with correct owner
	ctx := context.Background()
	convoy, err := store.GetIssue(ctx, result.ConvoyID)
	if err != nil {
		t.Fatalf("Failed to get convoy: %v", err)
	}

	if convoy.Owner != "test-owner@example.com" {
		t.Errorf("Expected owner='test-owner@example.com', got %q", convoy.Owner)
	}

	// Verify notify address is in the description
	if !strings.Contains(convoy.Description, "mayor/inbox") {
		t.Errorf("Expected description to contain notify address 'mayor/inbox', got %q", convoy.Description)
	}
}

// TestCreateConvoyWithTracking_WithProvidedConvoyID verifies that a specific
// convoy ID can be provided and is used.
func TestCreateConvoyWithTracking_WithProvidedConvoyID(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Create a test issue to track
	issue := createTestIssueForConvoy(t, client, "Issue for specific ID convoy")

	// Create convoy with a specific ID (must match the "bd-" prefix from setupTestServerWithStore)
	specificID := "bd-custom-convoy-id"
	args := &CreateConvoyWithTrackingArgs{
		ConvoyID:      specificID,
		Name:          "Convoy with specific ID",
		TrackedIssues: []string{issue},
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err != nil {
		t.Fatalf("CreateConvoyWithTracking failed: %v", err)
	}

	// Verify the specific ID was used
	if result.ConvoyID != specificID {
		t.Errorf("Expected convoy ID=%q, got %q", specificID, result.ConvoyID)
	}

	// Verify convoy exists with that ID
	ctx := context.Background()
	convoy, err := store.GetIssue(ctx, specificID)
	if err != nil {
		t.Fatalf("Failed to get convoy: %v", err)
	}
	if convoy == nil {
		t.Fatalf("Convoy with ID %q not found", specificID)
	}
}

// TestCreateConvoyWithTracking_EmptyName verifies that an error is returned
// when no name is provided.
func TestCreateConvoyWithTracking_EmptyName(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Configure custom types including "convoy"
	setupCustomTypesForConvoy(t, store)

	// Create a test issue to track
	issue := createTestIssueForConvoy(t, client, "Issue for empty name test")

	// Try to create convoy without a name
	args := &CreateConvoyWithTrackingArgs{
		Name:          "",
		TrackedIssues: []string{issue},
	}

	result, err := client.CreateConvoyWithTracking(args)
	if err == nil && result != nil {
		t.Errorf("Expected error for empty convoy name, got success with result: %+v", result)
	}
}
