package issueops

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// SearchIssuesWithCountsInTx is the single-statement equivalent of
// SearchIssuesInTx + per-issue hydration of labels / dependency records /
// dep/dependent/comment counts / parent ID, intended for the bd list --json
// path.
//
// Issues and wisps are each fetched via one mega-query against their own
// table set; results are merged Go-side. Cross-table reverse-dependency
// edges are reflected in rdep_count via a UNION ALL of both dep tables.
//
// When the wisps table is missing or empty (the common case on projects
// that never use wisps) one cheap probe replaces the entire wisp branch.
//
// No per-call hydration fallbacks: both sides use the same mega-query
// shape.
func SearchIssuesWithCountsInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter) ([]*types.IssueWithCounts, error) {
	// Ephemeral-only: only the wisps table should be scanned.
	if filter.Ephemeral != nil && *filter.Ephemeral {
		empty, probeErr := wispsTableEmptyOrMissingInTx(ctx, tx)
		if probeErr != nil {
			return nil, fmt.Errorf("search issues with counts: ephemeral wisp probe: %w", probeErr)
		}
		if empty {
			return nil, nil
		}
		return runFilterMegaQueryInTx(ctx, tx, query, filter, WispsFilterTables)
	}

	out, err := runFilterMegaQueryInTx(ctx, tx, query, filter, IssuesFilterTables)
	if err != nil {
		return nil, err
	}

	// Merge in wisps when not restricted to permanents-only-via-ephemeral=true.
	// Empty wisps table short-circuits the entire wisp branch (one probe).
	empty, probeErr := wispsTableEmptyOrMissingInTx(ctx, tx)
	if probeErr != nil {
		return nil, fmt.Errorf("search issues with counts: wisp probe: %w", probeErr)
	}
	if empty {
		return out, nil
	}

	wisps, err := runFilterMegaQueryInTx(ctx, tx, query, filter, WispsFilterTables)
	if err != nil {
		if isTableNotExistError(err) {
			return out, nil
		}
		return nil, err
	}
	if len(wisps) == 0 {
		return out, nil
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
	return out, nil
}

// runFilterMegaQueryInTx is the IssueFilter-driven entry point: it routes
// BuildIssueFilterClauses through the shared LEFT-JOIN mega-query against
// the specified table set.
func runFilterMegaQueryInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter, tables FilterTables) ([]*types.IssueWithCounts, error) {
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
	return runMegaQueryInTx(ctx, tx, tables, whereSQL, orderBy, limitSQL, args)
}

// runMegaQueryInTx is the shared single-statement entry point used by both
// the ready-work and the bd list --json paths. It builds the six LEFT-JOIN
// shape against the supplied table set, executes, and scans rows into
// []*types.IssueWithCounts.
//
// orderBySQL must already reference the "i." alias the mega-query uses for
// the outer table (see readyWorkIssueColumns). whereSQL must include the
// leading "WHERE " keyword when non-empty.
//
// + ? placeholders + hardcoded table names from FilterTables; the integer
// LIMIT is the only non-? interpolation.
//
//nolint:gosec // G201: SQL fragments are caller-built from hardcoded shapes
func runMegaQueryInTx(ctx context.Context, tx *sql.Tx, tables FilterTables, whereSQL, orderBySQL, limitSQL string, args []interface{}) ([]*types.IssueWithCounts, error) {
	megaSQL := fmt.Sprintf(`
		SELECT %s,
			l.labels_json    AS labels_json,
			COALESCE(dc.cnt, 0) AS dep_count,
			COALESCE(rc.cnt, 0) AS rdep_count,
			COALESCE(cc.cnt, 0) AS comment_count,
			pc.parent_id     AS parent_id,
			d.deps_json      AS deps_json
		FROM %s i
		LEFT JOIN (
			SELECT issue_id, JSON_ARRAYAGG(label) AS labels_json
			FROM %s
			GROUP BY issue_id
		) l ON l.issue_id = i.id
		LEFT JOIN (
			SELECT issue_id, COUNT(*) AS cnt
			FROM %s
			WHERE type = 'blocks'
			GROUP BY issue_id
		) dc ON dc.issue_id = i.id
		LEFT JOIN (
			SELECT dep_id, COUNT(*) AS cnt FROM (
				SELECT COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) AS dep_id
				FROM dependencies WHERE type = 'blocks'
				UNION ALL
				SELECT COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) AS dep_id
				FROM wisp_dependencies WHERE type = 'blocks'
			) all_blockers GROUP BY dep_id
		) rc ON rc.dep_id = i.id
		LEFT JOIN (
			SELECT issue_id, COUNT(*) AS cnt
			FROM %s
			GROUP BY issue_id
		) cc ON cc.issue_id = i.id
		LEFT JOIN (
			SELECT issue_id,
			       MIN(COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)) AS parent_id
			FROM %s
			WHERE type = 'parent-child'
			GROUP BY issue_id
		) pc ON pc.issue_id = i.id
		LEFT JOIN (
			SELECT issue_id, JSON_ARRAYAGG(%s) AS deps_json
			FROM %s
			GROUP BY issue_id
		) d ON d.issue_id = i.id
		%s
		%s
		%s
	`,
		readyWorkIssueColumns,
		tables.Main,
		tables.Labels,
		tables.Dependencies,
		tables.Comments,
		tables.Dependencies,
		readyWorkDepJSONObject,
		tables.Dependencies,
		whereSQL,
		orderBySQL,
		limitSQL,
	)

	rows, err := tx.QueryContext(ctx, megaSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("mega-query %s: %w", tables.Main, err)
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
		// Defensive dedup in case any subquery shape introduces row duplication.
		if seen[iwc.Issue.ID] {
			continue
		}
		seen[iwc.Issue.ID] = true
		out = append(out, iwc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mega-query %s: rows: %w", tables.Main, err)
	}
	return out, nil
}

// joinAnd mirrors strings.Join(..., " AND ") without pulling in strings
// just for this single site.
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
