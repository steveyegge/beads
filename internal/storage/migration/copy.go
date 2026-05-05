package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// copyAllTables drives the full Dolt → Postgres copy inside the caller's
// transaction. Returns per-table row counts on success; aborts the
// transaction on any error.
func copyAllTables(ctx context.Context, srcDB *sql.DB, tx pgx.Tx) (map[string]int, error) {
	out := make(map[string]int, len(allMigratedTables))
	for _, table := range allMigratedTables {
		n, err := copyTable(ctx, srcDB, tx, table)
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", table, err)
		}
		out[table] = n
	}
	return out, nil
}

// copyTable dispatches to the per-table copy implementation. Adding a new
// table means: (a) extend allMigratedTables in tables.go, (b) add a case
// here. Compile-time switch guarantees we never silently skip a registered
// table.
func copyTable(ctx context.Context, srcDB *sql.DB, tx pgx.Tx, table string) (int, error) {
	switch table {
	case "issues", "wisps":
		return copyIssueTable(ctx, srcDB, tx, table)
	case "dependencies", "wisp_dependencies":
		return copyDependencyTable(ctx, srcDB, tx, table)
	case "labels", "wisp_labels":
		return copyLabelTable(ctx, srcDB, tx, table)
	case "comments", "wisp_comments":
		return copyCommentTable(ctx, srcDB, tx, table)
	case "config", "metadata":
		return copyKVTable(ctx, srcDB, tx, table)
	case "custom_statuses":
		return copyCustomStatuses(ctx, srcDB, tx)
	case "custom_types":
		return copyCustomTypes(ctx, srcDB, tx)
	case "child_counters":
		return copyChildCounters(ctx, srcDB, tx)
	case "issue_counter":
		return copyIssueCounter(ctx, srcDB, tx)
	case "issue_snapshots":
		return copyIssueSnapshots(ctx, srcDB, tx)
	case "compaction_snapshots":
		return copyCompactionSnapshots(ctx, srcDB, tx)
	}
	return 0, fmt.Errorf("internal: no copy implementation for table %q", table)
}

// pgIssueColumns lists the issue columns in PG insert order (matches
// internal/storage/postgres/scan.go issueColumns). Kept here so the
// migration package does not import the postgres package.
var pgIssueColumns = []string{
	"id", "content_hash", "title", "description", "design", "acceptance_criteria", "notes",
	"status", "priority", "issue_type", "assignee", "estimated_minutes",
	"created_at", "created_by", "owner", "updated_at", "started_at", "closed_at",
	"external_ref", "spec_id",
	"compaction_level", "compacted_at", "compacted_at_commit", "original_size",
	"sender", "ephemeral", "no_history", "wisp_type", "pinned", "is_template",
	"mol_type", "work_type", "source_system", "source_repo", "close_reason",
	"event_kind", "actor", "target", "payload",
	"await_type", "await_id", "timeout_ns", "waiters",
	"due_at", "defer_until", "metadata",
}

