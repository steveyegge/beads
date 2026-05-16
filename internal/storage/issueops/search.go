package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// SearchIssuesInTx executes a filtered issue search within an existing transaction.
// It queries the issues table, optionally merges wisps, and returns hydrated issues
// with labels populated.
func SearchIssuesInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	// Route ephemeral-only queries to wisps table.
	if filter.Ephemeral != nil && *filter.Ephemeral {
		results, err := searchTableInTx(ctx, tx, query, filter, WispsFilterTables)
		if err != nil && !isTableNotExistError(err) {
			return nil, fmt.Errorf("search wisps (ephemeral filter): %w", err)
		}
		if len(results) > 0 {
			return results, nil
		}
		// Fall through: wisps table doesn't exist or returned no results
	}

	results, err := searchTableInTx(ctx, tx, query, filter, IssuesFilterTables)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}

	// When filter.Ephemeral is nil (search everything) or false (non-ephemeral
	// only), also search the wisps table and merge results. NoHistory beads are
	// stored in the wisps table with ephemeral=0, so they must survive an
	// Ephemeral=&false filter (GH#3649). The WHERE clause added by
	// BuildIssueFilterClauses handles the per-row ephemeral column check, so
	// querying wisps here with Ephemeral=&false returns only NoHistory beads
	// while correctly excluding true ephemeral wisps. (GH#3659)
	if filter.Ephemeral == nil || !*filter.Ephemeral {
		wispResults, wispErr := searchTableInTx(ctx, tx, query, filter, WispsFilterTables)
		if wispErr != nil && !isTableNotExistError(wispErr) {
			return nil, fmt.Errorf("search wisps (merge): %w", wispErr)
		}
		if len(wispResults) > 0 {
			seen := make(map[string]bool, len(results))
			for _, issue := range results {
				seen[issue.ID] = true
			}
			for _, issue := range wispResults {
				if !seen[issue.ID] {
					results = append(results, issue)
				}
			}
		}
	}

	return results, nil
}

// searchTableInTx runs a filtered search against a specific table set (issues or wisps).
func searchTableInTx(ctx context.Context, tx *sql.Tx, query string, filter types.IssueFilter, tables FilterTables) ([]*types.Issue, error) {
	fromSQL, labelWhere, labelArgs, labelDriven, filterForClauses := buildLabelDrivenSearch(filter, tables)
	whereClauses, args, err := BuildIssueFilterClauses(query, filterForClauses, tables)
	if err != nil {
		return nil, err
	}
	if len(labelWhere) > 0 {
		whereClauses = append(labelWhere, whereClauses...)
		args = append(labelArgs, args...)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	selectSQL := "SELECT "
	if labelDriven {
		selectSQL = "SELECT DISTINCT "
	}
	//nolint:gosec // G201: SQL fragments are built from fixed table/column names and parameterized filters.
	querySQL := fmt.Sprintf(`%s%s FROM %s %s ORDER BY priority ASC, created_at DESC, id ASC %s`,
		selectSQL, IssueSelectColumns, fromSQL, whereSQL, limitSQL)

	rows, err := tx.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", tables.Main, err)
	}

	var issues []*types.Issue
	seen := make(map[string]bool)
	for rows.Next() {
		issue, scanErr := ScanIssueFrom(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("search %s: scan: %w", tables.Main, scanErr)
		}
		if seen[issue.ID] {
			continue // GH#3567: skip duplicate rows from dependency subqueries
		}
		seen[issue.ID] = true
		issues = append(issues, issue)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search %s: rows: %w", tables.Main, err)
	}

	// Hydrate labels in bulk after closing the result set, so we don't hold
	// multiple active result sets on the same connection.
	if len(issues) > 0 {
		ids := make([]string, len(issues))
		for i, issue := range issues {
			ids[i] = issue.ID
		}
		// Fast path: searchTableInTx queries exclusively either the issues
		// or wisps table, so every ID in `ids` belongs to tables.Labels.
		// Skip the per-batch wisp-partition round-trip that the generic
		// GetLabelsForIssuesInTx performs (GH#3414).
		labelMap, labelErr := GetLabelsForIssuesFromTableInTx(ctx, tx, tables.Labels, ids)
		if labelErr != nil {
			return nil, fmt.Errorf("search %s: hydrate labels: %w", tables.Main, labelErr)
		}
		for _, issue := range issues {
			if labels, ok := labelMap[issue.ID]; ok {
				issue.Labels = labels
			}
		}

		// Optionally hydrate dependencies in bulk (same batched pattern as labels).
		if filter.IncludeDependencies {
			depMap, depErr := GetDependencyRecordsForIssuesFromTableInTx(ctx, tx, tables.Dependencies, ids)
			if depErr != nil {
				return nil, fmt.Errorf("search %s: hydrate dependencies: %w", tables.Main, depErr)
			}
			for _, issue := range issues {
				if deps, ok := depMap[issue.ID]; ok {
					issue.Dependencies = deps
				}
			}
		}
	}

	return issues, nil
}

func buildLabelDrivenSearch(filter types.IssueFilter, tables FilterTables) (string, []string, []interface{}, bool, types.IssueFilter) {
	labels := compactNonEmptyStrings(filter.Labels)
	labelsAny := compactNonEmptyStrings(filter.LabelsAny)
	if len(labels) == 0 && len(labelsAny) == 0 {
		return tables.Main, nil, nil, false, filter
	}

	filterForClauses := filter
	filterForClauses.Labels = nil
	filterForClauses.LabelsAny = nil

	var joins []string
	var where []string
	var args []interface{}

	for i, label := range labels {
		alias := fmt.Sprintf("label_filter_%d", i)
		joins = append(joins, fmt.Sprintf("JOIN %s %s ON %s.issue_id = %s.id", tables.Labels, alias, alias, tables.Main))
		where = append(where, fmt.Sprintf("%s.label = ?", alias))
		args = append(args, label)
	}

	if len(labelsAny) > 0 {
		alias := "label_filter_any"
		joins = append(joins, fmt.Sprintf("JOIN %s %s ON %s.issue_id = %s.id", tables.Labels, alias, alias, tables.Main))
		placeholders := make([]string, len(labelsAny))
		for i, label := range labelsAny {
			placeholders[i] = "?"
			args = append(args, label)
		}
		where = append(where, fmt.Sprintf("%s.label IN (%s)", alias, strings.Join(placeholders, ", ")))
	}

	return tables.Main + " " + strings.Join(joins, " "), where, args, true, filterForClauses
}

func compactNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
