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

	filtered, skipped, err := filterStaleImportIssues(context.Background(), store, incoming)
	if err != nil {
		t.Fatalf("filterStaleImportIssues: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
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