// copyIssueTable streams issues (or wisps) from Dolt and CopyFroms into
// the same-named PG table. Uses ScanIssueFrom from issueops so column-list
// drift on the Dolt side stays the responsibility of one canonical scanner.
func copyIssueTable(ctx context.Context, srcDB *sql.DB, tx pgx.Tx, table string) (int, error) {
	//nolint:gosec // table comes from allMigratedTables via copyTable dispatch
	q := "SELECT " + issueops.IssueSelectColumns + " FROM " + identMySQL(table)
	rows, err := srcDB.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		issue, err := issueops.ScanIssueFrom(rows)
		if err != nil {
			return 0, fmt.Errorf("scan %s: %w", table, err)
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(issues) == 0 {
		return 0, nil
	}
	pgRows := make([][]any, len(issues))
	for i, issue := range issues {
		pgRows[i] = issueToPGRow(issue)
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, pgIssueColumns, pgx.CopyFromRows(pgRows))
	if err != nil {
		return 0, fmt.Errorf("copy %s: %w", table, err)
	}
	return int(n), nil
}

// issueToPGRow projects a hydrated *types.Issue into the slice pgx.CopyFrom
// expects, in pgIssueColumns order. Nullable fields use nil-or-value so
// PG receives explicit NULLs.
func issueToPGRow(issue *types.Issue) []any {
	var (
		startedAt         any = nullTime(issue.StartedAt)
		closedAt          any = nullTime(issue.ClosedAt)
		compactedAt       any = nullTime(issue.CompactedAt)
		dueAt             any = nullTime(issue.DueAt)
		deferUntil        any = nullTime(issue.DeferUntil)
		externalRef       any = nullStringPtr(issue.ExternalRef)
		compactedAtCommit any = nullStringPtr(issue.CompactedAtCommit)
		estMinutes        any
		origSize          any
	)
	if issue.EstimatedMinutes != nil {
		estMinutes = *issue.EstimatedMinutes
	}
	if issue.OriginalSize != 0 {
		origSize = issue.OriginalSize
	}

	waitersJSON := ""
	if len(issue.Waiters) > 0 {
		if data, err := json.Marshal(issue.Waiters); err == nil {
			waitersJSON = string(data)
		}
	}

	metadata := jsonbBytes(issue.Metadata)

	return []any{
		issue.ID,
		nullStringValue(issue.ContentHash),
		issue.Title, issue.Description, issue.Design, issue.AcceptanceCriteria, issue.Notes,
		string(issue.Status), issue.Priority, string(issue.IssueType),
		nullStringValue(issue.Assignee), estMinutes,
		issue.CreatedAt.UTC(), nullStringValue(issue.CreatedBy), nullStringValue(issue.Owner),
		issue.UpdatedAt.UTC(), startedAt, closedAt,
		externalRef, nullStringValue(issue.SpecID),
		issue.CompactionLevel, compactedAt, compactedAtCommit, origSize,
		nullStringValue(issue.Sender), issue.Ephemeral, issue.NoHistory,
		nullStringValue(string(issue.WispType)), issue.Pinned, issue.IsTemplate,
		nullStringValue(string(issue.MolType)), nullStringValue(string(issue.WorkType)),
		nullStringValue(issue.SourceSystem), nullStringValue(issue.SourceRepo),
		nullStringValue(issue.CloseReason),
		nullStringValue(issue.EventKind), nullStringValue(issue.Actor),
		nullStringValue(issue.Target), nullStringValue(issue.Payload),
		nullStringValue(issue.AwaitType), nullStringValue(issue.AwaitID),
		issue.Timeout.Nanoseconds(), nullStringValue(waitersJSON),
		dueAt, deferUntil, metadata,
	}
}

// pgDependencyColumns mirrors the PG dependencies table layout.
var pgDependencyColumns = []string{
	"issue_id", "depends_on_id", "type", "created_at", "created_by", "metadata", "thread_id",
}

func copyDependencyTable(ctx context.Context, srcDB *sql.DB, tx pgx.Tx, table string) (int, error) {
	// Dolt schema for dependencies/wisp_dependencies stores created_at as a
	// DATETIME — sql.NullTime scans it directly. metadata is JSON-text;
	// thread_id may be missing in older clones (NULL handled by sql.NullString).
	//nolint:gosec // table comes from allMigratedTables via copyTable dispatch
	q := "SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id FROM " + identMySQL(table)
	rows, err := srcDB.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var pgRows [][]any
	for rows.Next() {
		var (
			issueID, dependsOnID, depType string
			createdAt                     sql.NullTime
			createdBy                     sql.NullString
			metadata                      sql.NullString
			threadID                      sql.NullString
		)
		if err := rows.Scan(&issueID, &dependsOnID, &depType, &createdAt, &createdBy, &metadata, &threadID); err != nil {
			return 0, fmt.Errorf("scan %s: %w", table, err)
		}
		ts := time.Time{}
		if createdAt.Valid {
			ts = createdAt.Time.UTC()
		}
		pgRows = append(pgRows, []any{
			issueID, dependsOnID, depType, ts, createdBy.String,
			jsonbBytes([]byte(metadata.String)), threadID.String,
		})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, pgDependencyColumns, pgx.CopyFromRows(pgRows))
	if err != nil {
		return 0, fmt.Errorf("copy %s: %w", table, err)
	}
	return int(n), nil
}

func copyLabelTable(ctx context.Context, srcDB *sql.DB, tx pgx.Tx, table string) (int, error) {
	//nolint:gosec // table comes from allMigratedTables via copyTable dispatch
	q := "SELECT issue_id, label FROM " + identMySQL(table)
	rows, err := srcDB.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var pgRows [][]any
	for rows.Next() {
		var issueID, label string
		if err := rows.Scan(&issueID, &label); err != nil {
			return 0, fmt.Errorf("scan %s: %w", table, err)
		}
		pgRows = append(pgRows, []any{issueID, label})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, []string{"issue_id", "label"}, pgx.CopyFromRows(pgRows))
	if err != nil {
		return 0, fmt.Errorf("copy %s: %w", table, err)
	}
	return int(n), nil
}

func copyCommentTable(ctx context.Context, srcDB *sql.DB, tx pgx.Tx, table string) (int, error) {
	// Dolt comments.id is CHAR(36) UUID; PG comments.id is UUID. Pass the
	// string through and pgx will cast it.
	//nolint:gosec // table comes from allMigratedTables via copyTable dispatch
	q := "SELECT id, issue_id, author, text, created_at FROM " + identMySQL(table)
	rows, err := srcDB.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var pgRows [][]any
	for rows.Next() {
		var (
			id, issueID, author, text string
			createdAt                 sql.NullTime
		)
		if err := rows.Scan(&id, &issueID, &author, &text, &createdAt); err != nil {
			return 0, fmt.Errorf("scan %s: %w", table, err)
		}
		ts := time.Time{}
		if createdAt.Valid {
			ts = createdAt.Time.UTC()
		}
		pgRows = append(pgRows, []any{id, issueID, author, text, ts})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	cols := []string{"id", "issue_id", "author", "text", "created_at"}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(pgRows))
	if err != nil {
		return 0, fmt.Errorf("copy %s: %w", table, err)
	}
	return int(n), nil
}

// copyKVTable handles the small key/value tables (config, metadata). Use
// upsert because the destination's seeded defaults from migration 0001 may
// have left rows in place when --force was not specified for those tables.
func copyKVTable(ctx context.Context, srcDB *sql.DB, tx pgx.Tx, table string) (int, error) {
	//nolint:gosec // table comes from allMigratedTables via copyTable dispatch
	q := "SELECT `key`, value FROM " + identMySQL(table)
	rows, err := srcDB.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()
	var (
		count int
		//nolint:gosec // table comes from allMigratedTables via copyTable dispatch
		stmt = "INSERT INTO " + identPG(table) + " (key, value) VALUES ($1, $2) " +
			"ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value"
	)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return 0, fmt.Errorf("scan %s: %w", table, err)
		}
		if _, err := tx.Exec(ctx, stmt, k, v); err != nil {
			return 0, fmt.Errorf("insert %s row %q: %w", table, k, err)
		}
		count++
	}
	return count, rows.Err()
}

