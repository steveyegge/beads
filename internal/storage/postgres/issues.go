package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// CreateIssue inserts a single issue (and its events row) atomically.
func (s *PostgresStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	if issue == nil {
		return errors.New("postgres: CreateIssue: issue must not be nil")
	}
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		return tx.CreateIssue(ctx, issue, actor)
	})
}

// CreateIssues is a thin wrapper over CreateIssuesWithFullOptions that uses
// the legacy "allow orphans, validate prefix" defaults.
func (s *PostgresStore) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return s.CreateIssuesWithFullOptions(ctx, issues, actor, storage.BatchCreateOptions{
		OrphanHandling:       storage.OrphanAllow,
		SkipPrefixValidation: false,
	})
}

// CreateIssuesWithFullOptions creates a batch of issues atomically.
func (s *PostgresStore) CreateIssuesWithFullOptions(ctx context.Context, issues []*types.Issue, actor string, opts storage.BatchCreateOptions) error {
	if len(issues) == 0 {
		return nil
	}
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		ptx := tx.(*pgxTransaction)
		ptx.opts = opts
		for _, issue := range issues {
			if err := ptx.CreateIssue(ctx, issue, actor); err != nil {
				return err
			}
		}
		return nil
	})
}

// insertIssueRow performs the INSERT itself. Used both by single-issue create
// and the batch path.
func insertIssueRow(ctx context.Context, c pgxConn, table string, issue *types.Issue) error {
	table = guardTable(table)
	//nolint:gosec // table is allowlisted via guardTable
	stmt := fmt.Sprintf(`
		INSERT INTO %s (
			id, content_hash, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, created_by, owner, updated_at, started_at, closed_at, external_ref, spec_id,
			compaction_level, compacted_at, compacted_at_commit, original_size,
			sender, ephemeral, no_history, wisp_type, pinned, is_template,
			mol_type, work_type, source_system, source_repo, close_reason,
			event_kind, actor, target, payload,
			await_type, await_id, timeout_ns, waiters,
			due_at, defer_until, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24,
			$25, $26, $27, $28, $29, $30,
			$31, $32, $33, $34, $35,
			$36, $37, $38, $39,
			$40, $41, $42, $43,
			$44, $45, $46
		)
		ON CONFLICT (id) DO UPDATE SET
			content_hash = EXCLUDED.content_hash,
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			design = EXCLUDED.design,
			acceptance_criteria = EXCLUDED.acceptance_criteria,
			notes = EXCLUDED.notes,
			status = EXCLUDED.status,
			priority = EXCLUDED.priority,
			issue_type = EXCLUDED.issue_type,
			assignee = EXCLUDED.assignee,
			estimated_minutes = EXCLUDED.estimated_minutes,
			updated_at = EXCLUDED.updated_at,
			started_at = EXCLUDED.started_at,
			closed_at = EXCLUDED.closed_at,
			external_ref = EXCLUDED.external_ref,
			source_repo = EXCLUDED.source_repo,
			close_reason = EXCLUDED.close_reason,
			metadata = EXCLUDED.metadata
	`, table)

	waitersJSON := ""
	if len(issue.Waiters) > 0 {
		if data, err := json.Marshal(issue.Waiters); err == nil {
			waitersJSON = string(data)
		}
	}

	_, err := c.Exec(ctx, stmt,
		issue.ID, nullString(issue.ContentHash), issue.Title, issue.Description, issue.Design, issue.AcceptanceCriteria, issue.Notes,
		string(issue.Status), issue.Priority, string(issue.IssueType), nullString(issue.Assignee), nullInt(issue.EstimatedMinutes),
		issue.CreatedAt, nullString(issue.CreatedBy), nullString(issue.Owner), issue.UpdatedAt, issue.StartedAt, issue.ClosedAt,
		nullStringPtr(issue.ExternalRef), nullString(issue.SpecID),
		issue.CompactionLevel, issue.CompactedAt, nullStringPtr(issue.CompactedAtCommit), nullIntVal(issue.OriginalSize),
		nullString(issue.Sender), issue.Ephemeral, issue.NoHistory, nullString(string(issue.WispType)), issue.Pinned, issue.IsTemplate,
		nullString(string(issue.MolType)), nullString(string(issue.WorkType)), nullString(issue.SourceSystem), nullString(issue.SourceRepo), nullString(issue.CloseReason),
		nullString(issue.EventKind), nullString(issue.Actor), nullString(issue.Target), nullString(issue.Payload),
		nullString(issue.AwaitType), nullString(issue.AwaitID), issue.Timeout.Nanoseconds(), nullString(waitersJSON),
		issue.DueAt, issue.DeferUntil, jsonbMetadata(issue.Metadata),
	)
	if err != nil {
		return wrapErr(fmt.Sprintf("insert into %s", table), err)
	}
	return nil
}

