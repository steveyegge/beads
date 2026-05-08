package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// BuildSummaryOrderBy returns the ORDER BY clause for SearchIssueSummaries.
// An empty sortBy preserves the historical default "priority ASC, created_at
// DESC, id ASC". Named sortBy values mirror the --sort flag in `bd list`. The
// id ASC tiebreaker keeps row order deterministic for equal sort keys.
//
// Direction matches the previous Go-side sortSummaries: priority/status/id/
// title/type/assignee default ASC; created/updated/closed default DESC. Reverse
// flips the primary direction; the id tiebreaker stays ASC so result order
// remains stable run-to-run.
//
// MySQL/Dolt's native NULL ordering aligns with the previous Go behavior:
// NULLs sort first for ASC and last for DESC, matching how nil ClosedAt rows
// were placed by sortSummaries.
func BuildSummaryOrderBy(sortBy string, reverse bool) (string, error) {
	if sortBy == "" {
		return "ORDER BY priority ASC, created_at DESC, id ASC", nil
	}
	col, dir, ok := summarySortColumn(sortBy)
	if !ok {
		return "", fmt.Errorf("invalid sort field %q", sortBy)
	}
	if reverse {
		if dir == "ASC" {
			dir = "DESC"
		} else {
			dir = "ASC"
		}
	}
	if col == "id" {
		return fmt.Sprintf("ORDER BY id %s", dir), nil
	}
	return fmt.Sprintf("ORDER BY %s %s, id ASC", col, dir), nil
}

func summarySortColumn(sortBy string) (col, dir string, ok bool) {
	switch sortBy {
	case "priority":
		return "priority", "ASC", true
	case "created":
		return "created_at", "DESC", true
	case "updated":
		return "updated_at", "DESC", true
	case "closed":
		return "closed_at", "DESC", true
	case "status":
		return "status", "ASC", true
	case "id":
		return "id", "ASC", true
	case "title":
		return "lower(title)", "ASC", true
	case "type":
		return "issue_type", "ASC", true
	case "assignee":
		return "assignee", "ASC", true
	}
	return "", "", false
}

// SearchIssueSummariesInTx executes a filtered search within an existing
// transaction and returns narrow IssueSummary rows. Mirrors SearchIssuesInTx
// (filter resolution, wisp admission, label hydration) but selects only
// IssueSummaryColumns — no TEXT/JSON dereferences. D3 build, be-nu4.3.2.
//
// Label hydration uses the D2 wisp-set helper; the set is built once before
// any result-producing query to avoid multiple-active-result-sets on the
// same connection.
func SearchIssueSummariesInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter) ([]*types.IssueSummary, error) {
	if filter.Ephemeral != nil && *filter.Ephemeral {
		results, err := searchSummaryTableInTx(ctx, tx, query, filter, WispsFilterTables)
		if err != nil && !isTableNotExistError(err) {
			return nil, fmt.Errorf("search wisps summaries (ephemeral filter): %w", err)
		}
		if len(results) > 0 {
			return results, nil
		}
	}

	results, err := searchSummaryTableInTx(ctx, tx, query, filter, IssuesFilterTables)
	if err != nil {
		return nil, fmt.Errorf("search issue summaries: %w", err)
	}

	if filter.Ephemeral == nil {
		wispResults, wispErr := searchSummaryTableInTx(ctx, tx, query, filter, WispsFilterTables)
		if wispErr != nil && !isTableNotExistError(wispErr) {
			return nil, fmt.Errorf("search wisps summaries (merge): %w", wispErr)
		}
		if len(wispResults) > 0 {
			seen := make(map[string]bool, len(results))
			for _, s := range results {
				seen[s.ID] = true
			}
			for _, s := range wispResults {
				if !seen[s.ID] {
					results = append(results, s)
				}
			}
		}
	}

	return results, nil
}

// searchSummaryTableInTx runs a filtered search against a specific table set
// (issues or wisps) and returns narrow summaries. Parallel to searchTableInTx
// but with IssueSummaryColumns and the summary scan path.
func searchSummaryTableInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter, tables FilterTables) ([]*types.IssueSummary, error) {
	whereClauses, args, err := BuildIssueFilterClauses(query, filter, tables)
	if err != nil {
		return nil, err
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	orderSQL, err := BuildSummaryOrderBy(filter.SortBy, filter.SortReverse)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // G201: whereSQL contains column comparisons with ?, orderSQL is built from a fixed allowlist, limitSQL is a safe integer
	querySQL := fmt.Sprintf(`SELECT %s FROM %s %s %s%s`,
		IssueSummaryColumns, tables.Main, whereSQL, orderSQL, limitSQL)

	rows, err := tx.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("search %s summaries: %w", tables.Main, err)
	}

	var summaries []*types.IssueSummary
	for rows.Next() {
		sum, scanErr := ScanIssueSummaryFrom(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("search %s summaries: scan: %w", tables.Main, scanErr)
		}
		summaries = append(summaries, sum)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search %s summaries: rows: %w", tables.Main, err)
	}

	if len(summaries) > 0 {
		ids := make([]string, len(summaries))
		for i, s := range summaries {
			ids[i] = s.ID
		}
		wispSet, wispErr := WispIDSetInTx(ctx, tx, ids)
		if wispErr != nil {
			return nil, fmt.Errorf("build wisp set: %w", wispErr)
		}
		labelMap, labelErr := GetLabelsForIssuesInTx(ctx, tx, ids, wispSet)
		if labelErr != nil {
			return nil, fmt.Errorf("search %s summaries: hydrate labels: %w", tables.Main, labelErr)
		}
		for _, s := range summaries {
			if labels, ok := labelMap[s.ID]; ok {
				s.Labels = labels
			}
		}
	}

	return summaries, nil
}