func copyCustomStatuses(ctx context.Context, srcDB *sql.DB, tx pgx.Tx) (int, error) {
	rows, err := srcDB.QueryContext(ctx, "SELECT name, category FROM custom_statuses")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var pgRows [][]any
	for rows.Next() {
		var name, category string
		if err := rows.Scan(&name, &category); err != nil {
			return 0, err
		}
		pgRows = append(pgRows, []any{name, category})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"custom_statuses"}, []string{"name", "category"}, pgx.CopyFromRows(pgRows))
	return int(n), err
}

func copyCustomTypes(ctx context.Context, srcDB *sql.DB, tx pgx.Tx) (int, error) {
	rows, err := srcDB.QueryContext(ctx, "SELECT name FROM custom_types")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var pgRows [][]any
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, err
		}
		pgRows = append(pgRows, []any{name})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"custom_types"}, []string{"name"}, pgx.CopyFromRows(pgRows))
	return int(n), err
}

func copyChildCounters(ctx context.Context, srcDB *sql.DB, tx pgx.Tx) (int, error) {
	rows, err := srcDB.QueryContext(ctx, "SELECT parent_id, last_child FROM child_counters")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var pgRows [][]any
	for rows.Next() {
		var parentID string
		var lastChild int
		if err := rows.Scan(&parentID, &lastChild); err != nil {
			return 0, err
		}
		pgRows = append(pgRows, []any{parentID, lastChild})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"child_counters"}, []string{"parent_id", "last_child"}, pgx.CopyFromRows(pgRows))
	return int(n), err
}

