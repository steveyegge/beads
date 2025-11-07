package api

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestClampClosedLimit(t *testing.T) {
	t.Run("returns zero for non-positive limits", func(t *testing.T) {
		for _, input := range []int{0, -1, -50} {
			if got := clampClosedLimit(input); got != 0 {
				t.Fatalf("expected 0 for input %d, got %d", input, got)
			}
		}
	})

	t.Run("clamps to maximum", func(t *testing.T) {
		if got := clampClosedLimit(closedQueueMaxLimit + 100); got != closedQueueMaxLimit {
			t.Fatalf("expected clamp to %d, got %d", closedQueueMaxLimit, got)
		}
	})

	t.Run("keeps values within range", func(t *testing.T) {
		if got := clampClosedLimit(42); got != 42 {
			t.Fatalf("expected 42, got %d", got)
		}
	})
}

func TestPaginateClosedIssues(t *testing.T) {
	now := time.Date(2025, 10, 29, 18, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{ID: "ui-1", ClosedAt: timePtr(now.Add(-1 * time.Minute))},
		{ID: "ui-2", ClosedAt: timePtr(now.Add(-2 * time.Minute))},
		{ID: "ui-3", ClosedAt: timePtr(now.Add(-3 * time.Minute))},
	}

	trimmed, hasMore, nextCursor := paginateClosedIssues(issues, 2)
	if len(trimmed) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(trimmed))
	}
	if !hasMore {
		t.Fatalf("expected hasMore to be true")
	}
	if nextCursor == "" {
		t.Fatalf("expected next cursor to be set")
	}

	cursorTime, cursorID, err := parseClosedCursor(nextCursor)
	if err != nil {
		t.Fatalf("expected cursor to parse, got err: %v", err)
	}
	if cursorID != "ui-2" {
		t.Fatalf("expected cursor id ui-2, got %s", cursorID)
	}
	expectedTime := trimmed[len(trimmed)-1].ClosedAt.UTC()
	if !cursorTime.Equal(expectedTime) {
		t.Fatalf("expected cursor time %v, got %v", expectedTime, cursorTime)
	}
}

func TestParseClosedCursor(t *testing.T) {
	now := time.Date(2025, 10, 29, 19, 0, 0, 0, time.UTC).UTC()
	cursor := now.Format(time.RFC3339Nano) + closedCursorSeparator + "ui-25"

	ts, id, err := parseClosedCursor(cursor)
	if err != nil {
		t.Fatalf("parseClosedCursor returned error: %v", err)
	}
	if id != "ui-25" {
		t.Fatalf("expected id ui-25, got %q", id)
	}
	if !ts.Equal(now) {
		t.Fatalf("expected timestamp %v, got %v", now, ts)
	}

	if _, _, err := parseClosedCursor(""); err == nil {
		t.Fatalf("expected error for empty cursor")
	}
	if _, _, err := parseClosedCursor("not-a-time|issue"); err == nil {
		t.Fatalf("expected error for invalid timestamp")
	}
	if _, _, err := parseClosedCursor(now.Format(time.RFC3339Nano)); err == nil {
		t.Fatalf("expected error for missing issue id")
	}
}
