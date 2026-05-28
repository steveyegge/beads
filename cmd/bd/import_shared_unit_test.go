package main

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

type fakeImportIssueLookupStore struct {
	storage.DoltStorage
	issues []*types.Issue
}

func (f *fakeImportIssueLookupStore) GetIssuesByIDs(_ context.Context, _ []string) ([]*types.Issue, error) {
	return f.issues, nil
}

func TestFilterStaleImportIssuesSkipsOlderIncomingRecords(t *testing.T) {
	base := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	incoming := []*types.Issue{
		{ID: "bd-stale", Title: "stale snapshot", UpdatedAt: base},
		{ID: "bd-equal", Title: "same snapshot time", UpdatedAt: base},
		{ID: "bd-newer", Title: "newer snapshot", UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "bd-new", Title: "new record", UpdatedAt: base},
	}
	store := &fakeImportIssueLookupStore{issues: []*types.Issue{
		{ID: "bd-stale", UpdatedAt: base.Add(time.Hour)},
		{ID: "bd-equal", UpdatedAt: base},
		{ID: "bd-newer", UpdatedAt: base.Add(time.Hour)},
	}}

	filtered, skippedIDs, err := filterStaleImportIssues(context.Background(), store, incoming)
	if err != nil {
		t.Fatalf("filterStaleImportIssues: %v", err)
	}
	if len(skippedIDs) != 1 || skippedIDs[0] != "bd-stale" {
		t.Fatalf("skippedIDs = %#v, want [bd-stale]", skippedIDs)
	}

	got := make(map[string]bool, len(filtered))
	for _, issue := range filtered {
		got[issue.ID] = true
	}
	for _, id := range []string{"bd-equal", "bd-newer", "bd-new"} {
		if !got[id] {
			t.Fatalf("filtered issues missing %s: %#v", id, got)
		}
	}
	if got["bd-stale"] {
		t.Fatalf("stale issue was not filtered: %#v", got)
	}
}

func TestImportIssuesCoreReportsStaleSkippedIDs(t *testing.T) {
	base := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	store := &fakeImportIssueLookupStore{issues: []*types.Issue{
		{ID: "bd-stale", UpdatedAt: base.Add(time.Hour)},
	}}

	result, err := importIssuesCore(context.Background(), "", store, []*types.Issue{
		{ID: "bd-stale", Title: "stale snapshot", UpdatedAt: base},
	}, ImportOptions{})
	if err != nil {
		t.Fatalf("importIssuesCore: %v", err)
	}
	if result.Created != 0 {
		t.Fatalf("Created = %d, want 0", result.Created)
	}
	if result.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.ImportedIDs) != 0 {
		t.Fatalf("ImportedIDs = %#v, want empty", result.ImportedIDs)
	}
	if len(result.StaleSkippedIDs) != 1 || result.StaleSkippedIDs[0] != "bd-stale" {
		t.Fatalf("StaleSkippedIDs = %#v, want [bd-stale]", result.StaleSkippedIDs)
	}
}
