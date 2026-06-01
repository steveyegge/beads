package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

func SearchIssuesWithCountsInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter) ([]*types.IssueWithCounts, error) {
	limit := filter.Limit

	wispDepsExist, err := optionalTableExistsInTx(ctx, tx, "wisp_dependencies")
	if err != nil {
		return nil, fmt.Errorf("search issues with counts: wisp dependency probe: %w", err)
	}

	if filter.Ephemeral != nil && *filter.Ephemeral {
		empty, probeErr := wispsTableEmptyOrMissingInTx(ctx, tx)
		if probeErr != nil {
			return nil, fmt.Errorf("search issues with counts: ephemeral wisp probe: %w", probeErr)
		}
		if empty || !wispDepsExist {
			return nil, nil
		}
		wisps, err := runFilterSearchQueryInTx(ctx, tx, query, filter, WispsFilterTables, true)
		if err != nil {
			return nil, err
		}
		return finishSearchIssuesWithCounts(wisps, limit), nil
	}

	out, err := runFilterSearchQueryInTx(ctx, tx, query, filter, IssuesFilterTables, wispDepsExist)
	if err != nil {
		return nil, err
	}

	empty, probeErr := wispsTableEmptyOrMissingInTx(ctx, tx)
	if probeErr != nil {
		return nil, fmt.Errorf("search issues with counts: wisp probe: %w", probeErr)
	}
	if empty {
		return finishSearchIssuesWithCounts(out, limit), nil
	}
	if !wispDepsExist {
		return finishSearchIssuesWithCounts(out, limit), nil
	}

	wisps, err := runFilterSearchQueryInTx(ctx, tx, query, filter, WispsFilterTables, true)
	if err != nil {
		if isTableNotExistError(err) {
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

func runFilterSearchQueryInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter, tables FilterTables, includeWispReverseDeps bool) ([]*types.IssueWithCounts, error) {
	whereClauses, args, err := BuildIssueFilterClauses(query, filter, tables)
	if err != nil {
		return nil, err
	}
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + joinAnd(whereClauses)
	}
	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf("LIMIT %d", filter.Limit)
	}
	const orderBy = "ORDER BY i.priority ASC, i.created_at DESC, i.id ASC"
	return runSearchQueryInTx(ctx, tx, tables, whereSQL, orderBy, limitSQL, args, includeWispReverseDeps, filter.SkipLabels)
}

//nolint:gosec // G201: SQL fragments are caller-built from hardcoded shapes
func runSearchQueryInTx(ctx context.Context, tx *sql.Tx, tables FilterTables, whereSQL, orderBySQL, limitSQL string, args []interface{}, includeWispReverseDeps bool, skipLabels bool) ([]*types.IssueWithCounts, error) {
	// Reverse-blocker scan must consider every dep table when wisp source
	// edges are included; otherwise only issue-source tables.
	var reverseBlockerTables []string
	if includeWispReverseDeps {
		reverseBlockerTables = AllDepTables()
	} else {
		reverseBlockerTables = SourceDepTables(false)
	}
	reverseBlockerParts := make([]string, 0, len(reverseBlockerTables))
	for _, t := range reverseBlockerTables {
		col := DepTargetColumnForTable(t)
		reverseBlockerParts = append(reverseBlockerParts, fmt.Sprintf(`SELECT %s AS dep_id FROM %s WHERE type = 'blocks'`, col, t))
	}
	reverseBlockerSelect := strings.Join(reverseBlockerParts, " UNION ALL ")

	// Per-source subqueries (dep counts, parent_id, deps JSON) UNION across
	// the three source-routed dep tables for this filter's source class.
	depCountParts := make([]string, 0, len(tables.DepTables))
	parentParts := make([]string, 0, len(tables.DepTables))
	depJSONParts := make([]string, 0, len(tables.DepTables))
	for _, t := range tables.DepTables {
		col := DepTargetColumnForTable(t)
		depCountParts = append(depCountParts, fmt.Sprintf(`SELECT source_id FROM %s WHERE type = 'blocks'`, t))
		parentParts = append(parentParts, fmt.Sprintf(`SELECT source_id, %s AS parent_id FROM %s WHERE type = 'parent-child'`, col, t))
		depJSONParts = append(depJSONParts, fmt.Sprintf(`SELECT source_id, %s AS dep_json FROM %s`, readyWorkDepJSONObject(col), t))
	}
	depCountUnion := strings.Join(depCountParts, " UNION ALL ")
	parentUnion := strings.Join(parentParts, " UNION ALL ")
	depJSONUnion := strings.Join(depJSONParts, " UNION ALL ")

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
			SELECT source_id, COUNT(*) AS cnt
			FROM (%s) src_deps
			GROUP BY source_id
		) dc ON dc.source_id = i.id
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
			SELECT source_id, MIN(parent_id) AS parent_id
			FROM (%s) src_parents
			GROUP BY source_id
		) pc ON pc.source_id = i.id
		LEFT JOIN (
			SELECT source_id, JSON_ARRAYAGG(dep_json) AS deps_json
			FROM (%s) src_dep_rows
			GROUP BY source_id
		) d ON d.source_id = i.id
		%s
		%s
		%s
	`,
		readyWorkIssueColumns,
		labelsSelect,
		tables.Main,
		labelsJoin,
		depCountUnion,
		reverseBlockerSelect,
		tables.Comments,
		parentUnion,
		depJSONUnion,
		whereSQL,
		orderBySQL,
		limitSQL,
	)

	rows, err := tx.QueryContext(ctx, searchSQL, args...)
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

func joinAnd(clauses []string) string {
	switch len(clauses) {
	case 0:
		return ""
	case 1:
		return clauses[0]
	}
	total := 0
	for _, c := range clauses {
		total += len(c)
	}
	total += 5 * (len(clauses) - 1)
	buf := make([]byte, 0, total)
	for i, c := range clauses {
		if i > 0 {
			buf = append(buf, " AND "...)
		}
		buf = append(buf, c...)
	}
	return string(buf)
}