func copyIssueCounter(ctx context.Context, srcDB *sql.DB, tx pgx.Tx) (int, error) {
	rows, err := srcDB.QueryContext(ctx, "SELECT prefix, last_id FROM issue_counter")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int
	for rows.Next() {
		var prefix string
		var lastID int
		if err := rows.Scan(&prefix, &lastID); err != nil {
			return 0, err
		}
		// UPSERT — issue_counter table on PG starts empty after fresh init,
		// but a re-migration with --force produces a clean slate. This stays
		// idempotent for both paths.
		_, err = tx.Exec(ctx,
			`INSERT INTO issue_counter (prefix, last_id) VALUES ($1, $2)
			 ON CONFLICT (prefix) DO UPDATE SET last_id = EXCLUDED.last_id`,
			prefix, lastID)
		if err != nil {
			return 0, fmt.Errorf("insert issue_counter %q: %w", prefix, err)
		}
		count++
	}
	return count, rows.Err()
}

func copyIssueSnapshots(ctx context.Context, srcDB *sql.DB, tx pgx.Tx) (int, error) {
	q := `SELECT id, issue_id, snapshot_time, compaction_level, original_size,
	             compressed_size, original_content, archived_events
	      FROM issue_snapshots`
	rows, err := srcDB.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var pgRows [][]any
	for rows.Next() {
		var (
			id, issueID                             string
			snapshotTime                            sql.NullTime
			compLevel, originalSize, compressedSize int
			originalContent                         string
			archivedEvents                          sql.NullString
		)
		if err := rows.Scan(&id, &issueID, &snapshotTime, &compLevel, &originalSize,
			&compressedSize, &originalContent, &archivedEvents); err != nil {
			return 0, err
		}
		ts := time.Time{}
		if snapshotTime.Valid {
			ts = snapshotTime.Time.UTC()
		}
		var ae any
		if archivedEvents.Valid {
			ae = archivedEvents.String
		}
		pgRows = append(pgRows, []any{
			id, issueID, ts, compLevel, originalSize,
			compressedSize, originalContent, ae,
		})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	cols := []string{"id", "issue_id", "snapshot_time", "compaction_level",
		"original_size", "compressed_size", "original_content", "archived_events"}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"issue_snapshots"}, cols, pgx.CopyFromRows(pgRows))
	return int(n), err
}

func copyCompactionSnapshots(ctx context.Context, srcDB *sql.DB, tx pgx.Tx) (int, error) {
	q := `SELECT id, issue_id, compaction_level, snapshot_json, created_at FROM compaction_snapshots`
	rows, err := srcDB.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var pgRows [][]any
	for rows.Next() {
		var (
			id, issueID  string
			compLevel    int
			snapshotJSON []byte
			createdAt    sql.NullTime
		)
		if err := rows.Scan(&id, &issueID, &compLevel, &snapshotJSON, &createdAt); err != nil {
			return 0, err
		}
		ts := time.Time{}
		if createdAt.Valid {
			ts = createdAt.Time.UTC()
		}
		pgRows = append(pgRows, []any{id, issueID, compLevel, snapshotJSON, ts})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pgRows) == 0 {
		return 0, nil
	}
	cols := []string{"id", "issue_id", "compaction_level", "snapshot_json", "created_at"}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"compaction_snapshots"}, cols, pgx.CopyFromRows(pgRows))
	return int(n), err
}

// nullTime returns a *time.Time as either a UTC time.Time or nil for
// pgx.CopyFrom NULL handling.
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

// nullStringPtr returns the deref of a *string or nil for pgx NULL.
func nullStringPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// nullStringValue passes through a string, mapping empty to nil so PG
// receives explicit NULL for unset optional columns.
func nullStringValue(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// jsonbBytes prepares JSONB bytes for pgx; empty/whitespace becomes "{}".
func jsonbBytes(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}
