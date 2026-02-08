package sqlite

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestMetadataIndex_CreateIssue(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	// Create an issue with metadata
	issue := &types.Issue{
		Title:     "test metadata indexing",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"category":"security","severity":"high","story_points":5}`),
	}

	if err := env.Store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify index rows were created
	rows, err := env.Store.db.QueryContext(ctx, `SELECT key, value_text, value_int FROM issue_metadata_index WHERE issue_id = ? ORDER BY key`, issue.ID)
	if err != nil {
		t.Fatalf("query index failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	type indexRow struct {
		Key       string
		ValueText sql.NullString
		ValueInt  sql.NullInt64
	}

	var got []indexRow
	for rows.Next() {
		var r indexRow
		if err := rows.Scan(&r.Key, &r.ValueText, &r.ValueInt); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 index rows, got %d: %+v", len(got), got)
	}

	// Check specific values
	byKey := map[string]indexRow{}
	for _, r := range got {
		byKey[r.Key] = r
	}

	if r, ok := byKey["category"]; !ok || !r.ValueText.Valid || r.ValueText.String != "security" {
		t.Errorf("category: expected text 'security', got %+v", byKey["category"])
	}
	if r, ok := byKey["severity"]; !ok || !r.ValueText.Valid || r.ValueText.String != "high" {
		t.Errorf("severity: expected text 'high', got %+v", byKey["severity"])
	}
	if r, ok := byKey["story_points"]; !ok || !r.ValueInt.Valid || r.ValueInt.Int64 != 5 {
		t.Errorf("story_points: expected int 5, got %+v", byKey["story_points"])
	}
}

func TestMetadataIndex_NestedKeys(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	// Create an issue with namespaced metadata (e.g., tracker sync data)
	issue := &types.Issue{
		Title:     "test nested metadata",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"jira":{"story_points":8,"sprint":"Sprint 24"},"local_key":"value"}`),
	}

	if err := env.Store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify nested keys are indexed with dot notation
	rows, err := env.Store.db.QueryContext(ctx, `SELECT key, value_text, value_int FROM issue_metadata_index WHERE issue_id = ? ORDER BY key`, issue.ID)
	if err != nil {
		t.Fatalf("query index failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	type indexRow struct {
		Key       string
		ValueText sql.NullString
		ValueInt  sql.NullInt64
	}

	byKey := map[string]indexRow{}
	for rows.Next() {
		var r indexRow
		if err := rows.Scan(&r.Key, &r.ValueText, &r.ValueInt); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		byKey[r.Key] = r
	}

	if r, ok := byKey["jira.story_points"]; !ok || !r.ValueInt.Valid || r.ValueInt.Int64 != 8 {
		t.Errorf("jira.story_points: expected int 8, got %+v", byKey["jira.story_points"])
	}
	if r, ok := byKey["jira.sprint"]; !ok || !r.ValueText.Valid || r.ValueText.String != "Sprint 24" {
		t.Errorf("jira.sprint: expected text 'Sprint 24', got %+v", byKey["jira.sprint"])
	}
	if r, ok := byKey["local_key"]; !ok || !r.ValueText.Valid || r.ValueText.String != "value" {
		t.Errorf("local_key: expected text 'value', got %+v", byKey["local_key"])
	}
}

func TestMetadataIndex_UpdateIssue(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	// Create issue with initial metadata
	issue := &types.Issue{
		Title:     "test metadata update",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"category":"bug","priority_tag":"low"}`),
	}

	if err := env.Store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Update metadata
	err := env.Store.UpdateIssue(ctx, issue.ID, map[string]interface{}{
		"metadata": `{"category":"feature","new_field":"hello"}`,
	}, "test")
	if err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify index was refreshed (old keys removed, new keys added)
	rows, err := env.Store.db.QueryContext(ctx, `SELECT key, value_text FROM issue_metadata_index WHERE issue_id = ? ORDER BY key`, issue.ID)
	if err != nil {
		t.Fatalf("query index failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	byKey := map[string]string{}
	for rows.Next() {
		var key string
		var val sql.NullString
		if err := rows.Scan(&key, &val); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if val.Valid {
			byKey[key] = val.String
		}
	}

	if byKey["category"] != "feature" {
		t.Errorf("category: expected 'feature', got %q", byKey["category"])
	}
	if byKey["new_field"] != "hello" {
		t.Errorf("new_field: expected 'hello', got %q", byKey["new_field"])
	}
	if _, exists := byKey["priority_tag"]; exists {
		t.Error("priority_tag should have been removed from index after update")
	}
}

func TestMetadataIndex_EmptyMetadata(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	// Create issue with no metadata
	issue := &types.Issue{
		Title:     "no metadata",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	if err := env.Store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify no index rows
	var count int
	err := env.Store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issue_metadata_index WHERE issue_id = ?`, issue.ID).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 index rows for empty metadata, got %d", count)
	}
}

