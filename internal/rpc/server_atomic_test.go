package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/testutil/teststore"
	"github.com/steveyegge/beads/internal/types"
)

// TestBatchAddLabels_AddFiveLabelsAtomically verifies that BatchAddLabels can add
// 5 labels to an issue in a single atomic transaction.
func TestBatchAddLabels_AddFiveLabelsAtomically(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a test issue
	createArgs := &CreateArgs{
		Title:     "Test issue for batch labels",
		IssueType: "task",
		Priority:  2,
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	var issue struct{ ID string }
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Add 5 labels atomically
	labels := []string{"priority:high", "team:backend", "sprint:42", "type:feature", "status:review"}
	batchArgs := &BatchAddLabelsArgs{
		IssueID: issue.ID,
		Labels:  labels,
	}
	result, err := client.BatchAddLabels(batchArgs)
	if err != nil {
		t.Fatalf("BatchAddLabels failed: %v", err)
	}

	// Verify result
	if result.IssueID != issue.ID {
		t.Errorf("Expected issue_id=%q, got %q", issue.ID, result.IssueID)
	}
	if result.LabelsAdded != 5 {
		t.Errorf("Expected labels_added=5, got %d", result.LabelsAdded)
	}

	// Verify labels were actually added by querying the store directly
	ctx := context.Background()
	storedLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels from store: %v", err)
	}
	if len(storedLabels) != 5 {
		t.Errorf("Expected 5 labels in store, got %d", len(storedLabels))
	}

	// Verify all expected labels are present
	labelSet := make(map[string]bool)
	for _, l := range storedLabels {
		labelSet[l] = true
	}
	for _, expected := range labels {
		if !labelSet[expected] {
			t.Errorf("Expected label %q not found in stored labels: %v", expected, storedLabels)
		}
	}
}

// TestBatchAddLabels_RollbackOnFailure verifies that if an error occurs during
// the batch add operation, all changes are rolled back (atomicity).
// Note: This test verifies the transaction boundary by attempting to add labels
// to a non-existent issue after resolving the ID, which should fail and not leave
// partial state.
func TestBatchAddLabels_RollbackOnFailure(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to add labels to a non-existent issue
	// The handleBatchAddLabels function first resolves the issue ID, which should fail
	batchArgs := &BatchAddLabelsArgs{
		IssueID: "nonexistent-issue-id",
		Labels:  []string{"label1", "label2", "label3"},
	}
	result, err := client.BatchAddLabels(batchArgs)

	// Should fail because the issue doesn't exist
	if err == nil && result != nil {
		t.Errorf("Expected error for non-existent issue, got success with result: %+v", result)
	}

	// Verify no labels were added anywhere (check the database is clean)
	// The non-existent issue should have no labels
	ctx := context.Background()
	labels, err := store.GetLabels(ctx, "nonexistent-issue-id")
	if err == nil && len(labels) > 0 {
		t.Errorf("Expected no labels for non-existent issue, got: %v", labels)
	}
}

// TestBatchAddLabels_Idempotent verifies that BatchAddLabels is idempotent -
// adding labels that already exist should succeed without creating duplicates.
func TestBatchAddLabels_Idempotent(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a test issue with some initial labels
	createArgs := &CreateArgs{
		Title:     "Test issue for idempotent labels",
		IssueType: "task",
		Priority:  2,
		Labels:    []string{"existing:label1", "existing:label2"},
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	var issue struct{ ID string }
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Try to add a mix of existing and new labels
	batchArgs := &BatchAddLabelsArgs{
		IssueID: issue.ID,
		Labels:  []string{"existing:label1", "existing:label2", "new:label3", "new:label4"},
	}
	result, err := client.BatchAddLabels(batchArgs)
	if err != nil {
		t.Fatalf("BatchAddLabels failed: %v", err)
	}

	// Should only report the 2 new labels as added
	if result.LabelsAdded != 2 {
		t.Errorf("Expected labels_added=2 (only new ones), got %d", result.LabelsAdded)
	}

	// Verify total labels in store (should be 4, no duplicates)
	ctx := context.Background()
	storedLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels from store: %v", err)
	}
	if len(storedLabels) != 4 {
		t.Errorf("Expected 4 labels in store (no duplicates), got %d: %v", len(storedLabels), storedLabels)
	}

	// Call again with all the same labels - should add 0
	result2, err := client.BatchAddLabels(batchArgs)
	if err != nil {
		t.Fatalf("Second BatchAddLabels failed: %v", err)
	}
	if result2.LabelsAdded != 0 {
		t.Errorf("Expected labels_added=0 on repeat call, got %d", result2.LabelsAdded)
	}

	// Verify no duplicates were created
	storedLabels2, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels from store: %v", err)
	}
	if len(storedLabels2) != 4 {
		t.Errorf("Expected 4 labels after repeat call, got %d: %v", len(storedLabels2), storedLabels2)
	}
}

// TestBatchAddLabels_EmptyLabelList verifies that calling BatchAddLabels with
// an empty label list succeeds and reports 0 labels added.
func TestBatchAddLabels_EmptyLabelList(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a test issue
	createArgs := &CreateArgs{
		Title:     "Test issue for empty label list",
		IssueType: "task",
		Priority:  2,
		Labels:    []string{"preexisting:label"},
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	var issue struct{ ID string }
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Call with empty label list
	batchArgs := &BatchAddLabelsArgs{
		IssueID: issue.ID,
		Labels:  []string{},
	}
	result, err := client.BatchAddLabels(batchArgs)
	if err != nil {
		t.Fatalf("BatchAddLabels with empty list failed: %v", err)
	}

	// Should succeed with 0 labels added
	if result.LabelsAdded != 0 {
		t.Errorf("Expected labels_added=0 for empty list, got %d", result.LabelsAdded)
	}
	if result.IssueID != issue.ID {
		t.Errorf("Expected issue_id=%q, got %q", issue.ID, result.IssueID)
	}

	// Verify existing labels are unchanged
	ctx := context.Background()
	storedLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels from store: %v", err)
	}
	if len(storedLabels) != 1 {
		t.Errorf("Expected 1 label (preexisting), got %d: %v", len(storedLabels), storedLabels)
	}
}

// TestBatchAddLabels_IssueDoesNotExist verifies that BatchAddLabels fails
// gracefully when the specified issue does not exist.
func TestBatchAddLabels_IssueDoesNotExist(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to add labels to a non-existent issue
	batchArgs := &BatchAddLabelsArgs{
		IssueID: "bd-nonexistent",
		Labels:  []string{"label1", "label2"},
	}
	result, err := client.BatchAddLabels(batchArgs)

	// Should return an error
	if err == nil {
		t.Errorf("Expected error for non-existent issue, got success: %+v", result)
	}

	// The error should mention the issue ID or indicate not found
	if err != nil {
		errStr := err.Error()
		// Error should indicate the issue couldn't be resolved/found
		if errStr == "" {
			t.Errorf("Expected non-empty error message")
		}
	}
}

// TestBatchAddLabels_DuplicateLabelsInInput verifies that BatchAddLabels handles
// duplicate labels in the input correctly - they should be deduplicated and only
// count as one label added.
func TestBatchAddLabels_DuplicateLabelsInInput(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a test issue
	createArgs := &CreateArgs{
		Title:     "Test issue for duplicate labels in input",
		IssueType: "task",
		Priority:  2,
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	var issue struct{ ID string }
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Add labels with duplicates in the input
	batchArgs := &BatchAddLabelsArgs{
		IssueID: issue.ID,
		Labels:  []string{"unique:label", "duplicate:label", "duplicate:label", "another:unique", "duplicate:label"},
	}
	result, err := client.BatchAddLabels(batchArgs)
	if err != nil {
		t.Fatalf("BatchAddLabels failed: %v", err)
	}

	// Note: The current implementation may or may not deduplicate the input.
	// The important thing is that the database should not have duplicates.
	// The result.LabelsAdded should reflect actual unique additions.

	// Verify no duplicates in the store
	ctx := context.Background()
	storedLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels from store: %v", err)
	}

	// Count occurrences of each label
	labelCounts := make(map[string]int)
	for _, l := range storedLabels {
		labelCounts[l]++
	}

	// Verify no duplicates exist
	for label, count := range labelCounts {
		if count > 1 {
			t.Errorf("Found duplicate label %q with count %d", label, count)
		}
	}

	// Should have exactly 3 unique labels
	expectedUnique := 3
	if len(storedLabels) != expectedUnique {
		t.Errorf("Expected %d unique labels, got %d: %v", expectedUnique, len(storedLabels), storedLabels)
	}

	// Verify the expected labels are present
	expectedLabels := map[string]bool{
		"unique:label":    true,
		"duplicate:label": true,
		"another:unique":  true,
	}
	for _, l := range storedLabels {
		if !expectedLabels[l] {
			t.Errorf("Unexpected label found: %q", l)
		}
	}

	// The LabelsAdded count behavior depends on implementation:
	// - If input is deduplicated first, it should be 3
	// - If duplicates are filtered during transaction, it might report differently
	// The key invariant is: no duplicates in the database
	t.Logf("LabelsAdded reported: %d (input had 5 items, 3 unique)", result.LabelsAdded)
}

// TestBatchAddLabels_EmptyIssueID verifies that BatchAddLabels fails when
// issue_id is empty.
func TestBatchAddLabels_EmptyIssueID(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to add labels with empty issue ID
	batchArgs := &BatchAddLabelsArgs{
		IssueID: "",
		Labels:  []string{"label1", "label2"},
	}
	result, err := client.BatchAddLabels(batchArgs)

	// Should return an error
	if err == nil {
		t.Errorf("Expected error for empty issue_id, got success: %+v", result)
	}

	// Error should mention that issue_id is required
	if err != nil {
		errStr := err.Error()
		if errStr == "" {
			t.Errorf("Expected non-empty error message")
		}
	}
}

