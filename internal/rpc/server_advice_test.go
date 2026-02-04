package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// ============================================================================
// Advice System RPC Tests
// ============================================================================
//
// These tests verify the RPC layer behavior for the advice system.
//
// KNOWN LIMITATION: The storage layer (sqlite/issues.go) does not currently
// persist advice_hook_* fields. The migrations add the columns, but:
// - insertIssue/insertIssueStrict don't include these columns
// - scanIssues doesn't read these columns
// - UpdateIssue doesn't recognize these fields
//
// Tests marked with "// Storage gap:" document this limitation and will
// start passing once the storage layer is updated.
// ============================================================================

// TestAdvice_CreateWithTargetingLabels verifies that advice can be created with
// targeting labels (rig:X, role:Y) and the labels are properly attached.
func TestAdvice_CreateWithTargetingLabels(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create advice targeting a specific rig and role
	createArgs := &CreateArgs{
		Title:       "Format Go files before commit",
		Description: "Run gofmt on all modified Go files",
		IssueType:   "advice",
		Priority:    2,
		Labels:      []string{"rig:gastown", "role:polecat", "global"},
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Create failed: %s", resp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Verify labels were attached
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	expectedLabels := map[string]bool{
		"rig:gastown":  true,
		"role:polecat": true,
		"global":       true,
	}

	for _, label := range labels {
		delete(expectedLabels, label)
	}

	if len(expectedLabels) > 0 {
		missing := make([]string, 0, len(expectedLabels))
		for label := range expectedLabels {
			missing = append(missing, label)
		}
		t.Errorf("Missing expected labels: %v (got labels: %v)", missing, labels)
	}
}

// TestAdvice_ListByTypeFilter verifies that advice beads can be filtered by type.
func TestAdvice_ListByTypeFilter(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create several advice beads
	adviceTitles := []string{
		"Advice 1: Format code",
		"Advice 2: Run tests",
		"Advice 3: Check lint",
	}

	for _, title := range adviceTitles {
		createArgs := &CreateArgs{
			Title:       title,
			Description: "Test advice",
			IssueType:   "advice",
			Priority:    2,
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}
		if !resp.Success {
			t.Fatalf("Create failed: %s", resp.Error)
		}
	}

	// Create a non-advice issue
	createNonAdvice := &CreateArgs{
		Title:       "Regular task",
		Description: "Test task",
		IssueType:   "task",
		Priority:    2,
	}
	resp, err := client.Create(createNonAdvice)
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Create task failed: %s", resp.Error)
	}

	// List only advice type issues
	listArgs := &ListArgs{
		IssueType: "advice",
		Status:    "open",
	}
	listResp, err := client.List(listArgs)
	if err != nil {
		t.Fatalf("Failed to list issues: %v", err)
	}
	if !listResp.Success {
		t.Fatalf("List failed: %s", listResp.Error)
	}

	var issues []*types.Issue
	if err := json.Unmarshal(listResp.Data, &issues); err != nil {
		t.Fatalf("Failed to unmarshal issues: %v", err)
	}

	// Verify only advice issues were returned
	if len(issues) != 3 {
		t.Errorf("Expected 3 advice issues, got %d", len(issues))
	}

	for _, issue := range issues {
		if issue.IssueType != types.TypeAdvice {
			t.Errorf("Expected all issues to be type 'advice', got %q for %q", issue.IssueType, issue.Title)
		}
	}
}