func TestMetadataIndex_BoolAndFloat(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	issue := &types.Issue{
		Title:     "test types",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"active":true,"score":3.14,"count":42}`),
	}

	if err := env.Store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	rows, err := env.Store.db.QueryContext(ctx, `SELECT key, value_text, value_int, value_real FROM issue_metadata_index WHERE issue_id = ? ORDER BY key`, issue.ID)
	if err != nil {
		t.Fatalf("query index failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	type indexRow struct {
		Key       string
		ValueText sql.NullString
		ValueInt  sql.NullInt64
		ValueReal sql.NullFloat64
	}

	byKey := map[string]indexRow{}
	for rows.Next() {
		var r indexRow
		if err := rows.Scan(&r.Key, &r.ValueText, &r.ValueInt, &r.ValueReal); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		byKey[r.Key] = r
	}

	// Bool true → int 1
	if r, ok := byKey["active"]; !ok || !r.ValueInt.Valid || r.ValueInt.Int64 != 1 {
		t.Errorf("active: expected int 1 (true), got %+v", byKey["active"])
	}
	// Float
	if r, ok := byKey["score"]; !ok || !r.ValueReal.Valid {
		t.Errorf("score: expected real value, got %+v", byKey["score"])
	} else if r.ValueReal.Float64 < 3.13 || r.ValueReal.Float64 > 3.15 {
		t.Errorf("score: expected ~3.14, got %f", r.ValueReal.Float64)
	}
	// Integer
	if r, ok := byKey["count"]; !ok || !r.ValueInt.Valid || r.ValueInt.Int64 != 42 {
		t.Errorf("count: expected int 42, got %+v", byKey["count"])
	}
}

func TestMetadataIndex_RebuildMetadataIndex(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	// Create issues with metadata
	issue1 := &types.Issue{
		Title:     "issue1",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"team":"platform"}`),
	}
	issue2 := &types.Issue{
		Title:     "issue2",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"team":"frontend","severity":"high"}`),
	}

	if err := env.Store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("CreateIssue 1 failed: %v", err)
	}
	if err := env.Store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("CreateIssue 2 failed: %v", err)
	}

	// Manually wipe the index
	if _, err := env.Store.db.ExecContext(ctx, `DELETE FROM issue_metadata_index`); err != nil {
		t.Fatalf("failed to wipe index: %v", err)
	}

	// Verify it's empty
	var count int
	if err := env.Store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issue_metadata_index`).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after wipe, got %d", count)
	}

	// Rebuild
	if err := env.Store.RebuildMetadataIndex(ctx); err != nil {
		t.Fatalf("RebuildMetadataIndex failed: %v", err)
	}

	// Verify index is repopulated
	if err := env.Store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issue_metadata_index`).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	// issue1: team → 1 row; issue2: team + severity → 2 rows → total 3
	if count != 3 {
		t.Errorf("expected 3 index rows after rebuild, got %d", count)
	}
}

func TestMetadataIndex_ImportDuplicate(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	// 1. Create an existing issue with metadata
	now := time.Now()
	issue := &types.Issue{
		Title:     "Original",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"value":"original"}`),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := env.Store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// 2. Simulate an import of the SAME issue with DIFFERENT metadata.
	// insertIssues uses INSERT OR IGNORE, so the issues table keeps the old row,
	// but a buggy implementation would update the index with the stale import data.
	importIssue := *issue
	importIssue.Metadata = json.RawMessage(`{"value":"stale_import"}`)

	conn, err := env.Store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("Failed to get conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := insertIssues(ctx, conn, []*types.Issue{&importIssue}); err != nil {
		t.Fatalf("insertIssues failed: %v", err)
	}

	// 3. Verify the index was NOT updated with the stale import data.
	// The issue already existed, so INSERT OR IGNORE did nothing.
	// The index must match the DB ("original"), not the ignored import ("stale_import").
	var value string
	err = env.Store.db.QueryRowContext(ctx,
		`SELECT value_text FROM issue_metadata_index WHERE issue_id = ? AND key = 'value'`,
		issue.ID).Scan(&value)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if value != "original" {
		t.Errorf("Index drift detected! Expected 'original', got %q. "+
			"The index was updated even though the issue insert was ignored.", value)
	}
}

func TestMetadataIndex_DeleteCascade(t *testing.T) {
	env := newTestEnv(t)
	ctx := env.Ctx

	issue := &types.Issue{
		Title:     "To be deleted",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"ghost":"buster"}`),
	}

	if err := env.Store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify index exists
	var count int
	if err := env.Store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issue_metadata_index WHERE issue_id = ?`,
		issue.ID).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count == 0 {
		t.Fatal("Setup failed: index not created")
	}

	// Delete the issue — ON DELETE CASCADE should clean up the index
	if err := env.Store.DeleteIssue(ctx, issue.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	// Verify index rows are gone
	if err := env.Store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issue_metadata_index WHERE issue_id = ?`,
		issue.ID).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("FK CASCADE failed! Expected 0 index rows after delete, got %d. Ghost data remains.", count)
	}
}
