package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/types"
)

// issueColumns is the canonical SELECT column list for the issues / wisps
// tables. Order matches scanIssue. Kept constant so analyzers can verify it
// against the migration SQL.
const issueColumns = `
    id, content_hash, title, description, design, acceptance_criteria, notes,
    status, priority, issue_type, assignee, estimated_minutes,
    created_at, created_by, owner, updated_at, started_at, closed_at,
    external_ref, spec_id,
    compaction_level, compacted_at, compacted_at_commit, original_size,
    sender, ephemeral, no_history, wisp_type, pinned, is_template,
    mol_type, work_type, source_system, source_repo, close_reason,
    event_kind, actor, target, payload,
    await_type, await_id, timeout_ns, waiters,
    due_at, defer_until, metadata
`

// issueInsertColumns is the INSERT-time column list. Same order as scanIssue
// so positional binding stays in sync.
const issueInsertColumns = issueColumns

// scanArgs returns destination pointers in the same order as issueColumns.
// Nullable columns get sql.Null* shims that we fan out into the Issue struct
// after Scan.
type issueScanRow struct {
	id                 string
	contentHash        sql.NullString
	title              string
	description        string
	design             string
	acceptanceCriteria string
	notes              string
	status             string
	priority           int
	issueType          string
	assignee           sql.NullString
	estimatedMinutes   sql.NullInt32
	createdAt          time.Time
	createdBy          sql.NullString
	owner              sql.NullString
	updatedAt          time.Time
	startedAt          sql.NullTime
	closedAt           sql.NullTime
	externalRef        sql.NullString
	specID             sql.NullString
	compactionLevel    sql.NullInt32
	compactedAt        sql.NullTime
	compactedAtCommit  sql.NullString
	originalSize       sql.NullInt32
	sender             sql.NullString
	ephemeral          sql.NullBool
	noHistory          sql.NullBool
	wispType           sql.NullString
	pinned             sql.NullBool
	isTemplate         sql.NullBool
	molType            sql.NullString
	workType           sql.NullString
	sourceSystem       sql.NullString
	sourceRepo         sql.NullString
	closeReason        sql.NullString
	eventKind          sql.NullString
	actor              sql.NullString
	target             sql.NullString
	payload            sql.NullString
	awaitType          sql.NullString
	awaitID            sql.NullString
	timeoutNs          sql.NullInt64
	waiters            sql.NullString
	dueAt              sql.NullTime
	deferUntil         sql.NullTime
	metadata           []byte
}

func (r *issueScanRow) dest() []any {
	return []any{
		&r.id, &r.contentHash, &r.title, &r.description, &r.design, &r.acceptanceCriteria, &r.notes,
		&r.status, &r.priority, &r.issueType, &r.assignee, &r.estimatedMinutes,
		&r.createdAt, &r.createdBy, &r.owner, &r.updatedAt, &r.startedAt, &r.closedAt,
		&r.externalRef, &r.specID,
		&r.compactionLevel, &r.compactedAt, &r.compactedAtCommit, &r.originalSize,
		&r.sender, &r.ephemeral, &r.noHistory, &r.wispType, &r.pinned, &r.isTemplate,
		&r.molType, &r.workType, &r.sourceSystem, &r.sourceRepo, &r.closeReason,
		&r.eventKind, &r.actor, &r.target, &r.payload,
		&r.awaitType, &r.awaitID, &r.timeoutNs, &r.waiters,
		&r.dueAt, &r.deferUntil, &r.metadata,
	}
}

