package daemon

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestWispStore_Create(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
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

		// Verify it was created
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
		before := time.Now()
		issue := &types.Issue{
			ID:    "test-wisp-003",
			Title: "Timestamped",
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		after := time.Now()

		got, _ := store.Get(ctx, issue.ID)
		if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
			t.Errorf("CreatedAt = %v, want between %v and %v", got.CreatedAt, before, after)
		}
		if got.UpdatedAt.Before(before) || got.UpdatedAt.After(after) {
			t.Errorf("UpdatedAt = %v, want between %v and %v", got.UpdatedAt, before, after)
		}
	})
}

func TestWispStore_Get(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
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

	t.Run("returns clone not original", func(t *testing.T) {
		issue := &types.Issue{
			ID:     "test-wisp-clone",
			Title:  "Original Title",
			Labels: []string{"label1"},
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, _ := store.Get(ctx, issue.ID)
		got.Title = "Modified"
		got.Labels = append(got.Labels, "label2")

		// Get again and verify original is unchanged
		got2, _ := store.Get(ctx, issue.ID)
		if got2.Title != "Original Title" {
			t.Errorf("Original was mutated: Title = %v", got2.Title)
		}
		if len(got2.Labels) != 1 {
			t.Errorf("Original was mutated: Labels = %v", got2.Labels)
		}
	})
}

func TestWispStore_List(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
	ctx := context.Background()

	// Create test data
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

	t.Run("filters by priority", func(t *testing.T) {
		priority := 1
		got, err := store.List(ctx, types.IssueFilter{Priority: &priority})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("List() returned %d issues, want 2", len(got))
		}
	})

	t.Run("filters by labels AND", func(t *testing.T) {
		got, err := store.List(ctx, types.IssueFilter{Labels: []string{"bug", "urgent"}})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("List() returned %d issues, want 1", len(got))
		}
		if got[0].ID != "wisp-3" {
			t.Errorf("List() returned %s, want wisp-3", got[0].ID)
		}
	})

	t.Run("filters by labels OR", func(t *testing.T) {
		got, err := store.List(ctx, types.IssueFilter{LabelsAny: []string{"feature", "urgent"}})
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

	t.Run("filters by title search", func(t *testing.T) {
		got, err := store.List(ctx, types.IssueFilter{TitleSearch: "first"})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("List() returned %d issues, want 1", len(got))
		}
	})

	t.Run("filters by ID prefix", func(t *testing.T) {
		got, err := store.List(ctx, types.IssueFilter{IDPrefix: "wisp-"})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 3 {
			t.Errorf("List() returned %d issues, want 3", len(got))
		}
	})
}

func TestWispStore_Update(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
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

	t.Run("updates timestamp", func(t *testing.T) {
		issue := &types.Issue{
			ID:    "wisp-update-2",
			Title: "Original",
		}

		if err := store.Create(ctx, issue); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		original, _ := store.Get(ctx, issue.ID)
		originalUpdated := original.UpdatedAt

		time.Sleep(10 * time.Millisecond)

		issue.Title = "Changed"
		if err := store.Update(ctx, issue); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		got, _ := store.Get(ctx, issue.ID)
		if !got.UpdatedAt.After(originalUpdated) {
			t.Error("UpdatedAt was not updated")
		}
	})
}

func TestWispStore_Delete(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
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

func TestWispStore_Count(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
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

func TestWispStore_Clear(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			ID:    fmt.Sprintf("wisp-clear-%d", i),
			Title: fmt.Sprintf("Wisp %d", i),
		}
		store.Create(ctx, issue)
	}

	store.Clear()

	if store.Count() != 0 {
		t.Errorf("Count() = %d after Clear(), want 0", store.Count())
	}
}

func TestWispStore_Close(t *testing.T) {
	store := NewWispStore()
	ctx := context.Background()

	issue := &types.Issue{
		ID:    "wisp-close-1",
		Title: "Test",
	}
	store.Create(ctx, issue)

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Operations should fail after close
	if err := store.Create(ctx, &types.Issue{ID: "new", Title: "New"}); err == nil {
		t.Error("Create() should fail after Close()")
	}

	if _, err := store.Get(ctx, "wisp-close-1"); err == nil {
		t.Error("Get() should fail after Close()")
	}

	if _, err := store.List(ctx, types.IssueFilter{}); err == nil {
		t.Error("List() should fail after Close()")
	}
}

func TestWispStore_Concurrent(t *testing.T) {
	store := NewWispStore()
	defer store.Close()
	ctx := context.Background()

	const numGoroutines = 10
	const numOps = 100

	var wg sync.WaitGroup

	// Create initial wisps
	for i := 0; i < numGoroutines; i++ {
		issue := &types.Issue{
			ID:    fmt.Sprintf("wisp-concurrent-%d", i),
			Title: fmt.Sprintf("Wisp %d", i),
		}
		store.Create(ctx, issue)
	}

	// Concurrent reads and updates
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
