package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/storage/dberrors"
	"github.com/steveyegge/beads/internal/types"
)

func (r *issueSQLRepositoryImpl) searchAcrossIssuesAndWispsWithCounts(ctx context.Context, query string, filter types.IssueFilter) ([]*types.IssueWithCounts, error) {
	limit := filter.Limit

	wispDepsExist, err := r.optionalTableExists(ctx, "wisp_issue_dependencies")
	if err != nil {
		return nil, fmt.Errorf("search issues with counts: wisp dependency probe: %w", err)
	}

	if filter.Ephemeral != nil && *filter.Ephemeral {
		empty, probeErr := r.wispsTableEmptyOrMissing(ctx)
		if probeErr != nil {
			return nil, fmt.Errorf("search issues with counts: ephemeral wisp probe: %w", probeErr)
		}
		if empty || !wispDepsExist {
			return nil, nil
		}
		wisps, err := r.runFilterSearchQuery(ctx, query, filter, wispsFilterTables, true)
		if err != nil {
			return nil, err
		}
		return finishSearchIssuesWithCounts(wisps, limit), nil
	}

	out, err := r.runFilterSearchQuery(ctx, query, filter, issuesFilterTables, wispDepsExist)
	if err != nil {
		return nil, err
	}

	if filter.SkipWisps {
		return finishSearchIssuesWithCounts(out, limit), nil
	}

	empty, probeErr := r.wispsTableEmptyOrMissing(ctx)
	if probeErr != nil {
		return nil, fmt.Errorf("search issues with counts: wisp probe: %w", probeErr)
	}
	if empty || !wispDepsExist {
		return finishSearchIssuesWithCounts(out, limit), nil
	}

	wisps, err := r.runFilterSearchQuery(ctx, query, filter, wispsFilterTables, true)
	if err != nil {
		if dberrors.IsTableNotExist(err) {
			return finishSearchIssuesWithCounts(out, limit), nil
		}
		return nil, err
	}
	if len(wisps) == 0 {
		return finishSearchIssuesWithCounts(out, limit), nil
	}

	seen := make(map[string]struct{}, len(out))
	for _, iwc := range out {
		if iwc != nil && iwc.Issue != nil {
			seen[iwc.Issue.ID] = struct{}{}
		}
	}
	for _, w := range wisps {
		if w == nil || w.Issue == nil {
			continue
		}
		if _, dup := seen[w.Issue.ID]; dup {
			return nil, fmt.Errorf("search issues with counts: id %q exists in both issues and wisps", w.Issue.ID)
		}
		out = append(out, w)
	}
	return finishSearchIssuesWithCounts(out, limit), nil
}

func (r *issueSQLRepositoryImpl) runFilterSearchQuery(ctx context.Context, query string, filter types.IssueFilter, tables filterTables, includeWispReverseDeps bool) ([]*types.IssueWithCounts, error) {
	whereClauses, args, err := buildIssueFilterClauses(query, filter, tables)
	if err != nil {
		return nil, err
	}
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}
	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf("LIMIT %d", filter.Limit)
	}
	const orderBy = "ORDER BY i.priority ASC, i.created_at DESC, i.id ASC"
	return r.runSearchQuery(ctx, tables, whereSQL, orderBy, limitSQL, args, includeWispReverseDeps, filter.SkipLabels)
}

