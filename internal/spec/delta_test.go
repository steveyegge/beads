package spec

import (
	"testing"
	"time"
)

func TestComputeDelta(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	prev := []SpecSnapshot{
		{SpecID: "specs/a.md", Title: "A", Lifecycle: "active", SHA256: "aaa", Mtime: now},
		{SpecID: "specs/b.md", Title: "B", Lifecycle: "active", SHA256: "bbb", Mtime: now},
	}
	curr := []SpecSnapshot{
		{SpecID: "specs/a.md", Title: "A2", Lifecycle: "done", SHA256: "aaa", Mtime: now},
		{SpecID: "specs/c.md", Title: "C", Lifecycle: "active", SHA256: "ccc", Mtime: now},
	}

	result := ComputeDelta(prev, curr)
	if len(result.Added) != 1 {
		t.Fatalf("added = %d, want 1", len(result.Added))
	}
	if len(result.Removed) != 1 {
		t.Fatalf("removed = %d, want 1", len(result.Removed))
	}
	if len(result.Changed) == 0 {
		t.Fatalf("expected changes, got none")
	}
}