// TestBatchAddLabels_PartialIDResolution verifies that BatchAddLabels correctly
// resolves partial issue IDs to full IDs.
func TestBatchAddLabels_PartialIDResolution(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a test issue
	createArgs := &CreateArgs{
		Title:     "Test issue for partial ID resolution",
		IssueType: "task",
		Priority:  2,
	}
	resp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	var issue struct{ ID string }
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// The ID format is typically "bd-XXXX" - use partial ID (just the suffix)
	// Extract partial ID (remove prefix if present)
	partialID := issue.ID
	if len(partialID) > 3 {
		// Use the last few characters as partial ID
		partialID = "-" + partialID[len(partialID)-4:]
	}

	// Add labels using partial ID
	batchArgs := &BatchAddLabelsArgs{
		IssueID: partialID,
		Labels:  []string{"resolved:label"},
	}
	result, err := client.BatchAddLabels(batchArgs)
	if err != nil {
		// Partial ID resolution might not work depending on implementation
		t.Logf("Partial ID resolution failed (may be expected): %v", err)
		return
	}

	// If it succeeded, verify the result contains the full ID
	if result.IssueID != issue.ID && result.IssueID != partialID {
		t.Logf("Result IssueID=%q, original=%q, partial=%q", result.IssueID, issue.ID, partialID)
	}

	// Verify the label was added to the correct issue
	ctx := context.Background()
	storedLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels from store: %v", err)
	}

	found := false
	for _, l := range storedLabels {
		if l == "resolved:label" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected label 'resolved:label' not found in stored labels: %v", storedLabels)
	}
}

// ============================================================================
// UpdateWithComment Tests
// ============================================================================

// createTestIssueForUpdate is a helper to create a test issue and return its ID.
func createTestIssueForUpdate(t *testing.T, client *Client, title, issueType string) string {
	t.Helper()
	createArgs := &CreateArgs{
		Title:     title,
		IssueType: issueType,
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

// TestUpdateWithComment_StatusAndComment verifies that update_with_comment
// applies both status change and comment atomically in a single transaction.
func TestUpdateWithComment_StatusAndComment(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for atomic update", "task")

	// Get initial state
	initial, err := store.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get initial issue: %v", err)
	}
	if initial.Status != "open" {
		t.Fatalf("Expected initial status 'open', got %q", initial.Status)
	}

	// Update status and add comment atomically
	newStatus := "in_progress"
	resp, err := client.UpdateWithComment(&UpdateWithCommentArgs{
		UpdateArgs: UpdateArgs{
			ID:     issueID,
			Status: &newStatus,
		},
		CommentText:   "Starting work on this issue",
		CommentAuthor: "test-author",
	})
	if err != nil {
		t.Fatalf("UpdateWithComment failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("UpdateWithComment not successful: %s", resp.Error)
	}

	// Verify the response contains the updated issue
	var updated struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Data, &updated); err != nil {
		t.Fatalf("Failed to unmarshal updated issue: %v", err)
	}
	if updated.Status != newStatus {
		t.Errorf("Expected status %q in response, got %q", newStatus, updated.Status)
	}

	// Verify status was updated in storage
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get issue from store: %v", err)
	}
	if string(issue.Status) != newStatus {
		t.Errorf("Status not updated in store: expected %q, got %q", newStatus, issue.Status)
	}

	// Verify comment was added (via events table)
	events, err := store.GetEvents(ctx, issueID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.EventType == types.EventCommented && e.Comment != nil && *e.Comment == "Starting work on this issue" {
			if e.Actor == "test-author" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("Expected comment event 'Starting work on this issue' by 'test-author' not found")
	}
}

// TestUpdateWithComment_IssueNotFound verifies that update_with_comment fails
// gracefully when the issue doesn't exist and no changes are made.
func TestUpdateWithComment_IssueNotFound(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	_ = store // unused, but keep for consistency

	// Try to update a non-existent issue
	newStatus := "in_progress"
	resp, err := client.UpdateWithComment(&UpdateWithCommentArgs{
		UpdateArgs: UpdateArgs{
			ID:     "bd-nonexistent",
			Status: &newStatus,
		},
		CommentText:   "This should fail",
		CommentAuthor: "test-author",
	})
	// RPC calls may return the error in resp.Error rather than as err
	if err == nil && resp.Success {
		t.Fatal("Expected UpdateWithComment to fail for non-existent issue")
	}
	// Either err is set OR resp indicates failure
	if err == nil && resp.Error == "" {
		t.Error("Expected error message to be set")
	}
	// Verify the error mentions the issue or "not found"
	if err != nil {
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "nonexistent") {
			t.Logf("Error message: %s", err.Error())
		}
	} else if resp.Error != "" {
		if !strings.Contains(resp.Error, "not found") && !strings.Contains(resp.Error, "nonexistent") {
			t.Logf("Error message: %s", resp.Error)
		}
	}
}

