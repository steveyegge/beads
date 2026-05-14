package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// GetReadyWorkInTx returns issues that are ready to work on (not blocked).
// computeBlockedFn is the caller's function for computing blocked IDs (since
// the DoltStore and EmbeddedDoltStore have different caching strategies).
func GetReadyWorkInTx(
	ctx context.Context,
	tx *sql.Tx,
	filter types.WorkFilter,
	computeBlockedFn func(ctx context.Context, tx *sql.Tx, includeWisps bool) ([]string, error),
) ([]*types.Issue, error) {
	blockedSet := map[string]struct{}{}
	if filter.Limit == 0 {
		blockedIDs, err := computeBlockedFn(ctx, tx, filter.IncludeEphemeral)
		if err != nil {
			blockedIDs = nil
		}

		if len(blockedIDs) > 0 {
			childrenOfBlocked, childErr := getChildrenOfIssuesInTx(ctx, tx, blockedIDs)
			if childErr == nil {
				blockedIDs = append(blockedIDs, childrenOfBlocked...)
			}
			for _, id := range blockedIDs {
				blockedSet[id] = struct{}{}
			}
		}
	}

	issueIDs, err := readyWorkIDsFromTable(ctx, tx, filter, IssuesFilterTables, true, "", blockedSet)
	if err != nil {
		return nil, err
	}
	wispVisibilityClause := "no_history = 1"
	if filter.IncludeEphemeral {
		wispVisibilityClause = "(no_history = 1 OR ephemeral = 1)"
	}
	wispIDs, wErr := readyWorkIDsFromTable(ctx, tx, filter, WispsFilterTables, false, wispVisibilityClause, blockedSet)
	if wErr != nil {
		if !isTableNotExistError(wErr) {
			return nil, wErr
		}
	} else {
		issueIDs = append(issueIDs, wispIDs...)
	}

	issues, err := GetIssuesByIDsInTx(ctx, tx, issueIDs, nil)
	if err != nil {
		return nil, fmt.Errorf("get ready work: fetch issues: %w", err)
	}
	issueMap := make(map[string]*types.Issue, len(issues))
	for _, iss := range issues {
		issueMap[iss.ID] = iss
	}
	ordered := make([]*types.Issue, 0, len(issueIDs))
	for _, id := range issueIDs {
		if iss, ok := issueMap[id]; ok {
			ordered = append(ordered, iss)
		}
	}
	return ordered, nil
}