// TestAdvice_ListByLabelFilter verifies that advice beads can be filtered by targeting labels.
func TestAdvice_ListByLabelFilter(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create advice with different targeting labels
	testCases := []struct {
		title  string
		labels []string
	}{
		{"Advice for gastown polecats", []string{"rig:gastown", "role:polecat"}},
		{"Advice for gastown witnesses", []string{"rig:gastown", "role:witness"}},
		{"Advice for beads rig", []string{"rig:beads", "role:polecat"}},
		{"Global advice", []string{"global"}},
	}

	for _, tc := range testCases {
		createArgs := &CreateArgs{
			Title:       tc.title,
			Description: "Test advice",
			IssueType:   "advice",
			Priority:    2,
			Labels:      tc.labels,
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Failed to create advice %q: %v", tc.title, err)
		}
		if !resp.Success {
			t.Fatalf("Create failed for %q: %s", tc.title, resp.Error)
		}
	}

	// Filter by rig:gastown label
	listArgs := &ListArgs{
		IssueType: "advice",
		Labels:    []string{"rig:gastown"},
		Status:    "open",
	}
	listResp, err := client.List(listArgs)
	if err != nil {
		t.Fatalf("Failed to list issues: %v", err)
	}
	if !listResp.Success {
		t.Fatalf("List failed: %s", listResp.Error)
	}

	var issues []*types.Issue
	if err := json.Unmarshal(listResp.Data, &issues); err != nil {
		t.Fatalf("Failed to unmarshal issues: %v", err)
	}

	// Should return 2 issues (both gastown ones)
	if len(issues) != 2 {
		t.Errorf("Expected 2 issues with rig:gastown, got %d", len(issues))
	}

	// Filter by role:polecat
	listArgs2 := &ListArgs{
		IssueType: "advice",
		Labels:    []string{"role:polecat"},
		Status:    "open",
	}
	listResp2, err := client.List(listArgs2)
	if err != nil {
		t.Fatalf("Failed to list issues: %v", err)
	}
	if !listResp2.Success {
		t.Fatalf("List failed: %s", listResp2.Error)
	}

	var issues2 []*types.Issue
	if err := json.Unmarshal(listResp2.Data, &issues2); err != nil {
		t.Fatalf("Failed to unmarshal issues: %v", err)
	}

	// Should return 2 issues (gastown/polecat and beads/polecat)
	if len(issues2) != 2 {
		t.Errorf("Expected 2 issues with role:polecat, got %d", len(issues2))
	}

	// Filter by both rig:gastown AND role:polecat (AND semantics)
	listArgs3 := &ListArgs{
		IssueType: "advice",
		Labels:    []string{"rig:gastown", "role:polecat"},
		Status:    "open",
	}
	listResp3, err := client.List(listArgs3)
	if err != nil {
		t.Fatalf("Failed to list issues: %v", err)
	}
	if !listResp3.Success {
		t.Fatalf("List failed: %s", listResp3.Error)
	}

	var issues3 []*types.Issue
	if err := json.Unmarshal(listResp3.Data, &issues3); err != nil {
		t.Fatalf("Failed to unmarshal issues: %v", err)
	}

	// Should return 1 issue (only gastown/polecat)
	if len(issues3) != 1 {
		t.Errorf("Expected 1 issue with both rig:gastown and role:polecat, got %d", len(issues3))
	}
}

