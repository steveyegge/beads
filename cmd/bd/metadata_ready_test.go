//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestGetReadyWork_MetadataFieldMatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	store := newTestStore(t, tmpDir)
	ctx := context.Background()

	issue1 := &types.Issue{
		Title:     "Platform task",
		Priority:  2,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Metadata:  json.RawMessage(`{"team":"platform"}`),
	}
	issue2 := &types.Issue{
		Title:     "Frontend task",
		Priority:  2,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Metadata:  json.RawMessage(`{"team":"frontend"}`),
	}
	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	results, err := store.GetReadyWork(ctx, types.WorkFilter{
		Status:         "open",
		MetadataFields: map[string]string{"team": "platform"},
	})
	if err != nil {
		t.Fatalf("GetReadyWork: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != issue1.ID {
		t.Errorf("expected issue %s, got %s", issue1.ID, results[0].ID)
	}
}

func TestGetReadyWork_HasMetadataKey(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	store := newTestStore(t, tmpDir)
	ctx := context.Background()

	issue1 := &types.Issue{
		Title:     "Has team",
		Priority:  2,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Metadata:  json.RawMessage(`{"team":"platform"}`),
	}
	issue2 := &types.Issue{
		Title:     "No metadata",
		Priority:  2,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	results, err := store.GetReadyWork(ctx, types.WorkFilter{
		Status:         "open",
		HasMetadataKey: "team",
	})
	if err != nil {
		t.Fatalf("GetReadyWork: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != issue1.ID {
		t.Errorf("expected issue %s, got %s", issue1.ID, results[0].ID)
	}
}

func TestGetReadyWork_MetadataFieldNoMatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	store := newTestStore(t, tmpDir)
	ctx := context.Background()

	issue := &types.Issue{
		Title:     "Platform task",
		Priority:  2,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Metadata:  json.RawMessage(`{"team":"platform"}`),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	results, err := store.GetReadyWork(ctx, types.WorkFilter{
		Status:         "open",
		MetadataFields: map[string]string{"team": "backend"},
	})
	if err != nil {
		t.Fatalf("GetReadyWork: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestGetReadyWork_MetadataFieldInvalidKey(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	store := newTestStore(t, tmpDir)
	ctx := context.Background()

	_, err := store.GetReadyWork(ctx, types.WorkFilter{
		Status:         "open",
		MetadataFields: map[string]string{"'; DROP TABLE issues; --": "val"},
	})
	if err == nil {
		t.Fatal("expected error for invalid metadata key, got nil")
	}
}