//nolint:gosec // G201: whereSQL/orderBySQL built from hardcoded strings and ? placeholders
func readyWorkIDsFromTable(
	ctx context.Context,
	tx *sql.Tx,
	filter types.WorkFilter,
	tables FilterTables,
	applyPersistentExclusion bool,
	extraWhereClause string,
	blockedSet map[string]struct{},
) ([]string, error) {
	whereClauses, args, err := buildReadyWorkFilterClauses(ctx, tx, filter, tables, applyPersistentExclusion)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(extraWhereClause) != "" {
		whereClauses = append(whereClauses, extraWhereClause)
	}
	if len(blockedSet) > 0 {
		blockedIDs := make([]string, 0, len(blockedSet))
		for id := range blockedSet {
			blockedIDs = append(blockedIDs, id)
		}
		sort.Strings(blockedIDs)
		for start := 0; start < len(blockedIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(blockedIDs) {
				end = len(blockedIDs)
			}
			placeholders, batchArgs := buildSQLInClause(blockedIDs[start:end])
			args = append(args, batchArgs...)
			whereClauses = append(whereClauses, fmt.Sprintf("id NOT IN (%s)", placeholders))
		}
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")
	orderBySQL := readyWorkOrderBySQL(filter.SortPolicy)

	var issueIDs []string
	if filter.Limit > 0 {
		pageSize := readyWorkPageSize(filter.Limit)
		offset := 0
		for len(issueIDs) < filter.Limit {
			query := fmt.Sprintf(`
				SELECT id FROM %s
				%s
				%s
				LIMIT %d OFFSET %d
			`, tables.Main, whereSQL, orderBySQL, pageSize, offset)
			rawIDs, err := queryReadyIssueIDPage(ctx, tx, query, args)
			if err != nil {
				return nil, err
			}
			pageBlockedIDs, err := ComputeBlockedCandidateIDsInTx(ctx, tx, rawIDs, filter.IncludeEphemeral)
			if err != nil {
				return nil, err
			}
			pageBlockedSet := make(map[string]struct{}, len(pageBlockedIDs))
			for _, id := range pageBlockedIDs {
				pageBlockedSet[id] = struct{}{}
			}
			for _, id := range rawIDs {
				if _, blocked := pageBlockedSet[id]; blocked {
					continue
				}
				issueIDs = append(issueIDs, id)
				if len(issueIDs) >= filter.Limit {
					break
				}
			}
			if len(issueIDs) >= filter.Limit || len(rawIDs) < pageSize {
				break
			}
			offset += len(rawIDs)
		}
	} else {
		query := fmt.Sprintf(`
			SELECT id FROM %s
			%s
			%s
		`, tables.Main, whereSQL, orderBySQL)
		if _, err := appendReadyIssueIDs(ctx, tx, query, args, blockedSet, 0, &issueIDs); err != nil {
			return nil, err
		}
	}
	return issueIDs, nil
}

func buildReadyWorkFilterClauses(
	ctx context.Context,
	tx *sql.Tx,
	filter types.WorkFilter,
	tables FilterTables,
	applyPersistentExclusion bool,
) ([]string, []interface{}, error) {
	// Status filtering: default to open OR in_progress.
	var statusClause string
	if filter.Status != "" {
		statusClause = "status = ?"
	} else {
		statusClause = "status IN ('open', 'in_progress')"
	}
	whereClauses := []string{
		statusClause,
		"(pinned = 0 OR pinned IS NULL)",
	}
	if applyPersistentExclusion && !filter.IncludeEphemeral {
		whereClauses = append(whereClauses, "(ephemeral = 0 OR ephemeral IS NULL)")
	}
	var args []interface{}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
	}

	if filter.Priority != nil {
		whereClauses = append(whereClauses, "priority = ?")
		args = append(args, *filter.Priority)
	}
	// Use subquery for type filter to prevent join issues.
	if filter.Type != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("id IN (SELECT id FROM %s WHERE issue_type = ?)", tables.Main))
		args = append(args, filter.Type)
	} else {
		excludeTypes := []string{"merge-request", "gate", "molecule", "message", "agent", "role", "rig"}
		for _, t := range filter.ExcludeTypes {
			excludeTypes = append(excludeTypes, string(t))
		}
		placeholders := make([]string, len(excludeTypes))
		for i, t := range excludeTypes {
			placeholders[i] = "?"
			args = append(args, t)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("id IN (SELECT id FROM %s WHERE issue_type NOT IN (%s))", tables.Main, strings.Join(placeholders, ",")))
	}
	// Unassigned takes precedence over Assignee filter.
	if filter.Unassigned {
		whereClauses = append(whereClauses, "(assignee IS NULL OR assignee = '')")
	} else if filter.Assignee != nil {
		whereClauses = append(whereClauses, "assignee = ?")
		args = append(args, *filter.Assignee)
	}
	// Exclude future-deferred issues unless IncludeDeferred is set.
	if !filter.IncludeDeferred {
		whereClauses = append(whereClauses, "(defer_until IS NULL OR defer_until <= UTC_TIMESTAMP())")
	}
	// Exclude children of future-deferred parents.
	if !filter.IncludeDeferred {
		deferredChildIDs, dcErr := getChildrenOfDeferredParentsInTx(ctx, tx, tables.Main, tables.Dependencies)
		if dcErr == nil && len(deferredChildIDs) > 0 {
			for start := 0; start < len(deferredChildIDs); start += queryBatchSize {
				end := start + queryBatchSize
				if end > len(deferredChildIDs) {
					end = len(deferredChildIDs)
				}
				placeholders, batchArgs := buildSQLInClause(deferredChildIDs[start:end])
				args = append(args, batchArgs...)
				whereClauses = append(whereClauses, fmt.Sprintf("id NOT IN (%s)", placeholders))
			}
		}
	}
	if len(filter.Labels) > 0 {
		for _, label := range filter.Labels {
			whereClauses = append(whereClauses, fmt.Sprintf("id IN (SELECT issue_id FROM %s WHERE label = ?)", tables.Labels))
			args = append(args, label)
		}
	}
	if len(filter.LabelsAny) > 0 {
		placeholders := make([]string, len(filter.LabelsAny))
		for i, label := range filter.LabelsAny {
			placeholders[i] = "?"
			args = append(args, label)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("id IN (SELECT issue_id FROM %s WHERE label IN (%s))", tables.Labels, strings.Join(placeholders, ",")))
	}
	// Parent filtering.
	if filter.ParentID != nil {
		parentID := *filter.ParentID
		whereClauses = append(whereClauses, fmt.Sprintf("(id IN (SELECT issue_id FROM %s WHERE type = 'parent-child' AND depends_on_id = ?) OR (id LIKE CONCAT(?, '.%%') AND id NOT IN (SELECT issue_id FROM %s WHERE type = 'parent-child')))", tables.Dependencies, tables.Dependencies))
		args = append(args, parentID, parentID)
	}

	// Molecule filtering: filter to direct children of the specified molecule.
	if filter.MoleculeID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(id IN (SELECT issue_id FROM %s WHERE type = 'parent-child' AND depends_on_id = ?) OR (id LIKE CONCAT(?, '.%%') AND id NOT IN (SELECT issue_id FROM %s WHERE type = 'parent-child')))", tables.Dependencies, tables.Dependencies))
		args = append(args, filter.MoleculeID, filter.MoleculeID)
	}

	// Metadata existence check.
	if filter.HasMetadataKey != "" {
		if err := storage.ValidateMetadataKey(filter.HasMetadataKey); err != nil {
			return nil, nil, err
		}
		whereClauses = append(whereClauses, "JSON_EXTRACT(metadata, ?) IS NOT NULL")
		args = append(args, "$."+filter.HasMetadataKey)
	}

	// Metadata field equality filters.
	if len(filter.MetadataFields) > 0 {
		metaKeys := make([]string, 0, len(filter.MetadataFields))
		for k := range filter.MetadataFields {
			metaKeys = append(metaKeys, k)
		}
		sort.Strings(metaKeys)
		for _, k := range metaKeys {
			if err := storage.ValidateMetadataKey(k); err != nil {
				return nil, nil, err
			}
			whereClauses = append(whereClauses, "JSON_UNQUOTE(JSON_EXTRACT(metadata, ?)) = ?")
			args = append(args, storage.JSONMetadataPath(k), filter.MetadataFields[k])
		}
	}

	return whereClauses, args, nil
}