// recordEvent appends an audit-trail row from inside a transaction.
func recordEvent(ctx context.Context, c pgxConn, eventTable, issueID string, kind types.EventType, actor, oldVal, newVal string) error {
	eventTable = guardTable(eventTable)
	//nolint:gosec // table is allowlisted via guardTable
	stmt := fmt.Sprintf(`
		INSERT INTO %s (issue_id, event_type, actor, old_value, new_value)
		VALUES ($1, $2, $3, $4, $5)
	`, eventTable)
	_, err := c.Exec(ctx, stmt, issueID, string(kind), actor, oldVal, newVal)
	if err != nil {
		return wrapErr(fmt.Sprintf("record event in %s", eventTable), err)
	}
	return nil
}

// GetIssue returns a single issue by ID. Returns storage.ErrNotFound (wrapped)
// if the row does not exist in either the issues or wisps table.
func (s *PostgresStore) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	issue, err := getIssueFrom(ctx, s.pool, "issues", id)
	if err == nil {
		issue.Labels, _ = getLabelsFromTable(ctx, s.pool, "labels", id)
		return issue, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	wisp, werr := getIssueFrom(ctx, s.pool, "wisps", id)
	if werr == nil {
		wisp.Labels, _ = getLabelsFromTable(ctx, s.pool, "wisp_labels", id)
		return wisp, nil
	}
	return nil, err
}