//nolint:gosec // G201: SQL fragments are built from hardcoded table names and parameterized filters.
func (r *issueSQLRepositoryImpl) runSearchQuery(ctx context.Context, tables filterTables, whereSQL, orderBySQL, limitSQL string, args []any, includeWispReverseDeps bool, skipLabels bool) ([]*types.IssueWithCounts, error) {
	reverseBlockerSelect := reverseBlockerUnionSQL(includeWispReverseDeps)

	depUnion := depUnionForJSON(tables.DepTables)

	labelsSelect := "l.labels_json AS labels_json"
	labelsJoin := fmt.Sprintf(`
		LEFT JOIN (
			SELECT issue_id, JSON_ARRAYAGG(label) AS labels_json
			FROM %s
			GROUP BY issue_id
		) l ON l.issue_id = i.id`, tables.Labels)
	if skipLabels {
		labelsSelect = "NULL AS labels_json"
		labelsJoin = ""
	}

	searchSQL := fmt.Sprintf(`
		SELECT %s,
			%s,
			COALESCE(dc.cnt, 0) AS dep_count,
			COALESCE(rc.cnt, 0) AS rdep_count,
			COALESCE(cc.cnt, 0) AS comment_count,
			pc.parent_id     AS parent_id,
			d.deps_json      AS deps_json
		FROM %s i
		%s
		LEFT JOIN (
			SELECT issue_id, COUNT(*) AS cnt
			FROM %s u
			WHERE type = 'blocks'
			GROUP BY issue_id
		) dc ON dc.issue_id = i.id
		LEFT JOIN (
			SELECT dep_id, COUNT(*) AS cnt FROM (
				%s
			) all_blockers GROUP BY dep_id
		) rc ON rc.dep_id = i.id
		LEFT JOIN (
			SELECT issue_id, COUNT(*) AS cnt
			FROM %s
			GROUP BY issue_id
		) cc ON cc.issue_id = i.id
		LEFT JOIN (
			SELECT issue_id, MIN(depends_on_id) AS parent_id
			FROM %s u
			WHERE type = 'parent-child'
			GROUP BY issue_id
		) pc ON pc.issue_id = i.id
		LEFT JOIN (
			SELECT issue_id, JSON_ARRAYAGG(%s) AS deps_json
			FROM %s u
			GROUP BY issue_id
		) d ON d.issue_id = i.id
		%s
		%s
		%s
	`,
		readyWorkIssueColumns,
		labelsSelect,
		tables.Main,
		labelsJoin,
		depUnion,
		reverseBlockerSelect,
		tables.Comments,
		depUnion,
		readyWorkDepJSONObject,
		depUnion,
		whereSQL,
		orderBySQL,
		limitSQL,
	)

	rows, err := r.runner.QueryContext(ctx, searchSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("search count %s: %w", tables.Main, err)
	}
	defer func() { _ = rows.Close() }()

	var out []*types.IssueWithCounts
	seen := make(map[string]bool)
	for rows.Next() {
		iwc, scanErr := scanReadyWorkRowWithCounts(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if iwc == nil || iwc.Issue == nil {
			continue
		}
		if seen[iwc.Issue.ID] {
			continue
		}
		seen[iwc.Issue.ID] = true
		out = append(out, iwc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search count %s: rows: %w", tables.Main, err)
	}
	return out, nil
}

func (r *issueSQLRepositoryImpl) optionalTableExists(ctx context.Context, table string) (bool, error) {
	var probe int
	//nolint:gosec // G201: table is a hardcoded constant from caller (issues, wisps, dependencies, wisp_dependencies, ...).
	err := r.runner.QueryRowContext(ctx, fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", table)).Scan(&probe)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return true, nil
	case dberrors.IsTableNotExist(err):
		return false, nil
	default:
		return false, err
	}
}

var readyWorkIssueColumns = func() string {
	raw := strings.ReplaceAll(issueSelectColumns, "\n", " ")
	raw = strings.ReplaceAll(raw, "\t", " ")
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		parts[i] = "i." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}()

const readyWorkDepJSONObject = `JSON_OBJECT(
	'issue_id', issue_id,
	'depends_on_id', depends_on_id,
	'type', type,
	'created_at', DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'),
	'created_by', created_by,
	'metadata', CAST(metadata AS CHAR),
	'thread_id', thread_id
)`

// depUnionForJSON returns a UNION ALL subquery over the given split dep tables
// projecting (issue_id, depends_on_id, type, created_at, created_by, metadata,
// thread_id) where issue_id is source_id and depends_on_id is the table's
// typed target column. Wrap the result as `(<query>) u` in the caller's FROM.
func depUnionForJSON(depTables []string) string {
	parts := make([]string, 0, len(depTables))
	for _, t := range depTables {
		col := depTargetColumnForTable(t)
		parts = append(parts, fmt.Sprintf(
			"SELECT source_id AS issue_id, %s AS depends_on_id, type, created_at, created_by, metadata, thread_id FROM %s",
			col, t))
	}
	return "(" + strings.Join(parts, " UNION ALL ") + ")"
}

// reverseBlockerUnionSQL returns a UNION ALL subquery projecting `dep_id`
// (the target column) across the issue-target and wisp-target split dep
// tables filtered by type='blocks'. Wisp-source tables are included only when
// includeWisps is true. External-target tables are excluded because external
// IDs never match a local issue/wisp id in the outer LEFT JOIN.
func reverseBlockerUnionSQL(includeWisps bool) string {
	pairs := []struct{ table, col string }{
		{"issue_issue_dependencies", "depends_on_issue_id"},
		{"issue_wisp_dependencies", "depends_on_wisp_id"},
	}
	if includeWisps {
		pairs = append(pairs,
			struct{ table, col string }{"wisp_issue_dependencies", "depends_on_issue_id"},
			struct{ table, col string }{"wisp_wisp_dependencies", "depends_on_wisp_id"},
		)
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf(
			"SELECT %s AS dep_id FROM %s WHERE type = 'blocks'",
			p.col, p.table))
	}
	return strings.Join(parts, " UNION ALL ")
}

func scanReadyWorkRowWithCounts(rows *sql.Rows) (*types.IssueWithCounts, error) {
	var labelsJSON, depsJSON sql.NullString
	var parentID sql.NullString
	var depCount, rdepCount, commentCount sql.NullInt64

	composite := &compositeReadyRow{
		row: rows,
		extra: []any{
			&labelsJSON,
			&depCount,
			&rdepCount,
			&commentCount,
			&parentID,
			&depsJSON,
		},
	}
	issue, err := scanIssue(composite)
	if err != nil {
		return nil, fmt.Errorf("scan issue with counts: %w", err)
	}

	if labelsJSON.Valid && labelsJSON.String != "" {
		var labels []string
		if err := json.Unmarshal([]byte(labelsJSON.String), &labels); err != nil {
			return nil, fmt.Errorf("scan issue with counts: parse labels_json: %w", err)
		}
		sort.Strings(labels)
		issue.Labels = labels
	}

	if depsJSON.Valid && depsJSON.String != "" {
		var deps []*types.Dependency
		if err := json.Unmarshal([]byte(depsJSON.String), &deps); err != nil {
			return nil, fmt.Errorf("scan issue with counts: parse deps_json: %w", err)
		}
		issue.Dependencies = deps
	}

	iwc := &types.IssueWithCounts{
		Issue:           issue,
		DependencyCount: int(depCount.Int64),
		DependentCount:  int(rdepCount.Int64),
		CommentCount:    int(commentCount.Int64),
	}
	if parentID.Valid {
		s := parentID.String
		iwc.Parent = &s
	}
	return iwc, nil
}

type compositeReadyRow struct {
	row   *sql.Rows
	extra []any
}

func (c *compositeReadyRow) Scan(dest ...any) error {
	combined := make([]any, 0, len(dest)+len(c.extra))
	combined = append(combined, dest...)
	combined = append(combined, c.extra...)
	return c.row.Scan(combined...)
}

func finishSearchIssuesWithCounts(items []*types.IssueWithCounts, limit int) []*types.IssueWithCounts {
	sortSearchIssuesWithCounts(items)
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func sortSearchIssuesWithCounts(items []*types.IssueWithCounts) {
	if len(items) <= 1 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a == nil || a.Issue == nil {
			return false
		}
		if b == nil || b.Issue == nil {
			return true
		}
		if a.Issue.Priority != b.Issue.Priority {
			return a.Issue.Priority < b.Issue.Priority
		}
		if !a.Issue.CreatedAt.Equal(b.Issue.CreatedAt) {
			return a.Issue.CreatedAt.After(b.Issue.CreatedAt)
		}
		return a.Issue.ID < b.Issue.ID
	})
}
