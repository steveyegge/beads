package tracker

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestEnginePullRecordsPushHash verifies that a pull refreshes the stored push
// hash. The remote holds the pulled content after a pull. If the hash still
// described the last push, a local change back to that content would match
// the hash, and the push would skip it while the remote keeps the pulled content.
func TestEnginePullRecordsPushHash(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	ref := "https://test.test/EXT-1"
	local := &types.Issue{
		ID: "hash-after-pull", Title: "pushed", Status: types.StatusOpen,
		Priority: 2, IssueType: types.TypeTask, ExternalRef: &ref, UpdatedAt: base,
	}
	store := &pullFailureStore{
		pureTestStore: newPureTestStore(local),
		tx:            &pullTestTransaction{issue: local},
	}
	mock := newMockTracker("test")
	mock.issues = []TrackerIssue{{ID: "EXT-1", Identifier: "EXT-1", Title: "pushed", UpdatedAt: base}}

	engine := NewEngine(mock, store, "sync")
	engine.PushHooks = &PushHooks{
		ContentEqual: func(local *types.Issue, remote *TrackerIssue) bool { return local.Title == remote.Title },
		ContentHash:  func(local *types.Issue) string { return "h:" + local.Title },
	}
	// The last push sent "pushed".
	engine.recordPushHash(ctx, local, ref)

	// The remote changes, and a pull imports the change.
	mock.issues[0].Title = "remote edit"
	mock.issues[0].UpdatedAt = base.Add(time.Hour)
	if _, err := engine.Sync(ctx, SyncOptions{Pull: true}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if local.Title != "remote edit" {
		t.Fatalf("title after the pull = %q, want %q", local.Title, "remote edit")
	}
	pulled := &types.Issue{ID: local.ID, Title: "remote edit"}
	if got, want := store.localMetadata["test.pushhash.hash-after-pull"], engine.pushCacheValue(pulled, ref); got != want {
		t.Fatalf("push hash after the pull = %q, want %q", got, want)
	}

	// The local issue goes back to the last pushed content. The push must update the remote.
	local.Title = "pushed"
	local.UpdatedAt = base.Add(2 * time.Hour)
	if _, err := engine.Sync(ctx, SyncOptions{Push: true}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := mock.updated["EXT-1"]; got == nil || got.Title != "pushed" {
		t.Fatalf("push of the revert: updated = %+v, want the title %q", got, "pushed")
	}
}