// TestAdvice_DeleteClose verifies that advice beads can be closed and removed.
func TestAdvice_DeleteClose(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create an advice bead
	createArgs := &CreateArgs{
		Title:       "Advice to be closed",
		Description: "Test advice",
		IssueType:   "advice",
		Priority:    2,
	}
	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if !createResp.Success {
		t.Fatalf("Create failed: %s", createResp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(createResp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Verify it exists
	stored, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if stored.Status != types.StatusOpen {
		t.Errorf("Expected status 'open', got %q", stored.Status)
	}

	// Close the advice
	closeResp, err := client.CloseIssue(&CloseArgs{
		ID:     issue.ID,
		Reason: "No longer needed",
	})
	if err != nil {
		t.Fatalf("Failed to close advice: %v", err)
	}
	if !closeResp.Success {
		t.Fatalf("Close failed: %s", closeResp.Error)
	}

	// Verify it's closed
	stored, err = store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue after close: %v", err)
	}
	if stored.Status != types.StatusClosed {
		t.Errorf("Expected status 'closed' after close, got %q", stored.Status)
	}

	// Verify closed advice is not returned in default list
	listArgs := &ListArgs{
		IssueType: "advice",
		Status:    "open",
	}
	listResp, err := client.List(listArgs)
	if err != nil {
		t.Fatalf("Failed to list issues: %v", err)
	}
	if !listResp.Success {
		t.Fatalf("List failed: %s", listResp.Error)
	}

	var issues []*types.Issue
	if err := json.Unmarshal(listResp.Data, &issues); err != nil {
		t.Fatalf("Failed to unmarshal issues: %v", err)
	}

	for _, i := range issues {
		if i.ID == issue.ID {
			t.Error("Closed advice should not be returned in open issues list")
		}
	}
}

// TestAdvice_HookFieldsOnlyValidForAdviceType verifies that hook fields are rejected
// for non-advice issue types.
func TestAdvice_HookFieldsOnlyValidForAdviceType(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to create a task with advice hook fields
	createArgs := &CreateArgs{
		Title:               "Regular task with hook fields",
		Description:         "This should fail validation",
		IssueType:           "task",
		Priority:            2,
		AdviceHookCommand:   "make test", // Should be rejected for non-advice types
		AdviceHookTrigger:   "before-commit",
		AdviceHookTimeout:   30,
		AdviceHookOnFailure: "warn",
	}
	resp, err := client.Create(createArgs)

	// The operation should fail because hook fields are only valid for advice type
	if err == nil && resp.Success {
		t.Error("Expected creation to fail when using hook fields on non-advice type")
	}

	// Verify the error message mentions the constraint
	if err != nil {
		if !containsSubstr(err.Error(), "advice") {
			t.Logf("Error message: %s", err.Error())
		}
	} else if resp.Error != "" {
		if !containsSubstr(resp.Error, "advice") {
			t.Logf("Error message: %s", resp.Error)
		}
	}
}

// TestAdvice_InvalidTriggerRejected verifies that invalid trigger values are rejected.
func TestAdvice_InvalidTriggerRejected(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to create advice with invalid trigger
	createArgs := &CreateArgs{
		Title:             "Advice with invalid trigger",
		Description:       "This should fail validation",
		IssueType:         "advice",
		Priority:          2,
		AdviceHookCommand: "make test",
		AdviceHookTrigger: "invalid-trigger-value",
	}
	resp, err := client.Create(createArgs)

	// Should fail validation
	if err == nil && resp.Success {
		t.Error("Expected creation to fail with invalid trigger value")
	}
}

// TestAdvice_InvalidOnFailureRejected verifies that invalid on_failure values are rejected.
func TestAdvice_InvalidOnFailureRejected(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to create advice with invalid on_failure
	createArgs := &CreateArgs{
		Title:               "Advice with invalid on_failure",
		Description:         "This should fail validation",
		IssueType:           "advice",
		Priority:            2,
		AdviceHookCommand:   "make test",
		AdviceHookTrigger:   "before-commit",
		AdviceHookOnFailure: "invalid-failure-mode",
	}
	resp, err := client.Create(createArgs)

	// Should fail validation
	if err == nil && resp.Success {
		t.Error("Expected creation to fail with invalid on_failure value")
	}
}

// TestAdvice_TimeoutValidation verifies that timeout is validated within bounds.
func TestAdvice_TimeoutValidation(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to create advice with timeout exceeding max (300 seconds)
	createArgs := &CreateArgs{
		Title:             "Advice with excessive timeout",
		Description:       "This should fail validation",
		IssueType:         "advice",
		Priority:          2,
		AdviceHookCommand: "make test",
		AdviceHookTrigger: "before-commit",
		AdviceHookTimeout: 500, // Exceeds max of 300
	}
	resp, err := client.Create(createArgs)

	// Should fail validation
	if err == nil && resp.Success {
		t.Error("Expected creation to fail with timeout exceeding maximum")
	}
}

// TestAdvice_CommandLengthLimit verifies that overly long commands are rejected.
func TestAdvice_CommandLengthLimit(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a command that exceeds 1000 characters
	longCommand := ""
	for i := 0; i < 1100; i++ {
		longCommand += "x"
	}

	createArgs := &CreateArgs{
		Title:             "Advice with long command",
		Description:       "This should fail validation",
		IssueType:         "advice",
		Priority:          2,
		AdviceHookCommand: longCommand,
		AdviceHookTrigger: "before-commit",
	}
	resp, err := client.Create(createArgs)

	// Should fail validation
	if err == nil && resp.Success {
		t.Error("Expected creation to fail with command exceeding 1000 characters")
	}
}

// TestAdvice_ListWithLabelsAnySemantics verifies OR semantics for label filtering.
func TestAdvice_ListWithLabelsAnySemantics(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create advice with different labels
	testCases := []struct {
		title  string
		labels []string
	}{
		{"Advice A", []string{"priority:high"}},
		{"Advice B", []string{"priority:low"}},
		{"Advice C", []string{"team:frontend"}},
	}

	for _, tc := range testCases {
		createArgs := &CreateArgs{
			Title:       tc.title,
			Description: "Test advice",
			IssueType:   "advice",
			Priority:    2,
			Labels:      tc.labels,
		}
		resp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Failed to create advice %q: %v", tc.title, err)
		}
		if !resp.Success {
			t.Fatalf("Create failed for %q: %s", tc.title, resp.Error)
		}
	}

	// Use LabelsAny (OR semantics) - should match advice with priority:high OR team:frontend
	listArgs := &ListArgs{
		IssueType: "advice",
		LabelsAny: []string{"priority:high", "team:frontend"},
		Status:    "open",
	}
	listResp, err := client.List(listArgs)
	if err != nil {
		t.Fatalf("Failed to list issues: %v", err)
	}
	if !listResp.Success {
		t.Fatalf("List failed: %s", listResp.Error)
	}

	var issues []*types.Issue
	if err := json.Unmarshal(listResp.Data, &issues); err != nil {
		t.Fatalf("Failed to unmarshal issues: %v", err)
	}

	// Should return 2 issues (A with priority:high, C with team:frontend)
	if len(issues) != 2 {
		t.Errorf("Expected 2 issues with OR label filter, got %d", len(issues))
		for _, i := range issues {
			t.Logf("  - %s", i.Title)
		}
	}
}

// ============================================================================
// Hook Field Persistence Tests
// These tests document the expected behavior for advice hook field persistence.
// KNOWN ISSUE: Storage layer doesn't persist advice_hook_* fields.
// See: internal/storage/sqlite/issues.go - insertIssue/scanIssues need updating.
// ============================================================================

// TestAdvice_CreateWithHookFields_StorageGap verifies advice creation with hook fields.
func TestAdvice_CreateWithHookFields_StorageGap(t *testing.T) {

	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create an advice bead with all hook fields
	createArgs := &CreateArgs{
		Title:               "Run tests before commit",
		Description:         "Ensures all unit tests pass before allowing commits",
		IssueType:           "advice",
		Priority:            2,
		AdviceHookCommand:   "make test",
		AdviceHookTrigger:   "before-commit",
		AdviceHookTimeout:   60,
		AdviceHookOnFailure: "block",
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Create failed: %s", resp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Verify issue was created with correct type
	if issue.IssueType != types.TypeAdvice {
		t.Errorf("Expected issue_type='advice', got %q", issue.IssueType)
	}

	// Retrieve from store to verify persistence
	stored, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue from store: %v", err)
	}

	// Verify all hook fields round-tripped correctly
	if stored.AdviceHookCommand != "make test" {
		t.Errorf("AdviceHookCommand: expected 'make test', got %q", stored.AdviceHookCommand)
	}
	if stored.AdviceHookTrigger != "before-commit" {
		t.Errorf("AdviceHookTrigger: expected 'before-commit', got %q", stored.AdviceHookTrigger)
	}
	if stored.AdviceHookTimeout != 60 {
		t.Errorf("AdviceHookTimeout: expected 60, got %d", stored.AdviceHookTimeout)
	}
	if stored.AdviceHookOnFailure != "block" {
		t.Errorf("AdviceHookOnFailure: expected 'block', got %q", stored.AdviceHookOnFailure)
	}
}

// TestAdvice_UpdateHookFields_StorageGap verifies advice hook field updates.
func TestAdvice_UpdateHookFields_StorageGap(t *testing.T) {

	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create initial advice
	createArgs := &CreateArgs{
		Title:               "Initial advice",
		Description:         "Test advice",
		IssueType:           "advice",
		Priority:            2,
		AdviceHookCommand:   "make test",
		AdviceHookTrigger:   "before-commit",
		AdviceHookTimeout:   30,
		AdviceHookOnFailure: "warn",
	}
	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if !createResp.Success {
		t.Fatalf("Create failed: %s", createResp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(createResp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Update hook fields
	newCommand := "make test && make lint"
	newTrigger := "before-push"
	newTimeout := 120
	newOnFailure := "block"

	updateArgs := &UpdateArgs{
		ID:                  issue.ID,
		AdviceHookCommand:   &newCommand,
		AdviceHookTrigger:   &newTrigger,
		AdviceHookTimeout:   &newTimeout,
		AdviceHookOnFailure: &newOnFailure,
	}
	updateResp, err := client.Update(updateArgs)
	if err != nil {
		t.Fatalf("Failed to update advice: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Update failed: %s", updateResp.Error)
	}

	// Verify updates persisted
	stored, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue from store: %v", err)
	}

	if stored.AdviceHookCommand != newCommand {
		t.Errorf("AdviceHookCommand: expected %q, got %q", newCommand, stored.AdviceHookCommand)
	}
	if stored.AdviceHookTrigger != newTrigger {
		t.Errorf("AdviceHookTrigger: expected %q, got %q", newTrigger, stored.AdviceHookTrigger)
	}
	if stored.AdviceHookTimeout != newTimeout {
		t.Errorf("AdviceHookTimeout: expected %d, got %d", newTimeout, stored.AdviceHookTimeout)
	}
	if stored.AdviceHookOnFailure != newOnFailure {
		t.Errorf("AdviceHookOnFailure: expected %q, got %q", newOnFailure, stored.AdviceHookOnFailure)
	}
}

// TestAdvice_UpdateSubscriptions_StorageGap verifies subscription field updates.
func TestAdvice_UpdateSubscriptions_StorageGap(t *testing.T) {

	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create an agent bead (subscriptions are typically on agent beads, not advice beads)
	createArgs := &CreateArgs{
		Title:       "Test agent for subscriptions",
		Description: "Test agent",
		IssueType:   "task",
		Priority:    2,
		Labels:      []string{"gt:agent"},
		RoleType:    "polecat",
		Rig:         "testrig",
	}
	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	if !createResp.Success {
		t.Fatalf("Create failed: %s", createResp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(createResp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Update subscription fields
	updateArgs := &UpdateArgs{
		ID:                         issue.ID,
		AdviceSubscriptions:        []string{"team:backend", "priority:high"},
		AdviceSubscriptionsExclude: []string{"verbose-logging"},
	}
	updateResp, err := client.Update(updateArgs)
	if err != nil {
		t.Fatalf("Failed to update subscriptions: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Update failed: %s", updateResp.Error)
	}

	// Verify updates persisted
	stored, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue from store: %v", err)
	}

	// Check subscriptions
	if len(stored.AdviceSubscriptions) != 2 {
		t.Errorf("Expected 2 subscriptions, got %d: %v", len(stored.AdviceSubscriptions), stored.AdviceSubscriptions)
	} else {
		subsSet := make(map[string]bool)
		for _, sub := range stored.AdviceSubscriptions {
			subsSet[sub] = true
		}
		if !subsSet["team:backend"] {
			t.Error("Expected subscription 'team:backend' not found")
		}
		if !subsSet["priority:high"] {
			t.Error("Expected subscription 'priority:high' not found")
		}
	}

	// Check exclusions
	if len(stored.AdviceSubscriptionsExclude) != 1 {
		t.Errorf("Expected 1 exclusion, got %d: %v", len(stored.AdviceSubscriptionsExclude), stored.AdviceSubscriptionsExclude)
	} else if stored.AdviceSubscriptionsExclude[0] != "verbose-logging" {
		t.Errorf("Expected exclusion 'verbose-logging', got %q", stored.AdviceSubscriptionsExclude[0])
	}
}

// TestAdvice_ShowReturnsHookFields_StorageGap verifies that Show returns hook fields.
func TestAdvice_ShowReturnsHookFields_StorageGap(t *testing.T) {

	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create advice with all hook fields
	createArgs := &CreateArgs{
		Title:               "Full advice for show test",
		Description:         "Tests that show returns all fields",
		IssueType:           "advice",
		Priority:            3,
		AdviceHookCommand:   "./run-checks.sh",
		AdviceHookTrigger:   "before-push",
		AdviceHookTimeout:   90,
		AdviceHookOnFailure: "block",
		Labels:              []string{"rig:testrig", "role:crew"},
	}
	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create advice: %v", err)
	}
	if !createResp.Success {
		t.Fatalf("Create failed: %s", createResp.Error)
	}

	var created types.Issue
	if err := json.Unmarshal(createResp.Data, &created); err != nil {
		t.Fatalf("Failed to unmarshal created issue: %v", err)
	}

	// Show the issue
	showResp, err := client.Show(&ShowArgs{ID: created.ID})
	if err != nil {
		t.Fatalf("Failed to show issue: %v", err)
	}
	if !showResp.Success {
		t.Fatalf("Show failed: %s", showResp.Error)
	}

	var shown types.Issue
	if err := json.Unmarshal(showResp.Data, &shown); err != nil {
		t.Fatalf("Failed to unmarshal shown issue: %v", err)
	}

	// Verify all hook fields are present
	if shown.AdviceHookCommand != "./run-checks.sh" {
		t.Errorf("Show: AdviceHookCommand expected './run-checks.sh', got %q", shown.AdviceHookCommand)
	}
	if shown.AdviceHookTrigger != "before-push" {
		t.Errorf("Show: AdviceHookTrigger expected 'before-push', got %q", shown.AdviceHookTrigger)
	}
	if shown.AdviceHookTimeout != 90 {
		t.Errorf("Show: AdviceHookTimeout expected 90, got %d", shown.AdviceHookTimeout)
	}
	if shown.AdviceHookOnFailure != "block" {
		t.Errorf("Show: AdviceHookOnFailure expected 'block', got %q", shown.AdviceHookOnFailure)
	}
}

// TestAdvice_ValidTriggerTypes verifies all valid trigger types are accepted.
// Note: This only tests RPC-level validation, not storage persistence.
func TestAdvice_ValidTriggerTypes(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	triggers := []string{
		"session-end",
		"before-commit",
		"before-push",
		"before-handoff",
	}

	for _, trigger := range triggers {
		t.Run("trigger_"+trigger, func(t *testing.T) {
			createArgs := &CreateArgs{
				Title:             "Advice with " + trigger + " trigger",
				Description:       "Test advice with valid trigger",
				IssueType:         "advice",
				Priority:          2,
				AdviceHookCommand: "echo test",
				AdviceHookTrigger: trigger,
			}
			resp, err := client.Create(createArgs)
			if err != nil {
				t.Fatalf("Failed to create advice with trigger %s: %v", trigger, err)
			}
			if !resp.Success {
				t.Fatalf("Create failed for trigger %s: %s", trigger, resp.Error)
			}

			var issue types.Issue
			if err := json.Unmarshal(resp.Data, &issue); err != nil {
				t.Fatalf("Failed to unmarshal issue: %v", err)
			}

			// Verify issue was created (storage persistence tested separately)
			if issue.IssueType != types.TypeAdvice {
				t.Errorf("Expected type 'advice', got %q", issue.IssueType)
			}
		})
	}
}

// TestAdvice_ValidOnFailureModes verifies all valid on_failure modes are accepted.
// Note: This only tests RPC-level validation, not storage persistence.
func TestAdvice_ValidOnFailureModes(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	modes := []string{"block", "warn", "ignore"}

	for _, mode := range modes {
		t.Run("on_failure_"+mode, func(t *testing.T) {
			createArgs := &CreateArgs{
				Title:               "Advice with " + mode + " on failure",
				Description:         "Test advice with valid on_failure",
				IssueType:           "advice",
				Priority:            2,
				AdviceHookCommand:   "echo test",
				AdviceHookTrigger:   "before-commit",
				AdviceHookOnFailure: mode,
			}
			resp, err := client.Create(createArgs)
			if err != nil {
				t.Fatalf("Failed to create advice with on_failure %s: %v", mode, err)
			}
			if !resp.Success {
				t.Fatalf("Create failed for on_failure %s: %s", mode, resp.Error)
			}

			var issue types.Issue
			if err := json.Unmarshal(resp.Data, &issue); err != nil {
				t.Fatalf("Failed to unmarshal issue: %v", err)
			}

			// Verify issue was created (storage persistence tested separately)
			if issue.IssueType != types.TypeAdvice {
				t.Errorf("Expected type 'advice', got %q", issue.IssueType)
			}
		})
	}
}
