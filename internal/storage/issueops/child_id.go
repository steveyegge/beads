package issueops

import (
	"context"
	"database/sql"
	"fmt"
)

// GetNextChildIDTx atomically generates the next child ID for a parent issue.
// The parent's home table (issues vs wisps) determines which counter table
// (child_counters vs wisp_child_counters) and which transaction is used.
// Reconciles the counter against existing children in the parent's home table
// to handle imports that bypass the counter.
//
// Returns the full child ID string (e.g., "parent-id.3").
func GetNextChildIDTx(ctx context.Context, regularTx, ignoredTx *sql.Tx, parentID string) (string, error) {
	tx, counterTable, issueTable := regularTx, "child_counters", "issues"
	if IsActiveWispInTx(ctx, ignoredTx, parentID) {
		tx, counterTable, issueTable = ignoredTx, "wisp_child_counters", "wisps"
	}

	var lastChild int
	//nolint:gosec // G201: counterTable is one of two hardcoded constants.
	err := tx.QueryRowContext(ctx,
		fmt.Sprintf("SELECT last_child FROM %s WHERE parent_id = ?", counterTable),
		parentID).Scan(&lastChild)
	if err == sql.ErrNoRows {
		lastChild = 0
	} else if err != nil {
		return "", fmt.Errorf("get next child ID: read counter: %w", err)
	}

	// Check existing children to prevent overwrites after JSONL import (GH#2166).
	// The counter may be stale if issues were imported without reconciling counters.
	//
	// We fetch direct child IDs and parse the numeric suffix in Go rather than
	// using SQL CAST(SUBSTRING_INDEX(...) AS UNSIGNED), which silently returns 0
	// for non-numeric ID suffixes (see GH#2721).
	//nolint:gosec // G201: issueTable is one of two hardcoded constants.
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT id FROM %s
		WHERE id LIKE CONCAT(?, '.%%')
		  AND id NOT LIKE CONCAT(?, '.%%.%%')
	`, issueTable), parentID, parentID)
	if err != nil {
		return "", fmt.Errorf("get next child ID: query existing children: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("get next child ID: scan child row: %w", err)
		}
		_, childNum, ok := ParseHierarchicalID(id)
		if ok && childNum > lastChild {
			lastChild = childNum
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("get next child ID: iterate children: %w", err)
	}

	nextChild := lastChild + 1

	//nolint:gosec // G201: counterTable is one of two hardcoded constants.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (parent_id, last_child) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE last_child = ?
	`, counterTable), parentID, nextChild, nextChild); err != nil {
		return "", fmt.Errorf("get next child ID: update counter: %w", err)
	}

	return fmt.Sprintf("%s.%d", parentID, nextChild), nil
}