func getIssueFrom(ctx context.Context, c pgxConn, table, id string) (*types.Issue, error) {
	table = guardTable(table)
	//nolint:gosec // table allowlisted
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE id = $1`, issueColumns, table)
	issue, err := scanIssue(c.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, id)
		}
		return nil, wrapErr(fmt.Sprintf("get issue from %s", table), err)
	}
	return issue, nil
}

// GetIssueByExternalRef looks up an issue by external_ref. Searches the
// issues table only; ephemeral issues do not carry stable external refs.
func (s *PostgresStore) GetIssueByExternalRef(ctx context.Context, externalRef string) (*types.Issue, error) {
	q := fmt.Sprintf(`SELECT %s FROM issues WHERE external_ref = $1`, issueColumns)
	issue, err := scanIssue(s.pool.QueryRow(ctx, q, externalRef))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: external_ref=%s", storage.ErrNotFound, externalRef)
		}
		return nil, wrapErr("get issue by external ref", err)
	}
	issue.Labels, _ = getLabelsFromTable(ctx, s.pool, "labels", issue.ID)
	return issue, nil
}

// GetIssuesByIDs returns all issues with IDs in the given slice. Missing IDs
// are silently dropped; callers compare returned IDs to the input set.
func (s *PostgresStore) GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := joinPlaceholders(1, len(ids))
	//nolint:gosec // placeholders are bound parameters, not user input
	q := fmt.Sprintf(`SELECT %s FROM issues WHERE id IN (%s)`, issueColumns, placeholders)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("get issues by ids", err)
	}
	issues, err := scanIssues(rows)
	if err != nil {
		return nil, wrapErr("scan issues by ids", err)
	}
	if len(issues) == 0 {
		// Try wisps as a fallback (matches Dolt behavior for mixed lookups).
		//nolint:gosec // placeholders are bound parameters
		q2 := fmt.Sprintf(`SELECT %s FROM wisps WHERE id IN (%s)`, issueColumns, placeholders)
		wrows, werr := s.pool.Query(ctx, q2, args...)
		if werr != nil {
			return nil, wrapErr("get wisps by ids", werr)
		}
		issues, err = scanIssues(wrows)
		if err != nil {
			return nil, wrapErr("scan wisps by ids", err)
		}
	}
	return issues, nil
}

// UpdateIssue applies a map of column updates and emits an event row.
// Only the fields present in updates are touched.
func (s *PostgresStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		return tx.UpdateIssue(ctx, id, updates, actor)
	})
}

// updateIssueRow runs a partial UPDATE inside a transaction.
func updateIssueRow(ctx context.Context, c pgxConn, table, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	table = guardTable(table)
	cols := make([]string, 0, len(updates))
	args := make([]any, 0, len(updates)+1)
	for k, v := range updates {
		if !validUpdateColumn(k) {
			return fmt.Errorf("postgres: refused to update unknown column %q", k)
		}
		args = append(args, v)
		cols = append(cols, fmt.Sprintf("%s = $%d", k, len(args)))
	}
	args = append(args, id)
	//nolint:gosec // column names allowlisted
	stmt := fmt.Sprintf(`UPDATE %s SET %s WHERE id = $%d`, table, strings.Join(cols, ", "), len(args))
	tag, err := c.Exec(ctx, stmt, args...)
	if err != nil {
		return wrapErr(fmt.Sprintf("update %s", table), err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, id)
	}
	return nil
}

// validUpdateColumn lists the columns that UpdateIssue may touch directly.
// Anything else is rejected so updates cannot smuggle in unsanitized
// identifiers via the map keys.
func validUpdateColumn(name string) bool {
	switch name {
	case "title", "description", "design", "acceptance_criteria", "notes",
		"status", "priority", "issue_type", "assignee", "estimated_minutes",
		"owner", "updated_at", "started_at", "closed_at", "external_ref",
		"spec_id", "source_repo", "close_reason", "due_at", "defer_until",
		"metadata", "ephemeral", "no_history", "wisp_type", "pinned",
		"is_template", "mol_type", "work_type", "source_system",
		"event_kind", "actor", "target", "payload",
		"await_type", "await_id", "timeout_ns", "waiters",
		"closed_by_session", "content_hash":
		return true
	}
	return false
}

// ReopenIssue clears closed_at + close_reason and sets status=open.
func (s *PostgresStore) ReopenIssue(ctx context.Context, id, reason, actor string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		updates := map[string]interface{}{
			"status":       string(types.StatusOpen),
			"closed_at":    nil,
			"close_reason": "",
			"updated_at":   time.Now().UTC(),
		}
		return tx.UpdateIssue(ctx, id, updates, actor)
	})
}

// UpdateIssueType is a typed helper for changing issue_type without rebuilding
// a full updates map at the call site.
func (s *PostgresStore) UpdateIssueType(ctx context.Context, id, issueType, actor string) error {
	return s.UpdateIssue(ctx, id, map[string]interface{}{
		"issue_type": issueType,
		"updated_at": time.Now().UTC(),
	}, actor)
}

// CloseIssue sets status=closed and writes an event.
func (s *PostgresStore) CloseIssue(ctx context.Context, id, reason, actor, session string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		now := time.Now().UTC()
		updates := map[string]interface{}{
			"status":            string(types.StatusClosed),
			"closed_at":         now,
			"closed_by_session": session,
			"close_reason":      reason,
			"updated_at":        now,
		}
		return tx.UpdateIssue(ctx, id, updates, actor)
	})
}

// DeleteIssue removes the issue row and cascades dependent rows via FK.
func (s *PostgresStore) DeleteIssue(ctx context.Context, id string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		return tx.DeleteIssue(ctx, id)
	})
}

// pgGlobToLikePattern mirrors issueops.globToLikePattern: convert shell-style
// glob (* / ?) to a SQL LIKE pattern, escaping literal % / _ / | with '|' so
// they don't act as LIKE wildcards. Callers MUST emit the matching ESCAPE '|'
// clause. Mirrored locally to keep the postgres package free of a hard
// dependency on issueops.
func pgGlobToLikePattern(pattern string) string {
	var b strings.Builder
	b.Grow(len(pattern))
	for _, c := range pattern {
		switch c {
		case '%', '_', '|':
			b.WriteByte('|')
			b.WriteRune(c)
		case '*':
			b.WriteByte('%')
		case '?':
			b.WriteByte('_')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// searchTables identifies the (main, labels, dependencies) table tuple
// used by searchTable. The two instances cover the persistent issues path
// and the wisps path; SearchIssues calls searchTable once or twice
// depending on filter.Ephemeral. Mirrors Dolt's WispsFilterTables /
// IssuesFilterTables in internal/storage/issueops.
type searchTables struct {
	issues       string // "issues" or "wisps"
	labels       string // "labels" or "wisp_labels"
	dependencies string // "dependencies" or "wisp_dependencies"
}

var (
	issuesSearchTables = searchTables{issues: "issues", labels: "labels", dependencies: "dependencies"}
	wispsSearchTables  = searchTables{issues: "wisps", labels: "wisp_labels", dependencies: "wisp_dependencies"}
)

// SearchIssues runs a flexible filter query against the issues table and,
// when filter.Ephemeral is nil (default) or false, also against the wisps
// table — merging results so NoHistory beads (stored in wisps with
// ephemeral=0 per GH#3649 / GH#3659) survive the non-ephemeral guard.
// Mirrors Dolt's SearchIssuesInTx at internal/storage/issueops/search.go:40-56
// (be-2clc). The clause builder lives in searchTable; this wrapper handles
// table routing and merge-by-ID.
func (s *PostgresStore) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	results, err := s.searchTable(ctx, query, filter, issuesSearchTables)
	if err != nil {
		return nil, err
	}

	if filter.Ephemeral == nil || !*filter.Ephemeral {
		wispResults, werr := s.searchTable(ctx, query, filter, wispsSearchTables)
		if werr != nil {
			return nil, werr
		}
		if len(wispResults) > 0 {
			seen := make(map[string]bool, len(results))
			for _, issue := range results {
				seen[issue.ID] = true
			}
			for _, w := range wispResults {
				if !seen[w.ID] {
					results = append(results, w)
				}
			}
		}
	}
	return results, nil
}

// searchTable runs SearchIssues' filter set against a single table tuple
// (issues+labels+dependencies, or wisps+wisp_labels+wisp_dependencies).
// All EXISTS subqueries are parameterized on tables.labels /
// tables.dependencies so the same builder works for both paths. The
// per-row "(ephemeral = FALSE OR ephemeral IS NULL)" guard applied when
// filter.Ephemeral is nil is what keeps NoHistory beads in the wisps
// merge while filtering out true ephemeral wisps.
func (s *PostgresStore) searchTable(ctx context.Context, query string, filter types.IssueFilter, tables searchTables) ([]*types.Issue, error) {
	mainTable := guardTable(tables.issues)
	labelsTable := guardTable(tables.labels)
	depsTable := guardTable(tables.dependencies)

	clauses := []string{}
	args := []any{}
	next := 1
	add := func(clause string, vals ...any) {
		clauses = append(clauses, clause)
		args = append(args, vals...)
		next += len(vals)
	}

	if query != "" {
		add(fmt.Sprintf("(title ILIKE $%d OR description ILIKE $%d)", next, next), "%"+query+"%")
	}
	if filter.Status != nil {
		add(fmt.Sprintf("status = $%d", next), string(*filter.Status))
	}
	if len(filter.Statuses) > 0 {
		ph := joinPlaceholders(next, len(filter.Statuses))
		statuses := make([]any, len(filter.Statuses))
		for i, st := range filter.Statuses {
			statuses[i] = string(st)
		}
		add(fmt.Sprintf("status IN (%s)", ph), statuses...)
	}
	if filter.Priority != nil {
		add(fmt.Sprintf("priority = $%d", next), *filter.Priority)
	}
	if filter.IssueType != nil {
		add(fmt.Sprintf("issue_type = $%d", next), string(*filter.IssueType))
	}
	if filter.Assignee != nil {
		add(fmt.Sprintf("assignee = $%d", next), *filter.Assignee)
	}
	if filter.IDPrefix != "" {
		add(fmt.Sprintf("id LIKE $%d", next), filter.IDPrefix+"%")
	}
	if len(filter.IDs) > 0 {
		ph := joinPlaceholders(next, len(filter.IDs))
		ids := make([]any, len(filter.IDs))
		for i, id := range filter.IDs {
			ids[i] = id
		}
		add(fmt.Sprintf("id IN (%s)", ph), ids...)
	}
	if len(filter.ExcludeStatus) > 0 {
		ph := joinPlaceholders(next, len(filter.ExcludeStatus))
		excl := make([]any, len(filter.ExcludeStatus))
		for i, st := range filter.ExcludeStatus {
			excl[i] = string(st)
		}
		add(fmt.Sprintf("status NOT IN (%s)", ph), excl...)
	}
	if filter.Ephemeral != nil {
		add(fmt.Sprintf("ephemeral = $%d", next), *filter.Ephemeral)
	} else {
		// default: drop true ephemeral wisps. On the issues table this is
		// a no-op (every row has ephemeral=false); on the wisps table this
		// is the guard that lets NoHistory beads (ephemeral=false) surface
		// while filtering out true ephemeral wisps (ephemeral=true).
		clauses = append(clauses, "(ephemeral = FALSE OR ephemeral IS NULL)")
	}

	// be-ucslk4: previously these were silently dropped — `bd list --label X`
	// and friends returned the full open queue against PG-backed cities.
	if filter.TitleContains != "" {
		add(fmt.Sprintf("title ILIKE $%d", next), "%"+filter.TitleContains+"%")
	}
	for _, label := range filter.Labels {
		add(fmt.Sprintf("EXISTS (SELECT 1 FROM %s l WHERE l.issue_id = %s.id AND l.label = $%d)", labelsTable, mainTable, next), label)
	}
	if len(filter.LabelsAny) > 0 {
		ph := joinPlaceholders(next, len(filter.LabelsAny))
		labels := make([]any, len(filter.LabelsAny))
		for i, l := range filter.LabelsAny {
			labels[i] = l
		}
		add(fmt.Sprintf("EXISTS (SELECT 1 FROM %s l WHERE l.issue_id = %s.id AND l.label IN (%s))", labelsTable, mainTable, ph), labels...)
	}
	for _, label := range filter.ExcludeLabels {
		add(fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s l WHERE l.issue_id = %s.id AND l.label = $%d)", labelsTable, mainTable, next), label)
	}
	if filter.LabelPattern != "" {
		add(fmt.Sprintf("EXISTS (SELECT 1 FROM %s l WHERE l.issue_id = %s.id AND l.label LIKE $%d ESCAPE '|')", labelsTable, mainTable, next), pgGlobToLikePattern(filter.LabelPattern))
	}
	if filter.LabelRegex != "" {
		add(fmt.Sprintf("EXISTS (SELECT 1 FROM %s l WHERE l.issue_id = %s.id AND l.label ~ $%d)", labelsTable, mainTable, next), filter.LabelRegex)
	}

	// be-jdeief: extend coverage to the rest of the canonical filter set.
	// The Dolt analog at internal/storage/issueops/filters.go is the
	// reference shape. PG idioms differ:
	//  - EXISTS over labels/dependencies subqueries (matches the be-ucslk4
	//    pattern in this function and queries.go::GetReadyWork)
	//  - ILIKE for case-insensitive substring (no LOWER() needed)
	//  - jsonb operators (`->>` for value lookup, `?` for key existence)
	//  - boolean comparison via TRUE/FALSE literals against BOOLEAN columns

	if filter.NoLabels {
		clauses = append(clauses, fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s l WHERE l.issue_id = %s.id)", labelsTable, mainTable))
	}

	if len(filter.ExcludeTypes) > 0 {
		ph := joinPlaceholders(next, len(filter.ExcludeTypes))
		excl := make([]any, len(filter.ExcludeTypes))
		for i, t := range filter.ExcludeTypes {
			excl[i] = string(t)
		}
		add(fmt.Sprintf("issue_type NOT IN (%s)", ph), excl...)
	}

	if filter.IsTemplate != nil {
		if *filter.IsTemplate {
			clauses = append(clauses, "is_template = TRUE")
		} else {
			clauses = append(clauses, "(is_template = FALSE OR is_template IS NULL)")
		}
	}

	if filter.Pinned != nil {
		if *filter.Pinned {
			clauses = append(clauses, "pinned = TRUE")
		} else {
			clauses = append(clauses, "(pinned = FALSE OR pinned IS NULL)")
		}
	}

	if len(filter.MetadataFields) > 0 {
		// AND semantics: every key=value pair must match. Sort for deterministic SQL.
		keys := make([]string, 0, len(filter.MetadataFields))
		for k := range filter.MetadataFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := storage.ValidateMetadataKey(k); err != nil {
				return nil, err
			}
			add(fmt.Sprintf("metadata->>%s = $%d", quoteJSONLit(k), next), filter.MetadataFields[k])
		}
	}
	if filter.HasMetadataKey != "" {
		if err := storage.ValidateMetadataKey(filter.HasMetadataKey); err != nil {
			return nil, err
		}
		add(fmt.Sprintf("metadata ? $%d", next), filter.HasMetadataKey)
	}

	if filter.ParentID != nil {
		add(fmt.Sprintf("EXISTS (SELECT 1 FROM %s d WHERE d.issue_id = %s.id AND d.type = 'parent-child' AND d.depends_on_id = $%d)", depsTable, mainTable, next), *filter.ParentID)
	}
	if filter.NoParent {
		clauses = append(clauses, fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s d WHERE d.issue_id = %s.id AND d.type = 'parent-child')", depsTable, mainTable))
	}

	if filter.MolType != nil {
		add(fmt.Sprintf("mol_type = $%d", next), string(*filter.MolType))
	}
	if filter.WispType != nil {
		add(fmt.Sprintf("wisp_type = $%d", next), string(*filter.WispType))
	}

	if filter.Deferred {
		// Mirrors Dolt: surface anything scheduled later — defer_until set OR explicit deferred status.
		add(fmt.Sprintf("(defer_until IS NOT NULL OR status = $%d)", next), string(types.StatusDeferred))
	}
	if filter.DeferAfter != nil {
		add(fmt.Sprintf("defer_until > $%d", next), filter.DeferAfter.UTC())
	}
	if filter.DeferBefore != nil {
		add(fmt.Sprintf("defer_until < $%d", next), filter.DeferBefore.UTC())
	}
	if filter.DueAfter != nil {
		add(fmt.Sprintf("due_at > $%d", next), filter.DueAfter.UTC())
	}
	if filter.DueBefore != nil {
		add(fmt.Sprintf("due_at < $%d", next), filter.DueBefore.UTC())
	}
	if filter.Overdue {
		add(fmt.Sprintf("(due_at IS NOT NULL AND due_at < NOW() AND status != $%d)", next), string(types.StatusClosed))
	}

	if filter.TitleSearch != "" {
		add(fmt.Sprintf("title ILIKE $%d", next), "%"+filter.TitleSearch+"%")
	}
	if filter.DescriptionContains != "" {
		add(fmt.Sprintf("description ILIKE $%d", next), "%"+filter.DescriptionContains+"%")
	}
	if filter.NotesContains != "" {
		add(fmt.Sprintf("notes ILIKE $%d", next), "%"+filter.NotesContains+"%")
	}
	if filter.ExternalRefContains != "" {
		add(fmt.Sprintf("external_ref ILIKE $%d", next), "%"+filter.ExternalRefContains+"%")
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	limit := ""
	if filter.Limit > 0 {
		limit = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	//nolint:gosec // mainTable/labelsTable/depsTable are allowlisted via guardTable; clauses use bound parameters
	q := fmt.Sprintf(`SELECT %s FROM %s%s ORDER BY created_at DESC%s`, issueColumns, mainTable, where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr(fmt.Sprintf("search %s", mainTable), err)
	}
	issues, err := scanIssues(rows)
	if err != nil {
		return nil, wrapErr(fmt.Sprintf("scan %s", mainTable), err)
	}
	for _, issue := range issues {
		issue.Labels, _ = getLabelsFromTable(ctx, s.pool, labelsTable, issue.ID)
	}
	return issues, nil
}