func readyWorkOrderBySQL(sortPolicy types.SortPolicy) string {
	switch sortPolicy {
	case types.SortPolicyOldest:
		return "ORDER BY created_at ASC, id ASC"
	case types.SortPolicyPriority:
		return "ORDER BY priority ASC, created_at DESC, id ASC"
	case types.SortPolicyHybrid, "":
		return `ORDER BY
			CASE WHEN created_at >= DATE_SUB(NOW(), INTERVAL 48 HOUR) THEN 0 ELSE 1 END ASC,
			CASE WHEN created_at >= DATE_SUB(NOW(), INTERVAL 48 HOUR) THEN priority ELSE 999 END ASC,
			created_at ASC, id ASC`
	default:
		return "ORDER BY priority ASC, created_at DESC, id ASC"
	}
}

func readyWorkPageSize(limit int) int {
	pageSize := limit * 4
	if pageSize < 100 {
		return 100
	}
	if pageSize > 1000 {
		return 1000
	}
	return pageSize
}

func queryReadyIssueIDPage(ctx context.Context, tx *sql.Tx, query string, args []interface{}) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get ready work: %w", err)
	}
	defer rows.Close()

	var issueIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("get ready work: scan id: %w", err)
		}
		issueIDs = append(issueIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get ready work: rows: %w", err)
	}
	return issueIDs, nil
}

func appendReadyIssueIDs(
	ctx context.Context,
	tx *sql.Tx,
	query string,
	args []interface{},
	blockedSet map[string]struct{},
	max int,
	issueIDs *[]string,
) (int, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to get ready work: %w", err)
	}
	defer rows.Close()

	rawCount := 0
	for rows.Next() {
		rawCount++
		var id string
		if err := rows.Scan(&id); err != nil {
			return rawCount, fmt.Errorf("get ready work: scan id: %w", err)
		}
		if _, blocked := blockedSet[id]; blocked {
			continue
		}
		*issueIDs = append(*issueIDs, id)
		if max > 0 && len(*issueIDs) >= max {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return rawCount, fmt.Errorf("get ready work: rows: %w", err)
	}
	return rawCount, nil
}

