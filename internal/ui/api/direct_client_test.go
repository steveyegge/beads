package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func newTestSQLiteStorage(t *testing.T) *sqlite.SQLiteStorage {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if err := store.SetConfig(context.Background(), "issue_prefix", "ui"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close storage: %v", err)
		}
	})
	return store
}

func mustCreateIssue(t *testing.T, store *sqlite.SQLiteStorage, issue *types.Issue) {
	t.Helper()

	now := time.Now().UTC()
	if issue.CreatedAt.IsZero() {
		issue.CreatedAt = now
	}
	if issue.UpdatedAt.IsZero() {
		issue.UpdatedAt = now
	}
	if issue.Status == "" {
		issue.Status = types.StatusOpen
	}
	if issue.IssueType == "" {
		issue.IssueType = types.TypeTask
	}

	if err := store.CreateIssue(context.Background(), issue, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
}

func decodeIssueList(t *testing.T, data json.RawMessage) []types.Issue {
	t.Helper()
	var issues []types.Issue
	if err := json.Unmarshal(data, &issues); err != nil {
		t.Fatalf("decode issue list: %v", err)
	}
	return issues
}

func TestDirectListClientSuccess(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	mustCreateIssue(t, store, &types.Issue{ID: "ui-1", Title: "Open issue", Status: types.StatusOpen, Priority: 1})
	closedAt := time.Now().UTC()
	mustCreateIssue(t, store, &types.Issue{
		ID:        "ui-2",
		Title:     "Closed issue",
		Status:    types.StatusClosed,
		Priority:  3,
		ClosedAt:  &closedAt,
		CreatedAt: closedAt,
		UpdatedAt: closedAt,
	})

	client := NewDirectListClient(store)
	resp, err := client.List(&rpc.ListArgs{
		Status: string(types.StatusOpen),
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %s", resp.Error)
	}

	issues := decodeIssueList(t, resp.Data)
	if len(issues) != 1 || issues[0].ID != "ui-1" {
		t.Fatalf("unexpected issues: %+v", issues)
	}
}

func TestDirectListClientError(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	client := NewDirectListClient(store)
	resp, err := client.List(&rpc.ListArgs{})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure response")
	}
	if !strings.Contains(resp.Error, "closed") {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}

func TestDirectDetailClientShowAggregatesRelatedData(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	now := time.Now().UTC()
	mustCreateIssue(t, store, &types.Issue{ID: "ui-1", Title: "Parent", Status: types.StatusOpen, CreatedAt: now, UpdatedAt: now})
	mustCreateIssue(t, store, &types.Issue{ID: "ui-2", Title: "Child", Status: types.StatusOpen})
	mustCreateIssue(t, store, &types.Issue{ID: "ui-3", Title: "Dependent", Status: types.StatusOpen})

	if err := store.AddLabel(context.Background(), "ui-1", "bug", "test"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	if err := store.AddDependency(context.Background(), &types.Dependency{
		IssueID:     "ui-1",
		DependsOnID: "ui-2",
		Type:        types.DepBlocks,
	}, "test"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	if err := store.AddDependency(context.Background(), &types.Dependency{
		IssueID:     "ui-3",
		DependsOnID: "ui-1",
		Type:        types.DepBlocks,
	}, "test"); err != nil {
		t.Fatalf("AddDependency dependent: %v", err)
	}

	client := NewDirectDetailClient(store)
	resp, err := client.Show(&rpc.ShowArgs{ID: "ui-1"})
	if err != nil {
		t.Fatalf("Show returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %s", resp.Error)
	}

	var payload struct {
		ID                string             `json:"id"`
		Title             string             `json:"title"`
		Labels            []string           `json:"labels"`
		Dependencies      []types.Issue      `json:"dependencies"`
		Dependents        []types.Issue      `json:"dependents"`
		DependencyRecords []types.Dependency `json:"dependency_records"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("decode detail payload: %v", err)
	}

	if payload.ID != "ui-1" || payload.Title != "Parent" {
		t.Fatalf("unexpected issue payload: %+v", payload)
	}
	if len(payload.Labels) != 1 || payload.Labels[0] != "bug" {
		t.Fatalf("labels = %+v, want [bug]", payload.Labels)
	}
	if len(payload.Dependencies) != 1 || payload.Dependencies[0].ID != "ui-2" {
		t.Fatalf("dependencies = %+v", payload.Dependencies)
	}
	if len(payload.Dependents) != 1 || payload.Dependents[0].ID != "ui-3" {
		t.Fatalf("dependents = %+v", payload.Dependents)
	}
	if len(payload.DependencyRecords) != 1 || payload.DependencyRecords[0].DependsOnID != "ui-2" {
		t.Fatalf("dependency records = %+v", payload.DependencyRecords)
	}
}

func TestDirectDetailClientValidatesID(t *testing.T) {
	t.Parallel()

	client := NewDirectDetailClient(newTestSQLiteStorage(t))
	resp, err := client.Show(&rpc.ShowArgs{ID: "   "})
	if err != nil {
		t.Fatalf("Show returned error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure for blank id")
	}
}

func TestDirectDetailClientMissingIssue(t *testing.T) {
	t.Parallel()

	client := NewDirectDetailClient(newTestSQLiteStorage(t))
	resp, err := client.Show(&rpc.ShowArgs{ID: "ui-404"})
	if err != nil {
		t.Fatalf("Show returned error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure response for missing issue")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 status, got %d", resp.StatusCode)
	}
	if !strings.Contains(strings.ToLower(resp.Error), "not found") {
		t.Fatalf("expected not found error, got %q", resp.Error)
	}
}

func TestDirectUpdateClientAppliesUpdates(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	now := time.Now().UTC()
	mustCreateIssue(t, store, &types.Issue{
		ID:        "ui-9",
		Title:     "Original",
		Status:    types.StatusOpen,
		Priority:  1,
		CreatedAt: now,
		UpdatedAt: now,
	})

	client := NewDirectUpdateClient(store)
	title := "Updated"
	description := "Documented"
	status := string(types.StatusInProgress)
	priority := 3
	assignee := "alice"

	resp, err := client.Update(&rpc.UpdateArgs{
		ID:          "ui-9",
		Title:       &title,
		Description: &description,
		Status:      &status,
		Priority:    &priority,
		Assignee:    &assignee,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got %s", resp.Error)
	}

	updated, err := store.GetIssue(context.Background(), "ui-9")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated == nil {
		t.Fatalf("issue not found after update")
	}
	if updated.Title != "Updated" || updated.Status != types.StatusInProgress || updated.Priority != 3 {
		t.Fatalf("unexpected updated issue: %+v", updated)
	}
	if updated.Description != "Documented" {
		t.Fatalf("expected description to update, got %q", updated.Description)
	}
	if updated.Assignee != "alice" {
		t.Fatalf("expected assignee alice, got %s", updated.Assignee)
	}
}

func TestDirectUpdateClientValidatesID(t *testing.T) {
	t.Parallel()

	client := NewDirectUpdateClient(newTestSQLiteStorage(t))
	resp, err := client.Update(&rpc.UpdateArgs{ID: "  "})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure for blank id")
	}
}

func TestDirectCreateClientCreatesIssueAndDependencies(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	mustCreateIssue(t, store, &types.Issue{ID: "ui-1", Title: "Existing A"})
	mustCreateIssue(t, store, &types.Issue{ID: "ui-2", Title: "Existing B"})

	client := NewDirectCreateClient(store)
	resp, err := client.Create(&rpc.CreateArgs{
		ID:           "ui-20",
		Title:        "New Issue",
		Description:  "details",
		IssueType:    string(types.TypeFeature),
		Priority:     2,
		Labels:       []string{"UI", " feature "},
		Dependencies: []string{"ui-1", "  ", "ui-2"},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got %s", resp.Error)
	}

	created, err := store.GetIssue(context.Background(), "ui-20")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if created == nil {
		t.Fatalf("issue not persisted")
	}
	if created.IssueType != types.TypeFeature || created.Priority != 2 {
		t.Fatalf("unexpected created issue: %+v", created)
	}

	deps, err := store.GetDependencies(context.Background(), "ui-20")
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(deps))
	}
}

func TestDirectCreateClientValidatesTitle(t *testing.T) {
	t.Parallel()

	client := NewDirectCreateClient(newTestSQLiteStorage(t))
	resp, err := client.Create(&rpc.CreateArgs{Title: "   "})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected validation error")
	}
}

func TestDirectLabelClientAddRemove(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	mustCreateIssue(t, store, &types.Issue{ID: "ui-5", Title: "Label target"})

	client := NewDirectLabelClient(store)
	resp, err := client.AddLabel(&rpc.LabelAddArgs{ID: "ui-5", Label: " needs-docs "})
	if err != nil || !resp.Success {
		t.Fatalf("AddLabel failed: resp=%+v err=%v", resp, err)
	}

	labels, err := store.GetLabels(context.Background(), "ui-5")
	if err != nil {
		t.Fatalf("GetLabels: %v", err)
	}
	if len(labels) != 1 || labels[0] != "needs-docs" {
		t.Fatalf("label not stored: %+v", labels)
	}

	resp, err = client.RemoveLabel(&rpc.LabelRemoveArgs{ID: "ui-5", Label: "needs-docs"})
	if err != nil || !resp.Success {
		t.Fatalf("RemoveLabel failed: resp=%+v err=%v", resp, err)
	}
	labels, err = store.GetLabels(context.Background(), "ui-5")
	if err != nil {
		t.Fatalf("GetLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("expected labels cleared, got %+v", labels)
	}
}

func TestDirectLabelClientValidatesInput(t *testing.T) {
	t.Parallel()

	client := NewDirectLabelClient(newTestSQLiteStorage(t))
	resp, err := client.AddLabel(&rpc.LabelAddArgs{ID: "   ", Label: "x"})
	if err != nil {
		t.Fatalf("AddLabel returned error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected invalid issue id error")
	}
}

func TestDirectBulkClientBatchUpdates(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	mustCreateIssue(t, store, &types.Issue{ID: "ui-3", Title: "Old"})

	bulk := NewDirectBulkClient(store)
	title := "Bulk Updated"
	updateArgs := rpc.UpdateArgs{ID: "ui-3", Title: &title}
	payload, err := json.Marshal(updateArgs)
	if err != nil {
		t.Fatalf("marshal update args: %v", err)
	}

	resp, err := bulk.Batch(&rpc.BatchArgs{
		Operations: []rpc.BatchOperation{
			{Operation: rpc.OpUpdate, Args: payload},
		},
	})
	if err != nil {
		t.Fatalf("Batch returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %s", resp.Error)
	}

	var batchResp rpc.BatchResponse
	if err := json.Unmarshal(resp.Data, &batchResp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(batchResp.Results) != 1 || !batchResp.Results[0].Success {
		t.Fatalf("unexpected batch results: %+v", batchResp.Results)
	}

	updated, err := store.GetIssue(context.Background(), "ui-3")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Title != "Bulk Updated" {
		t.Fatalf("expected updated title, got %+v", updated.Title)
	}
}

func TestDirectBulkClientRejectsUnsupportedOperation(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	bulk := NewDirectBulkClient(store)

	resp, err := bulk.Batch(&rpc.BatchArgs{
		Operations: []rpc.BatchOperation{
			{Operation: "delete"},
		},
	})
	if err != nil {
		t.Fatalf("Batch returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected wrapper success, got %s", resp.Error)
	}
	var batchResp rpc.BatchResponse
	if err := json.Unmarshal(resp.Data, &batchResp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(batchResp.Results) != 1 || batchResp.Results[0].Success {
		t.Fatalf("expected failure result for unsupported operation: %+v", batchResp.Results)
	}
}

func TestDirectDeleteClientTrimsID(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	mustCreateIssue(t, store, &types.Issue{ID: "ui-7", Title: "Delete me"})

	deleteClient := NewDirectDeleteClient(store)
	if deleteClient == nil {
		t.Fatalf("expected delete client instance")
	}
	if err := deleteClient.DeleteIssue(context.Background(), "  ui-7  "); err != nil {
		t.Fatalf("DeleteIssue returned error: %v", err)
	}

	issue, err := store.GetIssue(context.Background(), "ui-7")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue != nil {
		t.Fatalf("expected issue to be deleted")
	}
}

func TestNewDirectDeleteClientNil(t *testing.T) {
	t.Parallel()

	if client := NewDirectDeleteClient(nil); client != nil {
		t.Fatalf("expected nil client when storage nil")
	}
}

func TestDirectLabelClientRemoveLabelError(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStorage(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	client := NewDirectLabelClient(store)
	resp, err := client.RemoveLabel(&rpc.LabelRemoveArgs{ID: "ui-8", Label: "triage"})
	if err != nil {
		t.Fatalf("RemoveLabel returned unexpected error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure when storage remove label fails")
	}
	if !strings.Contains(strings.ToLower(resp.Error), "closed") {
		t.Fatalf("expected closed database error, got %q", resp.Error)
	}
}

func TestDirectBulkClientZeroOperations(t *testing.T) {
	t.Parallel()

	client := NewDirectBulkClient(newTestSQLiteStorage(t))
	resp, err := client.Batch(&rpc.BatchArgs{})
	if err != nil {
		t.Fatalf("Batch returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success for empty operations, got %s", resp.Error)
	}

	batch := decodeBatchResponse(t, resp)
	if len(batch.Results) != 0 {
		t.Fatalf("expected no results for empty operations, got %+v", batch.Results)
	}
}

func TestDirectBulkClientMissingOperationShortCircuits(t *testing.T) {
	t.Parallel()

	client := NewDirectBulkClient(newTestSQLiteStorage(t))
	resp, err := client.Batch(&rpc.BatchArgs{
		Operations: []rpc.BatchOperation{
			{Operation: " ", Args: mustJSON(rpc.UpdateArgs{ID: "ui-1"})},
			{Operation: rpc.OpUpdate, Args: mustJSON(rpc.UpdateArgs{ID: "ui-2"})},
		},
	})
	if err != nil {
		t.Fatalf("Batch returned error: %v", err)
	}

	batch := decodeBatchResponse(t, resp)
	if len(batch.Results) != 1 {
		t.Fatalf("expected single result when first operation invalid, got %+v", batch.Results)
	}
	if batch.Results[0].Success {
		t.Fatalf("expected failure result for missing operation metadata, got %+v", batch.Results[0])
	}
	if !strings.Contains(batch.Results[0].Error, "missing operation") {
		t.Fatalf("expected missing operation error, got %q", batch.Results[0].Error)
	}
}

func TestDirectBulkClientDecodeFailure(t *testing.T) {
	t.Parallel()

	client := NewDirectBulkClient(newTestSQLiteStorage(t))
	resp, err := client.Batch(&rpc.BatchArgs{
		Operations: []rpc.BatchOperation{
			{Operation: rpc.OpUpdate, Args: json.RawMessage(`{"id":`)},
		},
	})
	if err != nil {
		t.Fatalf("Batch returned error: %v", err)
	}

	batch := decodeBatchResponse(t, resp)
	if len(batch.Results) != 1 || batch.Results[0].Success {
		t.Fatalf("expected single failure result for decode error, got %+v", batch.Results)
	}
	if !strings.Contains(batch.Results[0].Error, "decode update args") {
		t.Fatalf("expected decode error, got %q", batch.Results[0].Error)
	}
}

func TestDirectBulkClientStopsOnUpdateFailure(t *testing.T) {
	t.Parallel()

	client := NewDirectBulkClient(newTestSQLiteStorage(t))
	resp, err := client.Batch(&rpc.BatchArgs{
		Operations: []rpc.BatchOperation{
			{Operation: rpc.OpUpdate, Args: mustJSON(rpc.UpdateArgs{ID: "   "})},
			{Operation: rpc.OpUpdate, Args: mustJSON(rpc.UpdateArgs{ID: "ui-10"})},
		},
	})
	if err != nil {
		t.Fatalf("Batch returned error: %v", err)
	}

	batch := decodeBatchResponse(t, resp)
	if len(batch.Results) != 1 {
		t.Fatalf("expected short-circuit after failure, got %+v", batch.Results)
	}
	if batch.Results[0].Success {
		t.Fatalf("expected first operation to fail, got %+v", batch.Results[0])
	}
	if !strings.Contains(batch.Results[0].Error, "invalid issue id") {
		t.Fatalf("expected invalid id error, got %q", batch.Results[0].Error)
	}
}

func decodeBatchResponse(t *testing.T, resp *rpc.Response) rpc.BatchResponse {
	t.Helper()

	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if !resp.Success {
		t.Fatalf("expected success wrapper, got %s", resp.Error)
	}

	var batch rpc.BatchResponse
	if err := json.Unmarshal(resp.Data, &batch); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	return batch
}
