//go:build integration

package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func getTestRedisURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("BD_TEST_REDIS_URL")
	if url == "" {
		t.Skip("BD_TEST_REDIS_URL not set, skipping Redis integration tests")
	}
	return url
}

func newTestRedisStore(t *testing.T, opts ...RedisWispOption) *redisWispStore {
	t.Helper()
	url := getTestRedisURL(t)

	// Use a unique namespace per test to avoid interference
	ns := fmt.Sprintf("bd-test-%d", time.Now().UnixNano())
	allOpts := append([]RedisWispOption{WithNamespace(ns)}, opts...)

	store, err := NewRedisWispStore(url, allOpts...)
	if err != nil {
		t.Fatalf("NewRedisWispStore() error = %v", err)
	}

	rs := store.(*redisWispStore)
	t.Cleanup(func() {
		rs.Clear()
		rs.Close()
	})

	return rs
}

func TestRedisWispStore_Create(t *testing.T) {
	store := newTestRedisStore(t)
	ctx := context.Background()

	t.Run("creates wisp successfully", func(t *testing.T) {
		issue := &types.Issue{
			ID:    "test-wisp-001",
			Title: "Test Wisp",
		}

		err := store.Create(ctx, issue)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, err := store.Get(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got == nil {
			t.Fatal("Get() returned nil")
		}
		if got.Title != issue.Title {
			t.Errorf("Title = %v, want %v", got.Title, issue.Title)
		}
		if !got.Ephemeral {
			t.Error("Ephemeral = false, want true")
		}
	})

	t.Run("rejects duplicate ID", func(t *testing.T) {
		issue := &types.Issue{
			ID:    "test-wisp-002",
			Title: "Original",
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("first Create() error = %v", err)
		}

		duplicate := &types.Issue{
			ID:    "test-wisp-002",
			Title: "Duplicate",
		}

		err := store.Create(ctx, duplicate)
		if err == nil {
			t.Error("Create() should have failed for duplicate ID")
		}
	})

	t.Run("rejects nil issue", func(t *testing.T) {
		err := store.Create(ctx, nil)
		if err == nil {
			t.Error("Create(nil) should have failed")
		}
	})

	t.Run("rejects empty ID", func(t *testing.T) {
		issue := &types.Issue{
			Title: "No ID",
		}

		err := store.Create(ctx, issue)
		if err == nil {
			t.Error("Create() with empty ID should have failed")
		}
	})

	t.Run("sets timestamps", func(t *testing.T) {
		before := time.Now().Add(-time.Second)
		issue := &types.Issue{
			ID:    "test-wisp-003",
			Title: "Timestamped",
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		after := time.Now().Add(time.Second)

		got, _ := store.Get(ctx, issue.ID)
		if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
			t.Errorf("CreatedAt = %v, want between %v and %v", got.CreatedAt, before, after)
		}
	})
}

func TestRedisWispStore_Get(t *testing.T) {
	store := newTestRedisStore(t)
	ctx := context.Background()

	t.Run("returns nil for nonexistent", func(t *testing.T) {
		got, err := store.Get(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got != nil {
			t.Errorf("Get() = %v, want nil", got)
		}
	})

	t.Run("preserves all fields", func(t *testing.T) {
		closedAt := time.Now().Add(-time.Hour)
		est := 30
		issue := &types.Issue{
			ID:               "test-wisp-fields",
			Title:            "Full Fields",
			Description:      "A description",
			Status:           types.StatusOpen,
			Priority:         2,
			IssueType:        "task",
			Assignee:         "user@example.com",
			Labels:           []string{"label1", "label2"},
			ClosedAt:         &closedAt,
			EstimatedMinutes: &est,
			Notes:            "Some notes",
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, err := store.Get(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Description != issue.Description {
			t.Errorf("Description = %v, want %v", got.Description, issue.Description)
		}
		if got.Priority != issue.Priority {
			t.Errorf("Priority = %v, want %v", got.Priority, issue.Priority)
		}
		if got.Assignee != issue.Assignee {
			t.Errorf("Assignee = %v, want %v", got.Assignee, issue.Assignee)
		}
		if len(got.Labels) != 2 {
			t.Errorf("Labels = %v, want 2 labels", got.Labels)
		}
		if got.EstimatedMinutes == nil || *got.EstimatedMinutes != 30 {
			t.Errorf("EstimatedMinutes = %v, want 30", got.EstimatedMinutes)
		}
	})
}

func TestRedisWispStore_List(t *testing.T) {
	store := newTestRedisStore(t)
	ctx := context.Background()

	issues := []*types.Issue{
		{ID: "wisp-1", Title: "First", Status: types.StatusOpen, Priority: 1, Labels: []string{"bug"}},
		{ID: "wisp-2", Title: "Second", Status: types.StatusOpen, Priority: 2, Labels: []string{"feature"}},
		{ID: "wisp-3", Title: "Third", Status: types.StatusClosed, Priority: 1, Labels: []string{"bug", "urgent"}},
	}
	for _, issue := range issues {
		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	t.Run("returns all with empty filter", func(t *testing.T) {
		got, err := store.List(ctx, types.IssueFilter{})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 3 {
			t.Errorf("List() returned %d issues, want 3", len(got))
		}
	})

	t.Run("filters by status", func(t *testing.T) {
		status := types.StatusOpen
		got, err := store.List(ctx, types.IssueFilter{Status: &status})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("List() returned %d issues, want 2", len(got))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		got, err := store.List(ctx, types.IssueFilter{Limit: 2})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("List() returned %d issues, want 2", len(got))
		}
	})
}

func TestRedisWispStore_Update(t *testing.T) {
	store := newTestRedisStore(t)
	ctx := context.Background()

	t.Run("updates existing wisp", func(t *testing.T) {
		issue := &types.Issue{
			ID:    "wisp-update-1",
			Title: "Original",
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		issue.Title = "Updated"
		if err := store.Update(ctx, issue); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		got, _ := store.Get(ctx, issue.ID)
		if got.Title != "Updated" {
			t.Errorf("Title = %v, want Updated", got.Title)
		}
	})

	t.Run("fails for nonexistent", func(t *testing.T) {
		issue := &types.Issue{
			ID:    "nonexistent",
			Title: "Doesn't exist",
		}

		err := store.Update(ctx, issue)
		if err == nil {
			t.Error("Update() should have failed for nonexistent wisp")
		}
	})
}

func TestRedisWispStore_Delete(t *testing.T) {
	store := newTestRedisStore(t)
	ctx := context.Background()

	t.Run("deletes existing wisp", func(t *testing.T) {
		issue := &types.Issue{
			ID:    "wisp-delete-1",
			Title: "To be deleted",
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if err := store.Delete(ctx, issue.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		got, _ := store.Get(ctx, issue.ID)
		if got != nil {
			t.Error("Wisp still exists after Delete()")
		}
	})

	t.Run("fails for nonexistent", func(t *testing.T) {
		err := store.Delete(ctx, "nonexistent")
		if err == nil {
			t.Error("Delete() should have failed for nonexistent wisp")
		}
	})
}

func TestRedisWispStore_Count(t *testing.T) {
	store := newTestRedisStore(t)
	ctx := context.Background()

	if store.Count() != 0 {
		t.Errorf("Count() = %d, want 0 for new store", store.Count())
	}

	for i := 0; i < 5; i++ {
		issue := &types.Issue{
			ID:    fmt.Sprintf("wisp-count-%d", i),
			Title: fmt.Sprintf("Wisp %d", i),
		}
		store.Create(ctx, issue)
	}

	if store.Count() != 5 {
		t.Errorf("Count() = %d, want 5", store.Count())
	}
}

func TestRedisWispStore_SurvivesPodRecycling(t *testing.T) {
	url := getTestRedisURL(t)
	ns := fmt.Sprintf("bd-test-recycle-%d", time.Now().UnixNano())
	ctx := context.Background()

	// "Pod 1" creates wisps
	store1, err := NewRedisWispStore(url, WithNamespace(ns))
	if err != nil {
		t.Fatalf("NewRedisWispStore() error = %v", err)
	}

	issue := &types.Issue{
		ID:          "wisp-survive-1",
		Title:       "Survives Restart",
		Description: "This wisp should survive pod recycling",
		Labels:      []string{"important"},
	}

	if err := store1.Create(ctx, issue); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// "Pod 1 dies"
	store1.Close()

	// "Pod 2 starts" - new store, same Redis
	store2, err := NewRedisWispStore(url, WithNamespace(ns))
	if err != nil {
		t.Fatalf("NewRedisWispStore() for pod 2 error = %v", err)
	}
	defer func() {
		store2.(*redisWispStore).Clear()
		store2.Close()
	}()

	got, err := store2.Get(ctx, "wisp-survive-1")
	if err != nil {
		t.Fatalf("Get() after restart error = %v", err)
	}
	if got == nil {
		t.Fatal("Wisp not found after pod recycling")
	}
	if got.Title != "Survives Restart" {
		t.Errorf("Title = %v, want 'Survives Restart'", got.Title)
	}
	if got.Description != "This wisp should survive pod recycling" {
		t.Errorf("Description mismatch after recycling")
	}
	if len(got.Labels) != 1 || got.Labels[0] != "important" {
		t.Errorf("Labels = %v, want [important]", got.Labels)
	}
}

func TestRedisWispStore_MultipleInstances(t *testing.T) {
	url := getTestRedisURL(t)
	ns := fmt.Sprintf("bd-test-multi-%d", time.Now().UnixNano())
	ctx := context.Background()

	store1, err := NewRedisWispStore(url, WithNamespace(ns))
	if err != nil {
		t.Fatalf("store1 error = %v", err)
	}
	defer func() {
		store1.(*redisWispStore).Clear()
		store1.Close()
	}()

	store2, err := NewRedisWispStore(url, WithNamespace(ns))
	if err != nil {
		t.Fatalf("store2 error = %v", err)
	}
	defer store2.Close()

	// Store1 creates wisp
	issue := &types.Issue{
		ID:    "wisp-shared-1",
		Title: "Created by store1",
	}
	if err := store1.Create(ctx, issue); err != nil {
		t.Fatalf("store1.Create() error = %v", err)
	}

	// Store2 can read it immediately
	got, err := store2.Get(ctx, "wisp-shared-1")
	if err != nil {
		t.Fatalf("store2.Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("store2 can't see wisp created by store1")
	}
	if got.Title != "Created by store1" {
		t.Errorf("Title = %v, want 'Created by store1'", got.Title)
	}

	// Store2 updates wisp
	got.Title = "Updated by store2"
	if err := store2.Update(ctx, got); err != nil {
		t.Fatalf("store2.Update() error = %v", err)
	}

	// Store1 sees the update
	got2, err := store1.Get(ctx, "wisp-shared-1")
	if err != nil {
		t.Fatalf("store1.Get() after update error = %v", err)
	}
	if got2.Title != "Updated by store2" {
		t.Errorf("store1 sees Title = %v, want 'Updated by store2'", got2.Title)
	}
}

func TestRedisWispStore_TTLExpiry(t *testing.T) {
	store := newTestRedisStore(t, WithTTL(2*time.Second))
	ctx := context.Background()

	issue := &types.Issue{
		ID:    "wisp-ttl-1",
		Title: "Expires Soon",
	}

	if err := store.Create(ctx, issue); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify exists
	got, err := store.Get(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Wisp should exist immediately after creation")
	}

	// Wait for TTL expiry
	time.Sleep(3 * time.Second)

	// Verify expired
	got, err = store.Get(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Get() after TTL error = %v", err)
	}
	if got != nil {
		t.Error("Wisp should have expired after TTL")
	}
}

func TestRedisWispStore_ConnectionFailure(t *testing.T) {
	// Try to create store with invalid URL
	_, err := NewRedisWispStore("redis://localhost:19999/0")
	if err == nil {
		t.Error("NewRedisWispStore() should fail with unreachable Redis")
	}
}

func TestRedisWispStore_Concurrent(t *testing.T) {
	store := newTestRedisStore(t)
	ctx := context.Background()

	const numGoroutines = 10
	const numOps = 50

	// Create initial wisps
	for i := 0; i < numGoroutines; i++ {
		issue := &types.Issue{
			ID:    fmt.Sprintf("wisp-concurrent-%d", i),
			Title: fmt.Sprintf("Wisp %d", i),
		}
		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			wispID := fmt.Sprintf("wisp-concurrent-%d", id)

			for j := 0; j < numOps; j++ {
				// Read
				_, _ = store.Get(ctx, wispID)

				// List
				_, _ = store.List(ctx, types.IssueFilter{})

				// Update
				issue := &types.Issue{
					ID:    wispID,
					Title: fmt.Sprintf("Updated %d-%d", id, j),
				}
				_ = store.Update(ctx, issue)
			}
		}(i)
	}

	wg.Wait()

	// Verify store is still consistent
	if store.Count() != numGoroutines {
		t.Errorf("Count() = %d after concurrent ops, want %d", store.Count(), numGoroutines)
	}
}
