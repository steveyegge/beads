package dolt

import (
	"strings"
	"testing"
)

// TestWidenTextColumns0033 verifies that migration 0033 raises the MySQL/Dolt
// TEXT cap (65,535 bytes) on notes/events/comments/wisp/snapshots tables to
// MEDIUMTEXT (16 MiB) by:
//  1. Asserting the column types are MEDIUMTEXT after schema init.
//  2. Round-tripping a >65,535-byte value through issues.notes and verifying
//     byte-equality on read (ensures no truncation).
//  3. Preserving NOT NULL on columns that carry it.
func TestWidenTextColumns0033(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx, cancel := testContext(t)
	defer cancel()

	// ---------- (1) Column-type assertions ----------
	// Each table -> column -> expected lowercase type prefix. We use HasPrefix
	// because information_schema.columns can return "mediumtext" or
	// "mediumtext collate ..." depending on the column's collation. Any
	// "text" prefix without "medium" would indicate the migration didn't run.
	wantCols := map[string]map[string]string{
		"issues":          {"notes": "mediumtext"},
		"events":          {"old_value": "mediumtext", "new_value": "mediumtext", "comment": "mediumtext"},
		"comments":        {"text": "mediumtext"},
		"wisps":           {"notes": "mediumtext"},
		"wisp_events":     {"old_value": "mediumtext", "new_value": "mediumtext", "comment": "mediumtext"},
		"wisp_comments":   {"text": "mediumtext"},
		"issue_snapshots": {"original_content": "mediumtext", "archived_events": "mediumtext"},
	}
	for table, cols := range wantCols {
		got := queryColumns(t, store, table)
		gotMap := make(map[string]string, len(got))
		for _, c := range got {
			gotMap[c.Name] = strings.ToLower(c.ColumnType)
		}
		for col, wantType := range cols {
			if !strings.HasPrefix(gotMap[col], wantType) {
				t.Errorf("%s.%s: got column type %q, want prefix %q (migration 0033 may not have run)",
					table, col, gotMap[col], wantType)
			}
		}
	}

	// ---------- (2) Byte-equality round-trip on issues.notes (>64KB) ----------
	const issueID = "test-widen-1"
	largeContent := strings.Repeat("A", 100_000) // 100 KB; well above the 65,535-byte TEXT cap.
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO issues (id, title, description, design, acceptance_criteria, notes)
		 VALUES (?, ?, '', '', '', ?)`,
		issueID, "Widen test", largeContent); err != nil {
		t.Fatalf("INSERT large notes failed (likely truncation cap of 65,535 bytes still in effect): %v", err)
	}
	var got string
	if err := store.db.QueryRowContext(ctx,
		`SELECT notes FROM issues WHERE id = ?`, issueID).Scan(&got); err != nil {
		t.Fatalf("SELECT notes failed: %v", err)
	}
	if len(got) != len(largeContent) {
		t.Errorf("notes round-trip length mismatch: len(got)=%d, len(want)=%d", len(got), len(largeContent))
	}
	if got != largeContent {
		t.Errorf("notes round-trip byte-mismatch (truncation or corruption)")
	}

	// ---------- (3) NOT NULL preservation ----------
	// issues.notes is declared MEDIUMTEXT NOT NULL post-0033. An INSERT that
	// omits notes (forcing NULL) MUST fail. We use a separate ID to avoid PK
	// collision with the round-trip row above.
	const nullCheckID = "test-widen-notnull"
	_, err := store.db.ExecContext(ctx,
		`INSERT INTO issues (id, title, description, design, acceptance_criteria, notes)
		 VALUES (?, ?, '', '', '', NULL)`,
		nullCheckID, "Null-check")
	if err == nil {
		t.Error("expected NOT NULL constraint violation on issues.notes = NULL, got nil error (NOT NULL preservation broken?)")
	}
}
