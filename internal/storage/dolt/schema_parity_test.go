//go:build cgo

package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// getColumnNames queries information_schema.columns for a table and returns
// sorted column names.
func getColumnNames(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(`
		SELECT COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`, table)
	if err != nil {
		return nil, fmt.Errorf("query information_schema for %s: %w", table, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, fmt.Errorf("scan column name for %s: %w", table, err)
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

// TestSchemaParityWispsVsIssues verifies that the wisps table has the exact
// same column set as the issues table. If a column is added to issues but not
// wisps (or vice versa), this test fails — catching schema drift at CI time.
func TestSchemaParityWispsVsIssues(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	issuesCols, err := getColumnNames(store.db, "issues")
	if err != nil {
		t.Fatalf("failed to get issues columns: %v", err)
	}
	wispsCols, err := getColumnNames(store.db, "wisps")
	if err != nil {
		t.Fatalf("failed to get wisps columns: %v", err)
	}

	if len(issuesCols) == 0 {
		t.Fatal("issues table has no columns — schema not initialized?")
	}
	if len(wispsCols) == 0 {
		t.Fatal("wisps table has no columns — migration 004 not run?")
	}

	// Build sets for comparison
	issuesSet := make(map[string]bool, len(issuesCols))
	for _, c := range issuesCols {
		issuesSet[c] = true
	}
	wispsSet := make(map[string]bool, len(wispsCols))
	for _, c := range wispsCols {
		wispsSet[c] = true
	}

	// Find columns in issues but not in wisps
	var missingFromWisps []string
	for _, c := range issuesCols {
		if !wispsSet[c] {
			missingFromWisps = append(missingFromWisps, c)
		}
	}

	// Find columns in wisps but not in issues
	var extraInWisps []string
	for _, c := range wispsCols {
		if !issuesSet[c] {
			extraInWisps = append(extraInWisps, c)
		}
	}

	if len(missingFromWisps) > 0 {
		t.Errorf("columns in issues but missing from wisps: %v\n"+
			"Add these columns to wispsTableSchema in migrations/004_wisps_table.go",
			missingFromWisps)
	}
	if len(extraInWisps) > 0 {
		t.Errorf("columns in wisps but missing from issues: %v\n"+
			"Add these columns to the issues schema in schema.go",
			extraInWisps)
	}

	if t.Failed() {
		t.Logf("issues columns (%d): %v", len(issuesCols), issuesCols)
		t.Logf("wisps columns  (%d): %v", len(wispsCols), wispsCols)
	}
}

// TestSchemaParityAuxiliaryTables verifies that wisp auxiliary tables have the
// same column sets as their corresponding main tables. This catches drift in
// labels, dependencies, events, and comments table pairs.
func TestSchemaParityAuxiliaryTables(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	pairs := []struct {
		main string
		wisp string
	}{
		{"labels", "wisp_labels"},
		{"dependencies", "wisp_dependencies"},
		{"events", "wisp_events"},
		{"comments", "wisp_comments"},
	}

	for _, pair := range pairs {
		t.Run(pair.main+"_vs_"+pair.wisp, func(t *testing.T) {
			mainCols, err := getColumnNames(store.db, pair.main)
			if err != nil {
				t.Fatalf("failed to get %s columns: %v", pair.main, err)
			}
			wispCols, err := getColumnNames(store.db, pair.wisp)
			if err != nil {
				t.Fatalf("failed to get %s columns: %v", pair.wisp, err)
			}

			if len(mainCols) == 0 {
				t.Fatalf("%s has no columns", pair.main)
			}
			if len(wispCols) == 0 {
				t.Fatalf("%s has no columns — migration 005 not run?", pair.wisp)
			}

			mainSet := make(map[string]bool, len(mainCols))
			for _, c := range mainCols {
				mainSet[c] = true
			}
			wispSet := make(map[string]bool, len(wispCols))
			for _, c := range wispCols {
				wispSet[c] = true
			}

			var missing []string
			for _, c := range mainCols {
				if !wispSet[c] {
					missing = append(missing, c)
				}
			}
			var extra []string
			for _, c := range wispCols {
				if !mainSet[c] {
					extra = append(extra, c)
				}
			}

			if len(missing) > 0 {
				t.Errorf("columns in %s but missing from %s: %v",
					pair.main, pair.wisp, missing)
			}
			if len(extra) > 0 {
				t.Errorf("columns in %s but missing from %s: %v",
					pair.wisp, pair.main, extra)
			}
		})
	}
}

// TestMigrations004And005Together verifies that migrations 004 (wisps table)
// and 005 (wisp auxiliary tables) run correctly in sequence on a fully
// initialized database — the same order they execute in production.
func TestMigrations004And005Together(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// setupTestStore already ran all migrations. Verify all expected tables exist.
	expectedTables := []string{
		"wisps",
		"wisp_labels",
		"wisp_dependencies",
		"wisp_events",
		"wisp_comments",
	}

	for _, table := range expectedTables {
		var count int
		err := store.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = DATABASE() AND table_name = ?
		`, table).Scan(&count)
		if err != nil {
			t.Fatalf("failed to check table %s: %v", table, err)
		}
		if count == 0 {
			t.Errorf("expected table %s to exist after migrations 004+005", table)
		}
	}

	// Verify dolt_ignore patterns are set (migration 004 prerequisite)
	var ignoreCount int
	err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM dolt_ignore WHERE pattern IN ('wisps', 'wisp_%')").Scan(&ignoreCount)
	if err != nil {
		t.Fatalf("failed to query dolt_ignore: %v", err)
	}
	if ignoreCount != 2 {
		t.Errorf("expected 2 dolt_ignore patterns, got %d", ignoreCount)
	}

	// Verify we can INSERT and round-trip data through the wisps table
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO wisps (id, title, description, design, acceptance_criteria, notes)
		VALUES ('wisp-parity-test', 'Parity Test', 'desc', '', '', '')
	`)
	if err != nil {
		t.Fatalf("failed to insert into wisps: %v", err)
	}

	// Verify wisp auxiliary tables accept data referencing the wisp
	_, err = store.db.ExecContext(ctx,
		"INSERT INTO wisp_labels (issue_id, label) VALUES ('wisp-parity-test', 'test-label')")
	if err != nil {
		t.Fatalf("failed to insert into wisp_labels: %v", err)
	}

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO wisp_dependencies (issue_id, depends_on_id)
		VALUES ('wisp-parity-test', 'some-dep')
	`)
	if err != nil {
		t.Fatalf("failed to insert into wisp_dependencies: %v", err)
	}

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO wisp_events (issue_id, event_type, actor)
		VALUES ('wisp-parity-test', 'created', 'test')
	`)
	if err != nil {
		t.Fatalf("failed to insert into wisp_events: %v", err)
	}

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO wisp_comments (issue_id, author, text)
		VALUES ('wisp-parity-test', 'test', 'A comment')
	`)
	if err != nil {
		t.Fatalf("failed to insert into wisp_comments: %v", err)
	}
}

// TestSchemaParityIndexes verifies that the wisps table has matching indexes
// for every index on the issues table (with appropriate name adjustments).
func TestSchemaParityIndexes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Get index columns for issues (excluding PRIMARY)
	issuesIdx, err := getIndexColumns(store.db, "issues")
	if err != nil {
		t.Fatalf("failed to get issues indexes: %v", err)
	}
	wispsIdx, err := getIndexColumns(store.db, "wisps")
	if err != nil {
		t.Fatalf("failed to get wisps indexes: %v", err)
	}

	// Normalize: strip table prefix from index names for comparison.
	// idx_issues_status -> status, idx_wisps_status -> status
	issuesNorm := normalizeIndexes(issuesIdx, "issues")
	wispsNorm := normalizeIndexes(wispsIdx, "wisps")

	// Compare normalized index sets
	for name, cols := range issuesNorm {
		wispCols, ok := wispsNorm[name]
		if !ok {
			t.Errorf("issues has index %q (columns: %v) but wisps does not", name, cols)
			continue
		}
		if strings.Join(cols, ",") != strings.Join(wispCols, ",") {
			t.Errorf("index %q columns differ: issues=%v wisps=%v", name, cols, wispCols)
		}
	}
	for name, cols := range wispsNorm {
		if _, ok := issuesNorm[name]; !ok {
			t.Errorf("wisps has index %q (columns: %v) but issues does not", name, cols)
		}
	}
}

// getIndexColumns returns a map of index_name -> []column_name for non-PRIMARY indexes.
func getIndexColumns(db *sql.DB, table string) (map[string][]string, error) {
	rows, err := db.Query(`
		SELECT INDEX_NAME, COLUMN_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME != 'PRIMARY'
		ORDER BY INDEX_NAME, SEQ_IN_INDEX
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var idxName, colName string
		if err := rows.Scan(&idxName, &colName); err != nil {
			return nil, err
		}
		result[idxName] = append(result[idxName], colName)
	}
	return result, rows.Err()
}

// normalizeIndexes strips the table-specific prefix from index names.
// "idx_issues_status" -> "status", "idx_wisps_status" -> "status"
func normalizeIndexes(indexes map[string][]string, table string) map[string][]string {
	prefix := "idx_" + table + "_"
	result := make(map[string][]string, len(indexes))
	for name, cols := range indexes {
		normalized := strings.TrimPrefix(name, prefix)
		sorted := make([]string, len(cols))
		copy(sorted, cols)
		sort.Strings(sorted)
		result[normalized] = sorted
	}
	return result
}