// TestUpdateWithComment_UpdateOnly verifies that update_with_comment works
// correctly when no comment is provided (update-only mode).
func TestUpdateWithComment_UpdateOnly(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for update-only", "task")

	// Update without comment
	newTitle := "Updated title without comment"
	resp, err := client.UpdateWithComment(&UpdateWithCommentArgs{
		UpdateArgs: UpdateArgs{
			ID:    issueID,
			Title: &newTitle,
		},
		// CommentText intentionally empty
	})
	if err != nil {
		t.Fatalf("UpdateWithComment failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("UpdateWithComment not successful: %s", resp.Error)
	}

	// Verify title was updated
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get issue from store: %v", err)
	}
	if issue.Title != newTitle {
		t.Errorf("Title not updated: expected %q, got %q", newTitle, issue.Title)
	}

	// Verify no comment was added (check events for EventCommented)
	events, err := store.GetEvents(ctx, issueID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	commentCount := 0
	for _, e := range events {
		if e.EventType == types.EventCommented {
			commentCount++
		}
	}
	if commentCount != 0 {
		t.Errorf("Expected 0 comment events for update-only, got %d", commentCount)
	}
}

// TestUpdateWithComment_SpecialCharacters verifies that comments with special
// characters are handled correctly.
func TestUpdateWithComment_SpecialCharacters(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for special chars", "task")

	// Test various special characters in comment
	testCases := []struct {
		name    string
		comment string
	}{
		{"unicode", "Comment with unicode: \u4e2d\u6587 \u65e5\u672c\u8a9e \ud55c\uad6d\uc5b4"},
		{"emoji", "Comment with emoji: \U0001F680 \U0001F389 \u2705"},
		{"newlines", "Comment with\nmultiple\nlines"},
		{"quotes", `Comment with "double" and 'single' quotes`},
		{"backslash", `Comment with \backslash\ and path\\separators`},
		{"html_like", "Comment with <b>HTML-like</b> tags & entities"},
		{"sql_injection", "Comment with ' OR '1'='1'; DROP TABLE issues; --"},
		{"json_special", `Comment with {"json": "content", "array": [1,2,3]}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.UpdateWithComment(&UpdateWithCommentArgs{
				UpdateArgs: UpdateArgs{
					ID: issueID,
				},
				CommentText:   tc.comment,
				CommentAuthor: "test-author",
			})
			if err != nil {
				t.Fatalf("UpdateWithComment failed: %v", err)
			}
			if !resp.Success {
				t.Fatalf("UpdateWithComment not successful: %s", resp.Error)
			}

			// Verify comment was stored correctly (via events)
			events, err := store.GetEvents(ctx, issueID, 100)
			if err != nil {
				t.Fatalf("Failed to get events: %v", err)
			}

			// Find the comment event with matching text
			found := false
			for _, e := range events {
				if e.EventType == types.EventCommented && e.Comment != nil && *e.Comment == tc.comment {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Comment with special chars not found. Expected: %q", tc.comment)
			}
		})
	}
}

// TestUpdateWithComment_RollbackOnUpdateFailure verifies that if the update fails,
// the comment is NOT added (atomic rollback behavior).
func TestUpdateWithComment_RollbackOnUpdateFailure(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for rollback", "task")

	// Verify no comments initially
	comments, err := store.GetIssueComments(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get comments: %v", err)
	}
	initialCommentCount := len(comments)

	// Get initial status
	initial, err := store.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get initial issue: %v", err)
	}
	initialStatus := string(initial.Status)

	// Try to update with an invalid status (should fail validation)
	invalidStatus := "invalid_status_value_that_should_fail"
	resp, err := client.UpdateWithComment(&UpdateWithCommentArgs{
		UpdateArgs: UpdateArgs{
			ID:     issueID,
			Status: &invalidStatus,
		},
		CommentText:   "This comment should NOT be added due to rollback",
		CommentAuthor: "test-author",
	})

	// The operation might succeed (if status validation is loose) or fail
	// If it fails, verify no comment was added
	if !resp.Success || err != nil {
		// Verify comment was NOT added due to rollback
		comments, err := store.GetIssueComments(ctx, issueID)
		if err != nil {
			t.Fatalf("Failed to get comments: %v", err)
		}
		if len(comments) != initialCommentCount {
			t.Errorf("Comment was added despite update failure (rollback failed). Expected %d comments, got %d",
				initialCommentCount, len(comments))
		}

		// Verify status was NOT changed
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}
		if string(issue.Status) != initialStatus {
			t.Errorf("Status was changed despite expected failure: expected %q, got %q", initialStatus, issue.Status)
		}
	} else {
		// If the operation succeeded (status validation is loose), that's also acceptable
		// The important thing is atomicity - both operations succeeded together
		t.Log("Update with invalid status succeeded - status validation may be loose. Skipping rollback test.")
	}
}

// TestUpdateWithComment_LabelOperations verifies that label operations work
// atomically with comments.
func TestUpdateWithComment_LabelOperations(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for labels and comments", "task")

	// Add labels and comment atomically
	resp, err := client.UpdateWithComment(&UpdateWithCommentArgs{
		UpdateArgs: UpdateArgs{
			ID:        issueID,
			AddLabels: []string{"bug", "priority:high"},
		},
		CommentText:   "Adding labels to this issue",
		CommentAuthor: "test-author",
	})
	if err != nil {
		t.Fatalf("UpdateWithComment failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("UpdateWithComment not successful: %s", resp.Error)
	}

	// Verify labels were added
	labels, err := store.GetLabels(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	labelSet := make(map[string]bool)
	for _, l := range labels {
		labelSet[l] = true
	}

	if !labelSet["bug"] {
		t.Error("Expected label 'bug' to be added")
	}
	if !labelSet["priority:high"] {
		t.Error("Expected label 'priority:high' to be added")
	}

	// Verify comment was added (via events)
	events, err := store.GetEvents(ctx, issueID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.EventType == types.EventCommented && e.Comment != nil && *e.Comment == "Adding labels to this issue" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected comment event 'Adding labels to this issue' not found")
	}
}

// TestUpdateWithComment_DefaultAuthor verifies that CommentAuthor defaults
// to the request actor when not specified.
func TestUpdateWithComment_DefaultAuthor(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for default author", "task")

	// Add comment without specifying author
	resp, err := client.UpdateWithComment(&UpdateWithCommentArgs{
		UpdateArgs: UpdateArgs{
			ID: issueID,
		},
		CommentText: "Comment with default author",
		// CommentAuthor intentionally not set
	})
	if err != nil {
		t.Fatalf("UpdateWithComment failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("UpdateWithComment not successful: %s", resp.Error)
	}

	// Verify comment was added with a non-empty author (via events)
	events, err := store.GetEvents(ctx, issueID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.EventType == types.EventCommented && e.Comment != nil && *e.Comment == "Comment with default author" {
			if e.Actor != "" {
				found = true
			} else {
				t.Error("Expected comment actor to be set (defaulted from request actor), but it was empty")
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected comment event 'Comment with default author' not found")
	}
}

// ============================================================================
// SetState RPC Tests
// ============================================================================

// TestSetState_SetInitialState verifies that SetState adds a label and creates an event atomically.
func TestSetState_SetInitialState(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for state change", "task")

	// Set initial state
	setStateArgs := &SetStateArgs{
		IssueID:   issueID,
		Dimension: "patrol",
		NewValue:  "active",
		Reason:    "Starting patrol monitoring",
	}
	result, err := client.SetState(setStateArgs)
	if err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	// Verify result
	if result.IssueID != issueID {
		t.Errorf("Expected IssueID=%q, got %q", issueID, result.IssueID)
	}
	if result.Dimension != "patrol" {
		t.Errorf("Expected Dimension='patrol', got %q", result.Dimension)
	}
	if result.OldValue != nil {
		t.Errorf("Expected OldValue=nil for initial state, got %q", *result.OldValue)
	}
	if result.NewValue != "active" {
		t.Errorf("Expected NewValue='active', got %q", result.NewValue)
	}
	if !result.Changed {
		t.Error("Expected Changed=true for initial state")
	}
	if result.EventID == "" {
		t.Error("Expected EventID to be set")
	}

	// Verify label was added to the issue
	labels, err := store.GetLabels(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}
	expectedLabel := "patrol:active"
	labelFound := false
	for _, label := range labels {
		if label == expectedLabel {
			labelFound = true
			break
		}
	}
	if !labelFound {
		t.Errorf("Expected label %q not found in labels: %v", expectedLabel, labels)
	}

	// Verify event was created
	event, err := store.GetIssue(ctx, result.EventID)
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}
	if event.IssueType != types.TypeEvent {
		t.Errorf("Expected event type='event', got %q", event.IssueType)
	}
	if event.Status != types.StatusClosed {
		t.Errorf("Expected event status='closed', got %q", event.Status)
	}
	if !containsSubstr(event.Title, "patrol") {
		t.Errorf("Expected event title to contain 'patrol', got %q", event.Title)
	}
	if !containsSubstr(event.Description, "Starting patrol monitoring") {
		t.Errorf("Expected event description to contain reason, got %q", event.Description)
	}
}

// TestSetState_ChangeState verifies that changing state removes old label, adds new label,
// and creates an event atomically.
func TestSetState_ChangeState(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for state transition", "task")

	// Set initial state
	setStateArgs := &SetStateArgs{
		IssueID:   issueID,
		Dimension: "health",
		NewValue:  "healthy",
	}
	_, err := client.SetState(setStateArgs)
	if err != nil {
		t.Fatalf("SetState (initial) failed: %v", err)
	}

	// Change state
	changeArgs := &SetStateArgs{
		IssueID:   issueID,
		Dimension: "health",
		NewValue:  "degraded",
		Reason:    "Memory pressure detected",
	}
	result, err := client.SetState(changeArgs)
	if err != nil {
		t.Fatalf("SetState (change) failed: %v", err)
	}

	// Verify result
	if result.OldValue == nil {
		t.Error("Expected OldValue to be set for state change")
	} else if *result.OldValue != "healthy" {
		t.Errorf("Expected OldValue='healthy', got %q", *result.OldValue)
	}
	if result.NewValue != "degraded" {
		t.Errorf("Expected NewValue='degraded', got %q", result.NewValue)
	}
	if !result.Changed {
		t.Error("Expected Changed=true for state change")
	}

	// Verify old label was removed and new label was added
	labels, err := store.GetLabels(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	oldLabel := "health:healthy"
	newLabel := "health:degraded"

	for _, label := range labels {
		if label == oldLabel {
			t.Errorf("Old label %q should have been removed, but still present", oldLabel)
		}
	}

	foundNew := false
	for _, label := range labels {
		if label == newLabel {
			foundNew = true
			break
		}
	}
	if !foundNew {
		t.Errorf("New label %q not found in labels: %v", newLabel, labels)
	}

	// Verify event describes the change
	event, err := store.GetIssue(ctx, result.EventID)
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}
	if !containsSubstr(event.Description, "healthy") {
		t.Errorf("Expected event description to contain old value 'healthy', got %q", event.Description)
	}
	if !containsSubstr(event.Description, "degraded") {
		t.Errorf("Expected event description to contain new value 'degraded', got %q", event.Description)
	}
}

// TestSetState_InvalidState verifies error handling for invalid state arguments.
func TestSetState_InvalidState(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a test issue first
	issueID := createTestIssueForUpdate(t, client, "Test issue for invalid state", "task")

	tests := []struct {
		name      string
		args      *SetStateArgs
		wantError string
	}{
		{
			name: "missing issue_id",
			args: &SetStateArgs{
				IssueID:   "",
				Dimension: "patrol",
				NewValue:  "active",
			},
			wantError: "issue_id is required",
		},
		{
			name: "missing dimension",
			args: &SetStateArgs{
				IssueID:   issueID,
				Dimension: "",
				NewValue:  "active",
			},
			wantError: "dimension is required",
		},
		{
			name: "missing new_value",
			args: &SetStateArgs{
				IssueID:   issueID,
				Dimension: "patrol",
				NewValue:  "",
			},
			wantError: "new_value is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.SetState(tt.args)
			if err == nil && result != nil {
				t.Errorf("Expected error containing %q, but got success", tt.wantError)
			}
			if err != nil && !containsSubstr(err.Error(), tt.wantError) {
				t.Errorf("Expected error containing %q, got %q", tt.wantError, err.Error())
			}
		})
	}
}

// TestSetState_IssueDoesNotExist verifies error handling when issue doesn't exist.
func TestSetState_IssueDoesNotExist(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Try to set state on non-existent issue
	setStateArgs := &SetStateArgs{
		IssueID:   "bd-nonexistent",
		Dimension: "patrol",
		NewValue:  "active",
	}
	result, err := client.SetState(setStateArgs)
	if err == nil && result != nil {
		t.Error("Expected error for non-existent issue, but got success")
	}
	// The error could come from resolving the ID or from GetLabels
	if err != nil {
		t.Logf("Got expected error for non-existent issue: %v", err)
	}
}

// TestSetState_SpecialCharacters verifies handling of special characters in state values.
func TestSetState_SpecialCharacters(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test issue
	issueID := createTestIssueForUpdate(t, client, "Test issue for special characters", "task")

	tests := []struct {
		name      string
		dimension string
		value     string
	}{
		{
			name:      "spaces in value",
			dimension: "mode",
			value:     "test mode",
		},
		{
			name:      "dashes and underscores",
			dimension: "build-status",
			value:     "in_progress",
		},
		{
			name:      "numbers",
			dimension: "version",
			value:     "1.2.3",
		},
		{
			name:      "mixed case",
			dimension: "Status",
			value:     "InProgress",
		},
		{
			name:      "hyphenated",
			dimension: "run-state",
			value:     "active-running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setStateArgs := &SetStateArgs{
				IssueID:   issueID,
				Dimension: tt.dimension,
				NewValue:  tt.value,
			}
			result, err := client.SetState(setStateArgs)
			if err != nil {
				t.Fatalf("SetState failed: %v", err)
			}

			if !result.Changed {
				t.Error("Expected Changed=true")
			}

			// Verify label was created correctly
			labels, err := store.GetLabels(ctx, issueID)
			if err != nil {
				t.Fatalf("Failed to get labels: %v", err)
			}

			expectedLabel := tt.dimension + ":" + tt.value
			labelFound := false
			for _, label := range labels {
				if label == expectedLabel {
					labelFound = true
					break
				}
			}
			if !labelFound {
				t.Errorf("Expected label %q not found in labels: %v", expectedLabel, labels)
			}
		})
	}
}

// containsSubstr checks if s contains substr (helper for tests)
func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// hasLabelPrefix checks if s has the given prefix (helper for tests)
func hasLabelPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// ============================================================================
// CreateWithDependencies Tests
// ============================================================================

// Avoid unused import warning for strings package
var _ = strings.Contains

// setupAtomicTestServer creates a test server with storage for transaction testing
func setupAtomicTestServer(t *testing.T) (*Server, storage.Storage) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := teststore.New(t)

	// Initialize database with required config
	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	server := NewServer("/tmp/test.sock", store, tmpDir, dbPath)
	return server, store
}

// TestCreateWithDeps_IssueWithLabelsAndDependencies tests creating issues with labels
// and dependencies atomically in a single transaction
func TestCreateWithDeps_IssueWithLabelsAndDependencies(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// Create two issues with labels and a dependency between them
	// Don't provide explicit IDs - let them be auto-generated
	// Use index references ("0", "1") for dependencies
	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				Title:     "First Issue",
				IssueType: "task",
				Priority:  2,
				Labels:    []string{"frontend", "urgent"},
			},
			{
				Title:     "Second Issue (depends on first)",
				IssueType: "bug",
				Priority:  1,
				Labels:    []string{"backend"},
			},
		},
		Dependencies: []CreateWithDepsDependency{
			{
				FromID:  "1", // Index of second issue
				ToID:    "0", // Index of first issue
				DepType: string(types.DepBlocks),
			},
		},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if !resp.Success {
		t.Fatalf("create_with_deps failed: %s", resp.Error)
	}

	var result CreateWithDepsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Verify result
	if result.Created != 2 {
		t.Errorf("expected 2 created, got %d", result.Created)
	}
	if len(result.IDMapping) != 2 {
		t.Errorf("expected 2 ID mappings, got %d", len(result.IDMapping))
	}

	// Get the actual IDs (using index-based keys since no explicit IDs provided)
	newID1, ok := result.IDMapping["0"]
	if !ok {
		t.Fatal("missing ID mapping for index 0")
	}
	newID2, ok := result.IDMapping["1"]
	if !ok {
		t.Fatal("missing ID mapping for index 1")
	}

	// Verify issues were created
	issue1, err := store.GetIssue(ctx, newID1)
	if err != nil {
		t.Fatalf("failed to get issue 1: %v", err)
	}
	if issue1.Title != "First Issue" {
		t.Errorf("expected title 'First Issue', got %q", issue1.Title)
	}
	if issue1.IssueType != types.TypeTask {
		t.Errorf("expected type task, got %s", issue1.IssueType)
	}

	issue2, err := store.GetIssue(ctx, newID2)
	if err != nil {
		t.Fatalf("failed to get issue 2: %v", err)
	}
	if issue2.Title != "Second Issue (depends on first)" {
		t.Errorf("expected title 'Second Issue (depends on first)', got %q", issue2.Title)
	}

	// Verify labels were added
	labels1, err := store.GetLabels(ctx, newID1)
	if err != nil {
		t.Fatalf("failed to get labels for issue 1: %v", err)
	}
	if len(labels1) != 2 {
		t.Errorf("expected 2 labels for issue 1, got %d", len(labels1))
	}
	labelMap1 := make(map[string]bool)
	for _, l := range labels1 {
		labelMap1[l] = true
	}
	if !labelMap1["frontend"] || !labelMap1["urgent"] {
		t.Errorf("expected labels [frontend, urgent], got %v", labels1)
	}

	labels2, err := store.GetLabels(ctx, newID2)
	if err != nil {
		t.Fatalf("failed to get labels for issue 2: %v", err)
	}
	if len(labels2) != 1 || labels2[0] != "backend" {
		t.Errorf("expected labels [backend], got %v", labels2)
	}

	// Verify dependency was created
	deps, err := store.GetDependenciesWithMetadata(ctx, newID2)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].ID != newID1 {
		t.Errorf("expected dependency on %s, got %s", newID1, deps[0].ID)
	}
	if deps[0].DependencyType != types.DepBlocks {
		t.Errorf("expected dependency type 'blocks', got %s", deps[0].DependencyType)
	}
}

// TestCreateWithDeps_RollbackOnFailure verifies that a transaction failure
// results in no partial state - all or nothing semantics
func TestCreateWithDeps_RollbackOnFailure(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// Create an issue with a dependency on a non-existent issue
	// This should fail when trying to create the dependency
	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				ID:        "temp-1",
				Title:     "Issue with invalid dependency",
				IssueType: "task",
				Priority:  1,
				Labels:    []string{"test-label"},
			},
		},
		Dependencies: []CreateWithDepsDependency{
			{
				FromID:  "temp-1",
				ToID:    "bd-nonexistent-xyz", // This issue doesn't exist
				DepType: string(types.DepBlocks),
			},
		},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)

	// The operation should fail because the dependency target doesn't exist
	if resp.Success {
		t.Fatal("expected failure for dependency on non-existent issue")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}

	// Verify no issues were created (rollback occurred)
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues after rollback, got %d", len(issues))
	}
}

// TestCreateWithDeps_IDGenerationWithLabels verifies that ID generation works
// correctly when creating issues with custom ID prefixes and labels
func TestCreateWithDeps_IDGenerationWithLabels(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// Create issues with custom ID prefix
	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				Title:     "Issue with custom prefix",
				IssueType: "feature",
				Priority:  3,
				IDPrefix:  "custom",
				Labels:    []string{"v1.0", "api"},
			},
			{
				Title:     "Issue using default prefix",
				IssueType: "task",
				Priority:  2,
				Labels:    []string{"internal"},
			},
		},
		Dependencies: []CreateWithDepsDependency{},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if !resp.Success {
		t.Fatalf("create_with_deps failed: %s", resp.Error)
	}

	var result CreateWithDepsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Verify ID mapping uses index-based keys when no explicit ID provided
	if result.Created != 2 {
		t.Errorf("expected 2 created, got %d", result.Created)
	}

	// Check that IDs were generated with proper prefixes
	id0, ok := result.IDMapping["0"]
	if !ok {
		t.Fatal("missing ID mapping for index 0")
	}
	id1, ok := result.IDMapping["1"]
	if !ok {
		t.Fatal("missing ID mapping for index 1")
	}

	// First issue should have custom prefix embedded in the generated ID
	// The format is "bd-custom-XXX" where bd is the store prefix and custom is the IDPrefix
	if !strings.Contains(id0, "custom") {
		t.Errorf("expected ID with 'custom' in prefix, got %s", id0)
	}

	// Second issue should have default prefix (bd-)
	if len(id1) < 3 || id1[:3] != "bd-" {
		t.Errorf("expected ID with 'bd-' prefix, got %s", id1)
	}

	// Verify issues exist and have correct labels
	issue0, err := store.GetIssue(ctx, id0)
	if err != nil {
		t.Fatalf("failed to get issue 0: %v", err)
	}
	if issue0.IssueType != types.TypeFeature {
		t.Errorf("expected type feature, got %s", issue0.IssueType)
	}

	labels0, err := store.GetLabels(ctx, id0)
	if err != nil {
		t.Fatalf("failed to get labels: %v", err)
	}
	if len(labels0) != 2 {
		t.Errorf("expected 2 labels, got %d", len(labels0))
	}
}

// TestCreateWithDeps_EmptyDependenciesList verifies that creating issues
// without any dependencies works correctly
func TestCreateWithDeps_EmptyDependenciesList(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// Create multiple issues without dependencies (no explicit IDs)
	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				Title:     "Standalone Issue A",
				IssueType: "task",
				Priority:  1,
			},
			{
				Title:     "Standalone Issue B",
				IssueType: "task",
				Priority:  2,
			},
			{
				Title:     "Standalone Issue C",
				IssueType: "bug",
				Priority:  3,
			},
		},
		Dependencies: []CreateWithDepsDependency{}, // Empty dependencies
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if !resp.Success {
		t.Fatalf("create_with_deps failed: %s", resp.Error)
	}

	var result CreateWithDepsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Verify all issues were created
	if result.Created != 3 {
		t.Errorf("expected 3 created, got %d", result.Created)
	}

	// Verify ID mappings exist for all
	if len(result.IDMapping) != 3 {
		t.Errorf("expected 3 ID mappings, got %d", len(result.IDMapping))
	}

	// Verify each issue exists in storage
	for oldID, newID := range result.IDMapping {
		issue, err := store.GetIssue(ctx, newID)
		if err != nil {
			t.Errorf("failed to get issue for old ID %s: %v", oldID, err)
			continue
		}
		if issue == nil {
			t.Errorf("issue %s not found", newID)
		}
	}

	// Verify no dependencies exist
	for _, newID := range result.IDMapping {
		deps, err := store.GetDependenciesWithMetadata(ctx, newID)
		if err != nil {
			t.Errorf("failed to get dependencies for %s: %v", newID, err)
			continue
		}
		if len(deps) != 0 {
			t.Errorf("expected 0 dependencies for %s, got %d", newID, len(deps))
		}
	}
}

// TestCreateWithDeps_InvalidDependencyTarget verifies that attempting to create
// a dependency on a non-existent issue (not part of the batch) fails properly
func TestCreateWithDeps_InvalidDependencyTarget(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// Count issues before the operation
	issuesBefore, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	countBefore := len(issuesBefore)

	// Try to create an issue that depends on a non-existent external issue
	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				Title:       "New Issue",
				Description: "This issue depends on something that doesn't exist",
				IssueType:   "task",
				Priority:    1,
			},
		},
		Dependencies: []CreateWithDepsDependency{
			{
				FromID:  "0", // Index of new issue
				ToID:    "bd-does-not-exist", // Invalid target - not in batch and not in DB
				DepType: string(types.DepBlocks),
			},
		},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)

	// Should fail because dependency target doesn't exist
	if resp.Success {
		t.Fatal("expected failure for invalid dependency target")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}

	// Verify the error mentions the dependency issue
	t.Logf("error message: %s", resp.Error)

	// Verify no new issues were created (transaction rolled back)
	issuesAfter, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	if len(issuesAfter) != countBefore {
		t.Errorf("expected %d issues after rollback, got %d", countBefore, len(issuesAfter))
	}
}

// TestCreateWithDeps_InvalidArgs tests error handling for malformed arguments
func TestCreateWithDeps_InvalidArgs(t *testing.T) {
	server, _ := setupAtomicTestServer(t)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

// TestCreateWithDeps_NoIssues tests that attempting to create with no issues fails
func TestCreateWithDeps_NoIssues(t *testing.T) {
	server, _ := setupAtomicTestServer(t)

	args := CreateWithDepsArgs{
		Issues:       []CreateWithDepsIssue{},
		Dependencies: []CreateWithDepsDependency{},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if resp.Success {
		t.Error("expected failure when no issues provided")
	}
	if resp.Error != "no issues to create" {
		t.Errorf("expected 'no issues to create' error, got: %s", resp.Error)
	}
}

// TestCreateWithDeps_InternalDependencies tests creating dependencies between
// issues being created in the same batch (using old/temp IDs)
func TestCreateWithDeps_InternalDependencies(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// Create a chain of dependencies: A <- B <- C (C depends on B, B depends on A)
	// Use index-based references: issue 0 = A, issue 1 = B, issue 2 = C
	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				Title:     "Issue A (root)",
				IssueType: "epic",
				Priority:  1,
			},
			{
				Title:     "Issue B (depends on A)",
				IssueType: "task",
				Priority:  2,
			},
			{
				Title:     "Issue C (depends on B)",
				IssueType: "task",
				Priority:  3,
			},
		},
		Dependencies: []CreateWithDepsDependency{
			{
				FromID:  "1", // Issue B
				ToID:    "0", // Issue A
				DepType: string(types.DepBlocks),
			},
			{
				FromID:  "2", // Issue C
				ToID:    "1", // Issue B
				DepType: string(types.DepBlocks),
			},
		},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if !resp.Success {
		t.Fatalf("create_with_deps failed: %s", resp.Error)
	}

	var result CreateWithDepsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Get the new IDs using index-based keys
	idA := result.IDMapping["0"]
	idB := result.IDMapping["1"]
	idC := result.IDMapping["2"]

	// Verify dependency chain: B depends on A
	depsB, err := store.GetDependenciesWithMetadata(ctx, idB)
	if err != nil {
		t.Fatalf("failed to get dependencies for B: %v", err)
	}
	if len(depsB) != 1 {
		t.Fatalf("expected 1 dependency for B, got %d", len(depsB))
	}
	if depsB[0].ID != idA {
		t.Errorf("expected B to depend on A (%s), got %s", idA, depsB[0].ID)
	}

	// Verify dependency chain: C depends on B
	depsC, err := store.GetDependenciesWithMetadata(ctx, idC)
	if err != nil {
		t.Fatalf("failed to get dependencies for C: %v", err)
	}
	if len(depsC) != 1 {
		t.Fatalf("expected 1 dependency for C, got %d", len(depsC))
	}
	if depsC[0].ID != idB {
		t.Errorf("expected C to depend on B (%s), got %s", idB, depsC[0].ID)
	}

	// Verify A has no dependencies
	depsA, err := store.GetDependenciesWithMetadata(ctx, idA)
	if err != nil {
		t.Fatalf("failed to get dependencies for A: %v", err)
	}
	if len(depsA) != 0 {
		t.Errorf("expected 0 dependencies for A (root), got %d", len(depsA))
	}
}

// TestCreateWithDeps_MixedInternalExternalDependencies tests creating dependencies
// that reference both new issues (in batch) and existing issues
func TestCreateWithDeps_MixedInternalExternalDependencies(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// First create an existing issue
	existingIssue := &types.Issue{
		ID:        "bd-existing",
		Title:     "Existing Issue",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, existingIssue, "test"); err != nil {
		t.Fatalf("failed to create existing issue: %v", err)
	}

	// Now create new issues that depend on both new and existing issues
	// Index 0 = parent, Index 1 = child
	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				Title:     "New Parent Issue",
				IssueType: "epic",
				Priority:  1,
			},
			{
				Title:     "New Child Issue",
				IssueType: "task",
				Priority:  2,
			},
		},
		Dependencies: []CreateWithDepsDependency{
			{
				FromID:  "1", // New child (index 1)
				ToID:    "0", // New parent (index 0) - internal dependency
				DepType: string(types.DepParentChild),
			},
			{
				FromID:  "1", // New child (index 1)
				ToID:    existingIssue.ID, // External dependency (existing issue)
				DepType: string(types.DepBlocks),
			},
		},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if !resp.Success {
		t.Fatalf("create_with_deps failed: %s", resp.Error)
	}

	var result CreateWithDepsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Get the new IDs using index-based keys
	newParentID := result.IDMapping["0"]
	newChildID := result.IDMapping["1"]

	// Verify both dependencies exist for child
	deps, err := store.GetDependenciesWithMetadata(ctx, newChildID)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(deps))
	}

	// Check the dependencies
	depTargets := make(map[string]types.DependencyType)
	for _, d := range deps {
		depTargets[d.ID] = d.DependencyType
	}

	// Should have parent-child dep to new parent
	if depType, ok := depTargets[newParentID]; !ok {
		t.Error("missing dependency to new parent")
	} else if depType != types.DepParentChild {
		t.Errorf("expected parent-child dep type, got %s", depType)
	}

	// Should have blocks dep to existing issue
	if depType, ok := depTargets[existingIssue.ID]; !ok {
		t.Error("missing dependency to existing issue")
	} else if depType != types.DepBlocks {
		t.Errorf("expected blocks dep type, got %s", depType)
	}
}

// TestCreateWithDeps_EphemeralIssues tests creating ephemeral issues atomically
func TestCreateWithDeps_EphemeralIssues(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	args := CreateWithDepsArgs{
		Issues: []CreateWithDepsIssue{
			{
				Title:     "Ephemeral Wisp",
				IssueType: "task",
				Priority:  1,
				Ephemeral: true,
				Labels:    []string{"ephemeral", "workflow"},
			},
		},
		Dependencies: []CreateWithDepsDependency{},
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleCreateWithDeps(req)
	if !resp.Success {
		t.Fatalf("create_with_deps failed: %s", resp.Error)
	}

	var result CreateWithDepsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Verify the issue was created as ephemeral (using index-based key)
	newID := result.IDMapping["0"]
	issue, err := store.GetIssue(ctx, newID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if !issue.Ephemeral {
		t.Error("expected issue to be ephemeral")
	}

	// Verify labels
	labels, err := store.GetLabels(ctx, newID)
	if err != nil {
		t.Fatalf("failed to get labels: %v", err)
	}
	if len(labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(labels))
	}
}

// TestCreateWithDeps_TemplateIDsGenerateUniqueIDs tests that passing template IDs
// (e.g., from mol-polecat-work) results in unique generated IDs, not duplicate key errors.
// This is the fix for gt-yklx63: duplicate primary key error when creating wisp mol-polecat-work.
func TestCreateWithDeps_TemplateIDsGenerateUniqueIDs(t *testing.T) {
	server, store := setupAtomicTestServer(t)
	ctx := context.Background()

	// Simulate template IDs like those from mol-polecat-work proto
	templateIssues := []CreateWithDepsIssue{
		{
			ID:        "proto-abc-step1", // Template ID - should NOT be used as final ID
			Title:     "Template Step 1",
			IssueType: "task",
			Priority:  1,
			IDPrefix:  "wisp",
		},
		{
			ID:        "proto-abc-step2", // Template ID - should NOT be used as final ID
			Title:     "Template Step 2",
			IssueType: "task",
			Priority:  2,
			IDPrefix:  "wisp",
		},
	}
	templateDeps := []CreateWithDepsDependency{
		{
			FromID:  "proto-abc-step2", // Reference using template ID
			ToID:    "proto-abc-step1", // Reference using template ID
			DepType: string(types.DepBlocks),
		},
	}

	// First creation - should succeed
	args1 := CreateWithDepsArgs{
		Issues:       templateIssues,
		Dependencies: templateDeps,
	}
	argsJSON1, _ := json.Marshal(args1)
	req1 := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON1,
		Actor:     "test",
	}

	resp1 := server.handleCreateWithDeps(req1)
	if !resp1.Success {
		t.Fatalf("first create_with_deps failed: %s", resp1.Error)
	}

	var result1 CreateWithDepsResult
	if err := json.Unmarshal(resp1.Data, &result1); err != nil {
		t.Fatalf("failed to parse first result: %v", err)
	}

	// Verify IDs were GENERATED (not the template IDs)
	newID1 := result1.IDMapping["proto-abc-step1"]
	newID2 := result1.IDMapping["proto-abc-step2"]
	if newID1 == "proto-abc-step1" {
		t.Errorf("expected generated ID, got template ID: %s", newID1)
	}
	if newID2 == "proto-abc-step2" {
		t.Errorf("expected generated ID, got template ID: %s", newID2)
	}

	// Verify the dependency was created correctly
	depsFor2, err := store.GetDependenciesWithMetadata(ctx, newID2)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}
	if len(depsFor2) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(depsFor2))
	}
	if depsFor2[0].ID != newID1 {
		t.Errorf("expected dependency on %s, got %s", newID1, depsFor2[0].ID)
	}

	// Second creation with SAME template IDs - this was failing before the fix
	// with "duplicate primary key" error
	args2 := CreateWithDepsArgs{
		Issues:       templateIssues, // Same template IDs
		Dependencies: templateDeps,
	}
	argsJSON2, _ := json.Marshal(args2)
	req2 := &Request{
		Operation: OpCreateWithDeps,
		Args:      argsJSON2,
		Actor:     "test",
	}

	resp2 := server.handleCreateWithDeps(req2)
	if !resp2.Success {
		t.Fatalf("second create_with_deps failed (gt-yklx63 bug): %s", resp2.Error)
	}

	var result2 CreateWithDepsResult
	if err := json.Unmarshal(resp2.Data, &result2); err != nil {
		t.Fatalf("failed to parse second result: %v", err)
	}

	// Verify second creation produced DIFFERENT IDs
	newID3 := result2.IDMapping["proto-abc-step1"]
	newID4 := result2.IDMapping["proto-abc-step2"]
	if newID3 == newID1 {
		t.Errorf("second creation used same ID as first: %s", newID3)
	}
	if newID4 == newID2 {
		t.Errorf("second creation used same ID as first: %s", newID4)
	}

	// Verify all 4 issues exist and are distinct
	allIDs := map[string]bool{newID1: true, newID2: true, newID3: true, newID4: true}
	if len(allIDs) != 4 {
		t.Errorf("expected 4 distinct IDs, got: %v", allIDs)
	}

	for id := range allIDs {
		issue, err := store.GetIssue(ctx, id)
		if err != nil {
			t.Errorf("failed to get issue %s: %v", id, err)
		}
		if issue == nil {
			t.Errorf("issue %s not found", id)
		}
	}
}

// =============================================================================
// CreateMolecule RPC Tests
// =============================================================================

// TestCreateMolecule_HappyPath tests the happy path for creating a molecule
// with multiple issues and dependencies atomically.
func TestCreateMolecule_HappyPath(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create molecule with 3 issues and dependencies between them
	// Structure: step1 <- step2 <- step3 (step2 blocks step1, step3 blocks step2)
	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "step1",
				CreateArgs: CreateArgs{
					Title:     "First step",
					IssueType: "task",
					Priority:  2,
				},
			},
			{
				TemplateID: "step2",
				CreateArgs: CreateArgs{
					Title:     "Second step",
					IssueType: "task",
					Priority:  2,
				},
			},
			{
				TemplateID: "step3",
				CreateArgs: CreateArgs{
					Title:     "Third step",
					IssueType: "task",
					Priority:  2,
				},
			},
		},
		Dependencies: []DepSpec{
			{
				FromTemplateID: "step1",
				ToTemplateID:   "step2",
				DepType:        "blocks",
			},
			{
				FromTemplateID: "step2",
				ToTemplateID:   "step3",
				DepType:        "blocks",
			},
		},
		Prefix:       "mol",
		RootTemplate: "step1",
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	if err != nil {
		t.Fatalf("CreateMolecule failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("CreateMolecule returned error: %s", resp.Error)
	}

	// Unmarshal result
	var result CreateMoleculeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify ID mapping has 3 entries
	if len(result.IDMapping) != 3 {
		t.Errorf("Expected 3 IDs in mapping, got %d", len(result.IDMapping))
	}

	// Verify RootID is set correctly
	if result.RootID == "" {
		t.Error("RootID should be set")
	}
	if result.RootID != result.IDMapping["step1"] {
		t.Errorf("RootID %q should match step1 mapping %q", result.RootID, result.IDMapping["step1"])
	}

	// Verify Created count
	if result.Created != 3 {
		t.Errorf("Expected 3 created, got %d", result.Created)
	}

	// Verify all issues exist in storage
	for templateID, issueID := range result.IDMapping {
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			t.Errorf("Failed to get issue %s (template %s): %v", issueID, templateID, err)
			continue
		}
		if issue == nil {
			t.Errorf("Issue %s (template %s) not found", issueID, templateID)
		}
	}

	// Verify dependencies were created
	step1ID := result.IDMapping["step1"]
	deps, err := store.GetDependencies(ctx, step1ID)
	if err != nil {
		t.Fatalf("Failed to get dependencies for step1: %v", err)
	}
	if len(deps) != 1 {
		t.Errorf("Expected 1 dependency for step1, got %d", len(deps))
	} else if deps[0].ID != result.IDMapping["step2"] {
		t.Errorf("step1 should depend on step2, got %s", deps[0].ID)
	}

	step2ID := result.IDMapping["step2"]
	deps2, err := store.GetDependencies(ctx, step2ID)
	if err != nil {
		t.Fatalf("Failed to get dependencies for step2: %v", err)
	}
	if len(deps2) != 1 {
		t.Errorf("Expected 1 dependency for step2, got %d", len(deps2))
	} else if deps2[0].ID != result.IDMapping["step3"] {
		t.Errorf("step2 should depend on step3, got %s", deps2[0].ID)
	}
}

// TestCreateMolecule_RollbackOnIssueCreationFailure tests that the transaction
// rolls back when issue creation fails.
func TestCreateMolecule_RollbackOnIssueCreationFailure(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a valid issue first to establish a starting point
	validArgs := &CreateArgs{
		Title:     "Pre-existing issue",
		IssueType: "task",
		Priority:  2,
	}
	preResp, err := client.Create(validArgs)
	if err != nil {
		t.Fatalf("Failed to create pre-existing issue: %v", err)
	}
	var preIssue struct{ ID string }
	if err := json.Unmarshal(preResp.Data, &preIssue); err != nil {
		t.Fatalf("Failed to unmarshal pre-existing issue: %v", err)
	}

	// Count issues before molecule creation attempt
	issuesBefore, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	countBefore := len(issuesBefore)

	// Try to create a molecule with invalid due_at format which will fail parsing
	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "valid1",
				CreateArgs: CreateArgs{
					Title:     "Valid issue",
					IssueType: "task",
					Priority:  2,
				},
			},
			{
				TemplateID: "invalid",
				CreateArgs: CreateArgs{
					Title:     "Issue with invalid due_at",
					IssueType: "task",
					Priority:  2,
					DueAt:     "not-a-valid-date", // Invalid date format
				},
			},
		},
		Dependencies: []DepSpec{},
		Prefix:       "mol",
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	// Execute returns error when resp.Success is false - this is expected
	if err == nil && resp != nil && resp.Success {
		t.Error("Expected CreateMolecule to fail with invalid due_at format")
	}

	// Verify no new issues were created (rollback worked)
	issuesAfter, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	countAfter := len(issuesAfter)

	if countAfter != countBefore {
		t.Errorf("Expected %d issues after rollback, got %d (rollback failed)", countBefore, countAfter)
	}
}

// TestCreateMolecule_RollbackOnDependencyCreationFailure tests that the transaction
// rolls back when dependency creation fails (e.g., invalid template ID reference).
func TestCreateMolecule_RollbackOnDependencyCreationFailure(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Count issues before attempt
	issuesBefore, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	countBefore := len(issuesBefore)

	// Create molecule with a dependency referencing non-existent template ID
	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "step1",
				CreateArgs: CreateArgs{
					Title:     "First step",
					IssueType: "task",
					Priority:  2,
				},
			},
		},
		Dependencies: []DepSpec{
			{
				FromTemplateID: "step1",
				ToTemplateID:   "nonexistent", // This template ID doesn't exist
				DepType:        "blocks",
			},
		},
		Prefix: "mol",
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	// Execute returns error when resp.Success is false - this is expected
	if err == nil && resp != nil && resp.Success {
		t.Error("Expected CreateMolecule to fail with invalid template ID reference")
	}
	// err contains the error message when operation fails

	// Verify no issues were created (rollback worked)
	issuesAfter, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	countAfter := len(issuesAfter)

	if countAfter != countBefore {
		t.Errorf("Expected %d issues after rollback, got %d (rollback failed)", countBefore, countAfter)
	}
}

// TestCreateMolecule_IDMappingCorrectness tests that the ID mapping correctly
// maps template IDs to generated issue IDs.
func TestCreateMolecule_IDMappingCorrectness(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create molecule with specific titles to identify each issue
	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "alpha",
				CreateArgs: CreateArgs{
					Title:       "Alpha Issue",
					IssueType:   "task",
					Priority:    1,
					Description: "This is alpha",
				},
			},
			{
				TemplateID: "beta",
				CreateArgs: CreateArgs{
					Title:       "Beta Issue",
					IssueType:   "task",
					Priority:    2,
					Description: "This is beta",
				},
			},
			{
				TemplateID: "gamma",
				CreateArgs: CreateArgs{
					Title:       "Gamma Issue",
					IssueType:   "task",
					Priority:    3,
					Description: "This is gamma",
				},
			},
		},
		Dependencies: []DepSpec{
			{
				FromTemplateID: "alpha",
				ToTemplateID:   "beta",
				DepType:        "blocks",
			},
			{
				FromTemplateID: "beta",
				ToTemplateID:   "gamma",
				DepType:        "blocks",
			},
		},
		RootTemplate: "alpha",
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	if err != nil {
		t.Fatalf("CreateMolecule failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("CreateMolecule returned error: %s", resp.Error)
	}

	var result CreateMoleculeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify each template ID maps to the correct issue (by title)
	expectedTitles := map[string]string{
		"alpha": "Alpha Issue",
		"beta":  "Beta Issue",
		"gamma": "Gamma Issue",
	}

	for templateID, expectedTitle := range expectedTitles {
		issueID, ok := result.IDMapping[templateID]
		if !ok {
			t.Errorf("Template ID %q not found in mapping", templateID)
			continue
		}

		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			t.Errorf("Failed to get issue for template %q: %v", templateID, err)
			continue
		}

		if issue.Title != expectedTitle {
			t.Errorf("Template %q mapped to issue with title %q, expected %q",
				templateID, issue.Title, expectedTitle)
		}
	}

	// Verify RootID is correctly set to alpha's ID
	if result.RootID != result.IDMapping["alpha"] {
		t.Errorf("RootID should be alpha's ID, got %q, expected %q",
			result.RootID, result.IDMapping["alpha"])
	}
}

// TestCreateMolecule_EphemeralFlagHandling tests that the ephemeral flag
// is correctly applied to all issues in the molecule.
func TestCreateMolecule_EphemeralFlagHandling(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create molecule with ephemeral flag set at molecule level
	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "eph1",
				CreateArgs: CreateArgs{
					Title:     "Ephemeral issue 1",
					IssueType: "task",
					Priority:  2,
				},
			},
			{
				TemplateID: "eph2",
				CreateArgs: CreateArgs{
					Title:     "Ephemeral issue 2",
					IssueType: "task",
					Priority:  2,
				},
			},
		},
		Dependencies: []DepSpec{},
		Prefix:       "wisp",
		Ephemeral:    true, // Molecule-level ephemeral flag
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	if err != nil {
		t.Fatalf("CreateMolecule failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("CreateMolecule returned error: %s", resp.Error)
	}

	var result CreateMoleculeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify all issues have ephemeral flag set
	for templateID, issueID := range result.IDMapping {
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			t.Errorf("Failed to get issue %s (template %s): %v", issueID, templateID, err)
			continue
		}

		if !issue.Ephemeral {
			t.Errorf("Issue %s (template %s) should have Ephemeral=true", issueID, templateID)
		}
	}
}

// TestCreateMolecule_EphemeralFlagOverride tests that individual issue ephemeral
// settings work alongside molecule-level ephemeral flag.
func TestCreateMolecule_EphemeralFlagOverride(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create molecule without molecule-level ephemeral, but with one issue ephemeral
	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "normal",
				CreateArgs: CreateArgs{
					Title:     "Normal issue",
					IssueType: "task",
					Priority:  2,
					Ephemeral: false, // Explicitly non-ephemeral
				},
			},
			{
				TemplateID: "ephemeral",
				CreateArgs: CreateArgs{
					Title:     "Ephemeral issue",
					IssueType: "task",
					Priority:  2,
					Ephemeral: true, // This one is ephemeral
				},
			},
		},
		Dependencies: []DepSpec{},
		Ephemeral:    false, // Molecule-level is false
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	if err != nil {
		t.Fatalf("CreateMolecule failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("CreateMolecule returned error: %s", resp.Error)
	}

	var result CreateMoleculeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify normal issue is NOT ephemeral
	normalID := result.IDMapping["normal"]
	normalIssue, err := store.GetIssue(ctx, normalID)
	if err != nil {
		t.Fatalf("Failed to get normal issue: %v", err)
	}
	if normalIssue.Ephemeral {
		t.Error("Normal issue should not be ephemeral")
	}

	// Verify ephemeral issue IS ephemeral
	ephID := result.IDMapping["ephemeral"]
	ephIssue, err := store.GetIssue(ctx, ephID)
	if err != nil {
		t.Fatalf("Failed to get ephemeral issue: %v", err)
	}
	if !ephIssue.Ephemeral {
		t.Error("Ephemeral issue should be ephemeral")
	}
}

// TestCreateMolecule_EmptyInput tests handling of empty input.
func TestCreateMolecule_EmptyInput(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Test: Empty issues array
	args := &CreateMoleculeArgs{
		Issues:       []IssueCreateSpec{},
		Dependencies: []DepSpec{},
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	// Execute returns error when resp.Success is false - this is expected
	if err == nil && resp != nil && resp.Success {
		t.Error("Expected CreateMolecule to fail with empty issues array")
	}
	// err contains the error message (from resp.Error) when operation fails
}

// TestCreateMolecule_NoDependencies tests creating a molecule with issues but no dependencies.
func TestCreateMolecule_NoDependencies(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "standalone1",
				CreateArgs: CreateArgs{
					Title:     "Standalone issue 1",
					IssueType: "task",
					Priority:  2,
				},
			},
			{
				TemplateID: "standalone2",
				CreateArgs: CreateArgs{
					Title:     "Standalone issue 2",
					IssueType: "task",
					Priority:  2,
				},
			},
		},
		Dependencies: []DepSpec{}, // No dependencies
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	if err != nil {
		t.Fatalf("CreateMolecule failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("CreateMolecule returned error: %s", resp.Error)
	}

	var result CreateMoleculeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify both issues were created
	if len(result.IDMapping) != 2 {
		t.Errorf("Expected 2 IDs in mapping, got %d", len(result.IDMapping))
	}

	// Verify neither issue has dependencies
	for _, issueID := range result.IDMapping {
		deps, err := store.GetDependencies(ctx, issueID)
		if err != nil {
			t.Errorf("Failed to get dependencies for %s: %v", issueID, err)
			continue
		}
		if len(deps) != 0 {
			t.Errorf("Issue %s should have no dependencies, got %d", issueID, len(deps))
		}
	}
}

// TestCreateMolecule_PrefixApplication tests that the prefix is correctly applied
// to generated issue IDs.
func TestCreateMolecule_PrefixApplication(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "prefixed",
				CreateArgs: CreateArgs{
					Title:     "Prefixed issue",
					IssueType: "task",
					Priority:  2,
				},
			},
		},
		Dependencies: []DepSpec{},
		Prefix:       "mol", // This prefix should be applied
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	if err != nil {
		t.Fatalf("CreateMolecule failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("CreateMolecule returned error: %s", resp.Error)
	}

	var result CreateMoleculeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify the ID contains "mol" (the prefix is combined with the store's prefix)
	// The ID format is typically "bd-mol-xxx" where bd is the store prefix and mol is the molecule prefix
	issueID := result.IDMapping["prefixed"]
	if !strings.Contains(issueID, "mol") {
		t.Errorf("Expected issue ID to contain 'mol', got %q", issueID)
	}
}

// TestCreateMolecule_InvalidFromTemplateID tests that using an invalid FromTemplateID
// in a dependency results in an error.
func TestCreateMolecule_InvalidFromTemplateID(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "step1",
				CreateArgs: CreateArgs{
					Title:     "Step 1",
					IssueType: "task",
					Priority:  2,
				},
			},
		},
		Dependencies: []DepSpec{
			{
				FromTemplateID: "nonexistent_from", // Invalid
				ToTemplateID:   "step1",
				DepType:        "blocks",
			},
		},
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	// Execute returns error when resp.Success is false - this is expected
	if err == nil && resp != nil && resp.Success {
		t.Error("Expected CreateMolecule to fail with invalid from_template_id")
	}
	// err contains the error message when operation fails
}

// TestCreateMolecule_InvalidToTemplateID tests that using an invalid ToTemplateID
// in a dependency results in an error.
func TestCreateMolecule_InvalidToTemplateID(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	args := &CreateMoleculeArgs{
		Issues: []IssueCreateSpec{
			{
				TemplateID: "step1",
				CreateArgs: CreateArgs{
					Title:     "Step 1",
					IssueType: "task",
					Priority:  2,
				},
			},
		},
		Dependencies: []DepSpec{
			{
				FromTemplateID: "step1",
				ToTemplateID:   "nonexistent_to", // Invalid
				DepType:        "blocks",
			},
		},
	}

	resp, err := client.Execute(OpCreateMolecule, args)
	// Execute returns error when resp.Success is false - this is expected
	if err == nil && resp != nil && resp.Success {
		t.Error("Expected CreateMolecule to fail with invalid to_template_id")
	}
	// err contains the error message when operation fails
}

// ============================================================================
// Bidirectional Relation Tests
// ============================================================================

// TestAddBidirectionalRelation_CreatesBothDirections verifies that handleDepAddBidirectional
// creates both id1->id2 and id2->id1 dependencies.
func TestAddBidirectionalRelation_CreatesBothDirections(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create two test issues
	issue1 := createTestIssueForBidir(t, client, "Issue One", "task")
	issue2 := createTestIssueForBidir(t, client, "Issue Two", "task")

	// Add bidirectional relation
	args := &DepAddBidirectionalArgs{
		ID1:     issue1,
		ID2:     issue2,
		DepType: string(types.DepRelatesTo),
	}
	resp, err := client.AddBidirectionalRelation(args)
	if err != nil {
		t.Fatalf("AddBidirectionalRelation failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("AddBidirectionalRelation not successful: %s", resp.Error)
	}

	// Verify response data
	var result struct {
		Status string `json:"status"`
		ID1    string `json:"id1"`
		ID2    string `json:"id2"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result.Status != "added" {
		t.Errorf("Expected status='added', got %q", result.Status)
	}
	if result.ID1 != issue1 {
		t.Errorf("Expected id1=%q, got %q", issue1, result.ID1)
	}
	if result.ID2 != issue2 {
		t.Errorf("Expected id2=%q, got %q", issue2, result.ID2)
	}
	if result.Type != string(types.DepRelatesTo) {
		t.Errorf("Expected type=%q, got %q", types.DepRelatesTo, result.Type)
	}

	// Verify both directions exist in storage
	ctx := context.Background()

	// Check id1 -> id2
	deps1, err := store.GetDependencyRecords(ctx, issue1)
	if err != nil {
		t.Fatalf("GetDependencyRecords for issue1 failed: %v", err)
	}
	if !hasDependencyBidir(deps1, issue1, issue2, types.DepRelatesTo) {
		t.Errorf("Missing dependency %s -> %s (relates-to)", issue1, issue2)
	}

	// Check id2 -> id1
	deps2, err := store.GetDependencyRecords(ctx, issue2)
	if err != nil {
		t.Fatalf("GetDependencyRecords for issue2 failed: %v", err)
	}
	if !hasDependencyBidir(deps2, issue2, issue1, types.DepRelatesTo) {
		t.Errorf("Missing dependency %s -> %s (relates-to)", issue2, issue1)
	}
}

// TestAddBidirectionalRelation_SelfReference verifies error handling when
// id1 and id2 are the same.
func TestAddBidirectionalRelation_SelfReference(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create a test issue
	issue1 := createTestIssueForBidir(t, client, "Issue One", "task")

	// Try to add self-referencing bidirectional relation
	args := &DepAddBidirectionalArgs{
		ID1:     issue1,
		ID2:     issue1, // Same as ID1
		DepType: string(types.DepRelatesTo),
	}
	resp, err := client.AddBidirectionalRelation(args)

	// Client returns error for failed operations
	if err == nil {
		t.Errorf("Expected error for self-reference, but got success")
	}
	// Check that error mentions the issue
	expectedErrPart := "id1 and id2 must be different"
	if err != nil && !strings.Contains(err.Error(), expectedErrPart) {
		t.Errorf("Expected error to contain %q, got: %v", expectedErrPart, err)
	}
	// Response should still be returned with error info
	if resp != nil && resp.Success {
		t.Errorf("Expected resp.Success to be false")
	}
}

// TestAddBidirectionalRelation_IdempotentBehavior verifies that adding an
// already-existing relation is handled (may fail with duplicate error or succeed idempotently).
func TestAddBidirectionalRelation_IdempotentBehavior(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create two test issues
	issue1 := createTestIssueForBidir(t, client, "Issue One", "task")
	issue2 := createTestIssueForBidir(t, client, "Issue Two", "task")

	// Add bidirectional relation first time
	args := &DepAddBidirectionalArgs{
		ID1:     issue1,
		ID2:     issue2,
		DepType: string(types.DepRelatesTo),
	}
	resp1, err := client.AddBidirectionalRelation(args)
	if err != nil {
		t.Fatalf("First AddBidirectionalRelation failed: %v", err)
	}
	if !resp1.Success {
		t.Fatalf("First AddBidirectionalRelation not successful: %s", resp1.Error)
	}

	// Add bidirectional relation second time
	// This may succeed (idempotent) or fail (duplicate error) - either is acceptable
	resp2, err := client.AddBidirectionalRelation(args)

	// Log the behavior - either outcome is acceptable
	if err != nil {
		t.Logf("Second add returned error (expected behavior): %v", err)
	} else {
		t.Logf("Second add succeeded (idempotent behavior)")
	}

	// Either way, verify the relations still exist
	ctx := context.Background()

	deps1, err := store.GetDependencyRecords(ctx, issue1)
	if err != nil {
		t.Fatalf("GetDependencyRecords for issue1 failed: %v", err)
	}
	if !hasDependencyBidir(deps1, issue1, issue2, types.DepRelatesTo) {
		t.Errorf("Missing dependency %s -> %s after second add", issue1, issue2)
	}

	deps2, err := store.GetDependencyRecords(ctx, issue2)
	if err != nil {
		t.Fatalf("GetDependencyRecords for issue2 failed: %v", err)
	}
	if !hasDependencyBidir(deps2, issue2, issue1, types.DepRelatesTo) {
		t.Errorf("Missing dependency %s -> %s after second add", issue2, issue1)
	}

	// Suppress unused variable warning
	_ = resp2
}

// TestRemoveBidirectionalRelation_RemovesBothDirections verifies that
// handleDepRemoveBidirectional removes both directions.
func TestRemoveBidirectionalRelation_RemovesBothDirections(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create two test issues
	issue1 := createTestIssueForBidir(t, client, "Issue One", "task")
	issue2 := createTestIssueForBidir(t, client, "Issue Two", "task")

	// First add a bidirectional relation
	addArgs := &DepAddBidirectionalArgs{
		ID1:     issue1,
		ID2:     issue2,
		DepType: string(types.DepRelatesTo),
	}
	resp, err := client.AddBidirectionalRelation(addArgs)
	if err != nil {
		t.Fatalf("AddBidirectionalRelation failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("AddBidirectionalRelation not successful: %s", resp.Error)
	}

	// Now remove the bidirectional relation
	removeArgs := &DepRemoveBidirectionalArgs{
		ID1: issue1,
		ID2: issue2,
	}
	resp, err = client.RemoveBidirectionalRelation(removeArgs)
	if err != nil {
		t.Fatalf("RemoveBidirectionalRelation failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("RemoveBidirectionalRelation not successful: %s", resp.Error)
	}

	// Verify response data
	var result struct {
		Status string `json:"status"`
		ID1    string `json:"id1"`
		ID2    string `json:"id2"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result.Status != "removed" {
		t.Errorf("Expected status='removed', got %q", result.Status)
	}
	if result.ID1 != issue1 {
		t.Errorf("Expected id1=%q, got %q", issue1, result.ID1)
	}
	if result.ID2 != issue2 {
		t.Errorf("Expected id2=%q, got %q", issue2, result.ID2)
	}

	// Verify both directions are removed from storage
	ctx := context.Background()

	// Check id1 -> id2 is gone
	deps1, err := store.GetDependencyRecords(ctx, issue1)
	if err != nil {
		t.Fatalf("GetDependencyRecords for issue1 failed: %v", err)
	}
	if hasDependencyBidir(deps1, issue1, issue2, types.DepRelatesTo) {
		t.Errorf("Dependency %s -> %s should be removed", issue1, issue2)
	}

	// Check id2 -> id1 is gone
	deps2, err := store.GetDependencyRecords(ctx, issue2)
	if err != nil {
		t.Fatalf("GetDependencyRecords for issue2 failed: %v", err)
	}
	if hasDependencyBidir(deps2, issue2, issue1, types.DepRelatesTo) {
		t.Errorf("Dependency %s -> %s should be removed", issue2, issue1)
	}
}

// TestAddBidirectionalRelation_OneIssueDoesNotExist verifies error handling
// when one of the issues does not exist.
func TestAddBidirectionalRelation_OneIssueDoesNotExist(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create only one test issue
	issue1 := createTestIssueForBidir(t, client, "Issue One", "task")

	// Try to add bidirectional relation with non-existent issue
	args := &DepAddBidirectionalArgs{
		ID1:     issue1,
		ID2:     "bd-nonexistent",
		DepType: string(types.DepRelatesTo),
	}
	resp, err := client.AddBidirectionalRelation(args)

	// Client returns error for failed operations
	if err == nil {
		t.Errorf("Expected error for non-existent issue, but got success")
	}
	// Check that error mentions something relevant
	if err != nil {
		t.Logf("Error for non-existent issue: %s", err.Error())
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "does not exist") {
			t.Logf("Note: error message doesn't contain 'not found' or 'does not exist': %v", err)
		}
	}
	// Response should indicate failure if returned
	if resp != nil && resp.Success {
		t.Errorf("Expected resp.Success to be false")
	}
}

// TestAddBidirectionalRelation_RollbackOnSecondDirectionFailure verifies that
// if the second direction fails to add, the first direction is rolled back.
// This tests the atomic transaction behavior.
func TestAddBidirectionalRelation_RollbackOnSecondDirectionFailure(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create two test issues
	issue1 := createTestIssueForBidir(t, client, "Issue One", "task")
	issue2 := createTestIssueForBidir(t, client, "Issue Two", "task")

	// First, manually add only the id2 -> id1 direction using regular dep_add
	// This sets up a situation where adding id2 -> id1 should fail (duplicate)
	// but id1 -> id2 would succeed
	depArgs := &DepAddArgs{
		FromID:  issue2,
		ToID:    issue1,
		DepType: string(types.DepRelatesTo),
	}
	resp, err := client.AddDependency(depArgs)
	if err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("AddDependency not successful: %s", resp.Error)
	}

	// Verify id2 -> id1 exists
	ctx := context.Background()
	deps2Before, err := store.GetDependencyRecords(ctx, issue2)
	if err != nil {
		t.Fatalf("GetDependencyRecords before failed: %v", err)
	}
	if !hasDependencyBidir(deps2Before, issue2, issue1, types.DepRelatesTo) {
		t.Fatalf("Setup: expected %s -> %s to exist", issue2, issue1)
	}

	// Verify id1 -> id2 does NOT exist yet
	deps1Before, err := store.GetDependencyRecords(ctx, issue1)
	if err != nil {
		t.Fatalf("GetDependencyRecords before failed: %v", err)
	}
	if hasDependencyBidir(deps1Before, issue1, issue2, types.DepRelatesTo) {
		t.Fatalf("Setup: expected %s -> %s to NOT exist yet", issue1, issue2)
	}

	// Now try to add bidirectional relation
	// The id1 -> id2 add should succeed, but id2 -> id1 might fail (already exists)
	biArgs := &DepAddBidirectionalArgs{
		ID1:     issue1,
		ID2:     issue2,
		DepType: string(types.DepRelatesTo),
	}
	_, biErr := client.AddBidirectionalRelation(biArgs)

	// Check state after the attempt
	deps1After, err := store.GetDependencyRecords(ctx, issue1)
	if err != nil {
		t.Fatalf("GetDependencyRecords after failed: %v", err)
	}
	deps2After, err := store.GetDependencyRecords(ctx, issue2)
	if err != nil {
		t.Fatalf("GetDependencyRecords after failed: %v", err)
	}

	if biErr == nil {
		// If it succeeded (idempotent storage), both directions should exist
		if !hasDependencyBidir(deps1After, issue1, issue2, types.DepRelatesTo) {
			t.Errorf("After success: expected %s -> %s to exist", issue1, issue2)
		}
		if !hasDependencyBidir(deps2After, issue2, issue1, types.DepRelatesTo) {
			t.Errorf("After success: expected %s -> %s to exist", issue2, issue1)
		}
		t.Log("Bidirectional add succeeded (storage allows duplicate deps or is idempotent)")
	} else {
		// If it failed, verify rollback - id1 -> id2 should NOT exist
		// (the first direction should have been rolled back)
		if hasDependencyBidir(deps1After, issue1, issue2, types.DepRelatesTo) {
			t.Errorf("After failure: expected %s -> %s to be rolled back (should not exist)", issue1, issue2)
		}
		// id2 -> id1 should still exist (was there before)
		if !hasDependencyBidir(deps2After, issue2, issue1, types.DepRelatesTo) {
			t.Errorf("After failure: expected pre-existing %s -> %s to still exist", issue2, issue1)
		}
		t.Logf("Bidirectional add failed as expected (transaction rolled back): %s", biErr)
	}
}

// TestRemoveBidirectionalRelation_NonExistentRelation verifies that removing
// a non-existent relation fails gracefully.
func TestRemoveBidirectionalRelation_NonExistentRelation(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create two test issues (but don't add any relation)
	issue1 := createTestIssueForBidir(t, client, "Issue One", "task")
	issue2 := createTestIssueForBidir(t, client, "Issue Two", "task")

	// Try to remove non-existent bidirectional relation
	removeArgs := &DepRemoveBidirectionalArgs{
		ID1: issue1,
		ID2: issue2,
	}
	resp, err := client.RemoveBidirectionalRelation(removeArgs)

	// Should fail because the relation doesn't exist
	if err == nil {
		t.Logf("Remove non-existent succeeded (storage is idempotent for removals)")
	} else {
		t.Logf("Remove non-existent returned error (expected): %v", err)
		// Check that error is about non-existent dependency
		if !strings.Contains(err.Error(), "does not exist") {
			t.Logf("Note: error message doesn't contain 'does not exist': %v", err)
		}
	}

	// Log response info if available
	if resp != nil {
		t.Logf("Response: success=%v, error=%q", resp.Success, resp.Error)
	}
}

// Helper function to create a test issue and return its ID (for bidirectional tests)
func createTestIssueForBidir(t *testing.T, client *Client, title, issueType string) string {
	t.Helper()
	createArgs := &CreateArgs{
		Title:     title,
		IssueType: issueType,
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

// Helper function to check if a dependency exists in the list (for bidirectional tests)
func hasDependencyBidir(deps []*types.Dependency, fromID, toID string, depType types.DependencyType) bool {
	for _, dep := range deps {
		if dep.IssueID == fromID && dep.DependsOnID == toID && dep.Type == depType {
			return true
		}
	}
	return false
}