// getChildrenOfDeferredParentsInTx returns IDs of issues whose parent has a
// future defer_until. Works within an existing transaction.
func getChildrenOfDeferredParentsInTx(ctx context.Context, tx *sql.Tx, issueTable string, dependencyTable string) ([]string, error) {
	// Step 1: Get IDs of issues with future defer_until.
	deferredRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT id FROM %s
		WHERE defer_until IS NOT NULL AND defer_until > UTC_TIMESTAMP()
	`, issueTable))
	if err != nil {
		return nil, fmt.Errorf("deferred parents: get deferred issues: %w", err)
	}
	var deferredIDs []string
	for deferredRows.Next() {
		var id string
		if err := deferredRows.Scan(&id); err != nil {
			_ = deferredRows.Close()
			return nil, fmt.Errorf("deferred parents: scan deferred issue: %w", err)
		}
		deferredIDs = append(deferredIDs, id)
	}
	_ = deferredRows.Close()
	if err := deferredRows.Err(); err != nil {
		return nil, fmt.Errorf("deferred parents: deferred rows: %w", err)
	}
	if len(deferredIDs) == 0 {
		return nil, nil
	}

	// Step 2: Get children of those deferred parents.
	return getChildrenOfIssuesFromDependencyTableInTx(ctx, tx, dependencyTable, deferredIDs)
}

// getChildrenOfIssuesInTx returns IDs of direct children (parent-child deps)
// of the given issue IDs. Scans both dependencies and wisp_dependencies tables.
//
//nolint:gosec // G201: depTable is hardcoded to "dependencies" or "wisp_dependencies"
func getChildrenOfIssuesInTx(ctx context.Context, tx *sql.Tx, parentIDs []string) ([]string, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	var children []string
	for _, depTable := range []string{"dependencies", "wisp_dependencies"} {
		for start := 0; start < len(parentIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(parentIDs) {
				end = len(parentIDs)
			}
			placeholders, args := buildSQLInClause(parentIDs[start:end])

			query := fmt.Sprintf(`
				SELECT issue_id FROM %s
				WHERE type = 'parent-child' AND depends_on_id IN (%s)
			`, depTable, placeholders)
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				// wisp_dependencies table may not exist on pre-migration databases.
				if depTable == "wisp_dependencies" {
					break
				}
				return nil, fmt.Errorf("get children of issues from %s: %w", depTable, err)
			}
			for rows.Next() {
				var childID string
				if err := rows.Scan(&childID); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("get children of issues: scan: %w", err)
				}
				children = append(children, childID)
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("get children of issues: rows from %s: %w", depTable, err)
			}
		}
	}
	return children, nil
}

// getChildrenOfIssuesFromDependencyTableInTx returns direct children from one
// dependency table. The table argument is selected from FilterTables constants.
//
//nolint:gosec // G201: dependencyTable comes from FilterTables, not user input.
func getChildrenOfIssuesFromDependencyTableInTx(ctx context.Context, tx *sql.Tx, dependencyTable string, parentIDs []string) ([]string, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	var children []string
	for start := 0; start < len(parentIDs); start += queryBatchSize {
		end := start + queryBatchSize
		if end > len(parentIDs) {
			end = len(parentIDs)
		}
		placeholders, args := buildSQLInClause(parentIDs[start:end])
		query := fmt.Sprintf(`
			SELECT issue_id FROM %s
			WHERE type = 'parent-child' AND depends_on_id IN (%s)
		`, dependencyTable, placeholders)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("get children of issues from %s: %w", dependencyTable, err)
		}
		for rows.Next() {
			var childID string
			if err := rows.Scan(&childID); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("get children of issues: scan: %w", err)
			}
			children = append(children, childID)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get children of issues: rows from %s: %w", dependencyTable, err)
		}
	}
	return children, nil
}

// buildSQLInClause builds a parameterized IN clause from a slice of IDs.
func buildSQLInClause(ids []string) (string, []interface{}) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return strings.Join(placeholders, ","), args
}