func (r *issueScanRow) toIssue() *types.Issue {
	issue := &types.Issue{
		ID:                 r.id,
		ContentHash:        r.contentHash.String,
		Title:              r.title,
		Description:        r.description,
		Design:             r.design,
		AcceptanceCriteria: r.acceptanceCriteria,
		Notes:              r.notes,
		Status:             types.Status(r.status),
		Priority:           r.priority,
		IssueType:          types.IssueType(r.issueType),
		Assignee:           r.assignee.String,
		CreatedAt:          r.createdAt,
		CreatedBy:          r.createdBy.String,
		Owner:              r.owner.String,
		UpdatedAt:          r.updatedAt,
		SpecID:             r.specID.String,
		Sender:             r.sender.String,
		Ephemeral:          r.ephemeral.Bool,
		NoHistory:          r.noHistory.Bool,
		WispType:           types.WispType(r.wispType.String),
		Pinned:             r.pinned.Bool,
		IsTemplate:         r.isTemplate.Bool,
		MolType:            types.MolType(r.molType.String),
		WorkType:           types.WorkType(r.workType.String),
		SourceSystem:       r.sourceSystem.String,
		SourceRepo:         r.sourceRepo.String,
		CloseReason:        r.closeReason.String,
		EventKind:          r.eventKind.String,
		Actor:              r.actor.String,
		Target:             r.target.String,
		Payload:            r.payload.String,
		AwaitType:          r.awaitType.String,
		AwaitID:            r.awaitID.String,
		Timeout:            time.Duration(r.timeoutNs.Int64),
	}
	if r.estimatedMinutes.Valid {
		v := int(r.estimatedMinutes.Int32)
		issue.EstimatedMinutes = &v
	}
	if r.startedAt.Valid {
		t := r.startedAt.Time
		issue.StartedAt = &t
	}
	if r.closedAt.Valid {
		t := r.closedAt.Time
		issue.ClosedAt = &t
	}
	if r.externalRef.Valid {
		s := r.externalRef.String
		issue.ExternalRef = &s
	}
	if r.compactionLevel.Valid {
		issue.CompactionLevel = int(r.compactionLevel.Int32)
	}
	if r.compactedAt.Valid {
		t := r.compactedAt.Time
		issue.CompactedAt = &t
	}
	if r.compactedAtCommit.Valid {
		s := r.compactedAtCommit.String
		issue.CompactedAtCommit = &s
	}
	if r.originalSize.Valid {
		issue.OriginalSize = int(r.originalSize.Int32)
	}
	if r.dueAt.Valid {
		t := r.dueAt.Time
		issue.DueAt = &t
	}
	if r.deferUntil.Valid {
		t := r.deferUntil.Time
		issue.DeferUntil = &t
	}
	if r.waiters.Valid && r.waiters.String != "" {
		var w []string
		if json.Valid([]byte(r.waiters.String)) {
			_ = json.Unmarshal([]byte(r.waiters.String), &w)
		}
		issue.Waiters = w
	}
	if len(r.metadata) > 0 && string(r.metadata) != "{}" {
		issue.Metadata = json.RawMessage(r.metadata)
	}
	return issue
}

// scanIssue reads a single issue row from a *pgx.Row.
func scanIssue(row pgx.Row) (*types.Issue, error) {
	var r issueScanRow
	if err := row.Scan(r.dest()...); err != nil {
		return nil, err
	}
	return r.toIssue(), nil
}

// scanIssues reads all rows of an issues query into a slice. Caller must
// already have run the Query call; the returned rows are closed here.
func scanIssues(rows pgx.Rows) ([]*types.Issue, error) {
	defer rows.Close()
	var issues []*types.Issue
	for rows.Next() {
		var r issueScanRow
		if err := rows.Scan(r.dest()...); err != nil {
			return nil, err
		}
		issues = append(issues, r.toIssue())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return issues, nil
}

// issueTableFor returns ("issues" / "wisps") and matching event tables based
// on the issue routing flags.
func issueTableFor(issue *types.Issue) (issueTable, eventTable string) {
	if issue.Ephemeral || issue.NoHistory {
		return "wisps", "wisp_events"
	}
	return "issues", "events"
}

// validIssueTables protects table-name interpolation against accidental
// dynamic-string drift; only these four tables are ever interpolated.
var validIssueTables = map[string]bool{
	"issues":            true,
	"wisps":             true,
	"events":            true,
	"wisp_events":       true,
	"labels":            true,
	"wisp_labels":       true,
	"comments":          true,
	"wisp_comments":     true,
	"dependencies":      true,
	"wisp_dependencies": true,
}

// guardTable panics on any table name not in the allowlist; callers using
// fmt.Sprintf to assemble SQL must run guarded names through this helper so
// the static-analysis vet rule has a clean signal that the input is bounded.
func guardTable(name string) string {
	if !validIssueTables[name] {
		panic(fmt.Sprintf("postgres: refused to interpolate unknown table %q", name))
	}
	return name
}

// chunkIDs splits a slice of IDs into chunks of at most n. Used by callers
// that batch ID-based lookups to avoid PG's 32k bound parameter cap.
func chunkIDs(ids []string, n int) [][]string {
	if n <= 0 || len(ids) <= n {
		return [][]string{ids}
	}
	var chunks [][]string
	for i := 0; i < len(ids); i += n {
		end := i + n
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[i:end])
	}
	return chunks
}

// joinPlaceholders returns `$1, $2, ..., $n` for n positional placeholders.
func joinPlaceholders(start, n int) string {
	if n == 0 {
		return ""
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(parts, ", ")
}
