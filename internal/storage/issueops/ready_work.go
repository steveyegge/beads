package issueops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// GetReadyWorkInTx returns issues that are ready to work on (not blocked).
// computeBlockedFn is the caller's function for computing blocked IDs (since
// the DoltStore and EmbeddedDoltStore have different caching strategies).
//
//nolint:gosec // G201: whereSQL/orderBySQL built from hardcoded strings and ? placeholders
func GetReadyWorkInTx(
	ctx context.Context,
	tx *sql.Tx,
	filter types.WorkFilter,
	computeBlockedFn func(ctx context.Context, tx *sql.Tx, includeWisps bool) ([]string, error),
) ([]*types.Issue, error) {
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
	if !filter.IncludeEphemeral {
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
	if filter.Type != "" {
		// Keep the type predicate isolated from other indexed predicates to
		// avoid Dolt's mergeJoinIter panic on type+status+priority queries.
		whereClauses = append(whereClauses, "id IN (SELECT id FROM issues WHERE issue_type = ?)")
		args = append(args, filter.Type)
	} else {
		excludeTypes := readyWorkExcludeTypes(filter.ExcludeTypes)
		placeholders := make([]string, len(excludeTypes))
		for i, t := range excludeTypes {
			placeholders[i] = "?"
			args = append(args, string(t))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("id IN (SELECT id FROM issues WHERE issue_type NOT IN (%s))", strings.Join(placeholders, ",")))
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
		deferredChildIDs, dcErr := getChildrenOfDeferredParentsInTx(ctx, tx)
		if dcErr != nil {
			return nil, fmt.Errorf("get ready work: compute deferred parent children: %w", dcErr)
		}
		if len(deferredChildIDs) > 0 {
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
			whereClauses = append(whereClauses, "id IN (SELECT issue_id FROM labels WHERE label = ?)")
			args = append(args, label)
		}
	}
	if len(filter.ExcludeLabels) > 0 {
		placeholders := make([]string, len(filter.ExcludeLabels))
		for i, label := range filter.ExcludeLabels {
			placeholders[i] = "?"
			args = append(args, label)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("id NOT IN (SELECT issue_id FROM labels WHERE label IN (%s))", strings.Join(placeholders, ", ")))
	}
	// Parent filtering: return all transitive descendants of parentID.
	// GH#3396: previously was a one-hop subquery against dependencies, so
	// grandchildren were silently dropped despite the help text and
	// WorkFilter.ParentID godoc both promising "descendants (recursive)".
	if filter.ParentID != nil {
		parentID := *filter.ParentID
		descendantIDs, descErr := GetDescendantIDsInTx(ctx, tx, parentID, 0)
		if descErr != nil {
			return nil, fmt.Errorf("get parent descendants: %w", descErr)
		}
		parentClauses := []string{"(id LIKE CONCAT(?, '.%') AND id NOT IN (SELECT issue_id FROM dependencies WHERE type = 'parent-child'))"}
		args = append(args, parentID)
		for start := 0; start < len(descendantIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(descendantIDs) {
				end = len(descendantIDs)
			}
			placeholders, batchArgs := buildSQLInClause(descendantIDs[start:end])
			parentClauses = append(parentClauses, fmt.Sprintf("id IN (%s)", placeholders))
			args = append(args, batchArgs...)
		}
		whereClauses = append(whereClauses, "("+strings.Join(parentClauses, " OR ")+")")
	}

	// Molecule filtering: filter to direct children of the specified molecule.
	if filter.MoleculeID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(id IN (SELECT issue_id FROM dependencies WHERE type = 'parent-child' AND %s = ?) OR (id LIKE CONCAT(?, '.%%') AND id NOT IN (SELECT issue_id FROM dependencies WHERE type = 'parent-child')))", DepTargetExpr))
		args = append(args, filter.MoleculeID, filter.MoleculeID)
	}

	// Metadata existence check.
	if filter.HasMetadataKey != "" {
		if err := storage.ValidateMetadataKey(filter.HasMetadataKey); err != nil {
			return nil, err
		}
		whereClauses = append(whereClauses, "JSON_EXTRACT(metadata, ?) IS NOT NULL")
		args = append(args, storage.JSONMetadataPath(filter.HasMetadataKey))
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
				return nil, err
			}
			whereClauses = append(whereClauses, "JSON_UNQUOTE(JSON_EXTRACT(metadata, ?)) = ?")
			args = append(args, storage.JSONMetadataPath(k), filter.MetadataFields[k])
		}
	}

	// Exclude blocked issues eagerly for unbounded queries. Limited queries page
	// candidate IDs first and filter blockers per page below, avoiding a full
	// dependency graph scan when the caller only needs a small ready set.
	if filter.Limit == 0 {
		blockedIDs, err := computeBlockedFn(ctx, tx, true)
		if err != nil {
			return nil, fmt.Errorf("compute blocked IDs: %w", err)
		}
		if len(blockedIDs) > 0 {
			// Also exclude children of blocked parents.
			childrenOfBlocked, childErr := getChildrenOfIssuesInTx(ctx, tx, blockedIDs)
			if childErr != nil {
				return nil, fmt.Errorf("compute blocked children: %w", childErr)
			}
			blockedIDs = append(blockedIDs, childrenOfBlocked...)

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
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	// Build ORDER BY clause based on SortPolicy.
	var orderBySQL string
	switch filter.SortPolicy {
	case types.SortPolicyOldest:
		orderBySQL = "ORDER BY created_at ASC, id ASC"
	case types.SortPolicyPriority:
		orderBySQL = "ORDER BY priority ASC, created_at DESC, id ASC"
	case types.SortPolicyHybrid, "":
		recentCutoff := time.Now().UTC().Add(-48 * time.Hour)
		orderBySQL = `ORDER BY
			CASE WHEN created_at >= ? THEN 0 ELSE 1 END ASC,
			CASE WHEN created_at >= ? THEN priority ELSE 999 END ASC,
			created_at ASC, id ASC`
		args = append(args, recentCutoff, recentCutoff)
	default:
		orderBySQL = "ORDER BY priority ASC, created_at DESC, id ASC"
	}

	var issueIDs []string
	if filter.Limit > 0 {
		pageSize := readyWorkPageSize(filter.Limit)
		for offset := 0; len(issueIDs) < filter.Limit; offset += pageSize {
			//nolint:gosec // G201: whereSQL/orderBySQL are hardcoded, pageSize/offset are integers
			query := fmt.Sprintf(`
				SELECT id FROM issues
				%s
				%s
				LIMIT %d OFFSET %d
			`, whereSQL, orderBySQL, pageSize, offset)

			pageIDs, err := queryReadyIssueIDPage(ctx, tx, query, args)
			if err != nil {
				return nil, err
			}
			if len(pageIDs) == 0 {
				break
			}

			blockedPageIDs, err := ComputeBlockedCandidateIDsInTx(ctx, tx, pageIDs, true)
			if err != nil {
				return nil, fmt.Errorf("get ready work: filter blocked candidates: %w", err)
			}
			blockedPageSet := make(map[string]struct{}, len(blockedPageIDs))
			for _, id := range blockedPageIDs {
				blockedPageSet[id] = struct{}{}
			}

			for _, id := range pageIDs {
				if _, blocked := blockedPageSet[id]; blocked {
					continue
				}
				issueIDs = append(issueIDs, id)
				if len(issueIDs) >= filter.Limit {
					break
				}
			}
			if len(pageIDs) < pageSize {
				break
			}
		}
	} else {
		//nolint:gosec // G201: whereSQL/orderBySQL are hardcoded strings and ? placeholders
		query := fmt.Sprintf(`
			SELECT id FROM issues
			%s
			%s
		`, whereSQL, orderBySQL)

		var err error
		issueIDs, err = queryReadyIssueIDPage(ctx, tx, query, args)
		if err != nil {
			return nil, err
		}
	}

	// Batch-fetch full issues preserving order.
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

	wisps, wErr := getReadyWispsInTx(ctx, tx, filter)
	if wErr != nil {
		return nil, wErr
	}
	if len(wisps) > 0 {
		ordered, err = mergeReadyWisps(ordered, wisps, filter)
		if err != nil {
			return nil, err
		}
	}

	return ordered, nil
}

func mergeReadyWisps(ordered []*types.Issue, wisps []*types.Issue, filter types.WorkFilter) ([]*types.Issue, error) {
	seen := make(map[string]struct{}, len(ordered))
	for _, issue := range ordered {
		seen[issue.ID] = struct{}{}
	}
	for _, wisp := range wisps {
		if _, exists := seen[wisp.ID]; exists {
			return nil, fmt.Errorf("ready work id %q exists in both issues and wisps", wisp.ID)
		}
		ordered = append(ordered, wisp)
	}
	sortReadyIssues(ordered, filter.SortPolicy)
	if filter.Limit > 0 && len(ordered) > filter.Limit {
		ordered = ordered[:filter.Limit]
	}
	return ordered, nil
}

func getReadyWispsInTx(ctx context.Context, tx *sql.Tx, filter types.WorkFilter) ([]*types.Issue, error) {
	wispFilter := readyWorkWispIssueFilter(filter)
	// Ready-only wisp predicates are applied after search, so avoid limiting
	// before that filtering can drop non-ready candidates.
	wispFilter.Limit = 0
	wisps, err := searchTableInTx(ctx, tx, "", wispFilter, WispsFilterTables)
	if err != nil {
		if isTableNotExistError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("search wisps (ready work): %w", err)
	}
	return filterReadyWispsInTx(ctx, tx, filter, wisps)
}

func readyWorkExcludeTypes(extra []types.IssueType) []types.IssueType {
	excludeTypes := []types.IssueType{
		types.IssueType("merge-request"),
		types.TypeGate,
		types.TypeMolecule,
		types.TypeMessage,
		types.IssueType("agent"),
		types.IssueType("role"),
		types.IssueType("rig"),
	}
	seen := make(map[types.IssueType]bool, len(excludeTypes)+len(extra))
	for _, t := range excludeTypes {
		seen[t] = true
	}
	for _, t := range extra {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		excludeTypes = append(excludeTypes, t)
	}
	return excludeTypes
}

func readyWorkWispIssueFilter(filter types.WorkFilter) types.IssueFilter {
	pinnedFalse := false
	wispFilter := types.IssueFilter{
		Priority:       filter.Priority,
		Labels:         filter.Labels,
		LabelsAny:      filter.LabelsAny,
		ExcludeLabels:  filter.ExcludeLabels,
		Limit:          filter.Limit,
		MolType:        filter.MolType,
		WispType:       filter.WispType,
		Pinned:         &pinnedFalse,
		MetadataFields: filter.MetadataFields,
		HasMetadataKey: filter.HasMetadataKey,
	}
	if filter.Status != "" {
		s := filter.Status
		wispFilter.Status = &s
	} else {
		wispFilter.Statuses = []types.Status{types.StatusOpen, types.StatusInProgress}
	}
	if filter.Type != "" {
		t := types.IssueType(filter.Type)
		wispFilter.IssueType = &t
	} else {
		wispFilter.ExcludeTypes = readyWorkExcludeTypes(filter.ExcludeTypes)
	}
	if filter.Unassigned {
		wispFilter.NoAssignee = true
	} else if filter.Assignee != nil {
		wispFilter.Assignee = filter.Assignee
	}
	if filter.MoleculeID != "" {
		moleculeID := filter.MoleculeID
		wispFilter.ParentID = &moleculeID
	}
	if !filter.IncludeEphemeral {
		ephFalse := false
		wispFilter.Ephemeral = &ephFalse
	}
	return wispFilter
}

func filterReadyWispsInTx(ctx context.Context, tx *sql.Tx, filter types.WorkFilter, wisps []*types.Issue) ([]*types.Issue, error) {
	if len(wisps) == 0 {
		return wisps, nil
	}

	wispIDs := make([]string, 0, len(wisps))
	for _, wisp := range wisps {
		wispIDs = append(wispIDs, wisp.ID)
	}

	excluded := make(map[string]struct{})
	if filter.ParentID != nil {
		parentID := *filter.ParentID
		descendantIDs, err := GetDescendantIDsInTx(ctx, tx, parentID, 0)
		if err != nil {
			return nil, fmt.Errorf("get wisp parent descendants: %w", err)
		}
		descendantSet := make(map[string]struct{}, len(descendantIDs))
		for _, id := range descendantIDs {
			descendantSet[id] = struct{}{}
		}
		parentedSet, err := getParentedIDSetInTx(ctx, tx, wispIDs)
		if err != nil {
			return nil, err
		}
		for _, wisp := range wisps {
			if _, ok := descendantSet[wisp.ID]; ok {
				continue
			}
			if strings.HasPrefix(wisp.ID, parentID+".") {
				if _, hasParent := parentedSet[wisp.ID]; !hasParent {
					continue
				}
			}
			excluded[wisp.ID] = struct{}{}
		}
	}

	if !filter.IncludeDeferred {
		now := time.Now().UTC()
		for _, wisp := range wisps {
			if wisp.DeferUntil != nil && wisp.DeferUntil.After(now) {
				excluded[wisp.ID] = struct{}{}
			}
		}
		deferredChildIDs, err := getChildrenOfDeferredParentsInTx(ctx, tx)
		if err != nil {
			return nil, err
		}
		for _, id := range deferredChildIDs {
			excluded[id] = struct{}{}
		}
	}

	blockedIDs, err := ComputeBlockedCandidateIDsInTx(ctx, tx, wispIDs, true)
	if err != nil {
		return nil, fmt.Errorf("get ready work: filter blocked wisps: %w", err)
	}
	for _, id := range blockedIDs {
		excluded[id] = struct{}{}
	}

	ready := wisps[:0]
	for _, wisp := range wisps {
		if wisp.Pinned {
			continue
		}
		if _, skip := excluded[wisp.ID]; skip {
			continue
		}
		ready = append(ready, wisp)
	}
	return ready, nil
}

func sortReadyIssues(issues []*types.Issue, policy types.SortPolicy) {
	recentCutoff := time.Now().UTC().Add(-48 * time.Hour)
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		switch policy {
		case types.SortPolicyOldest:
			return issueCreatedBefore(a, b)
		case types.SortPolicyPriority:
			return issuePriorityBefore(a, b)
		case types.SortPolicyHybrid, "":
			aRecent := !a.CreatedAt.Before(recentCutoff)
			bRecent := !b.CreatedAt.Before(recentCutoff)
			if aRecent != bRecent {
				return aRecent
			}
			if aRecent && a.Priority != b.Priority {
				return a.Priority < b.Priority
			}
			return issueCreatedBefore(a, b)
		default:
			return issuePriorityBefore(a, b)
		}
	})
}

func issuePriorityBefore(a, b *types.Issue) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID < b.ID
}

func issueCreatedBefore(a, b *types.Issue) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
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

	var issueIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("get ready work: scan id: %w", err)
		}
		issueIDs = append(issueIDs, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get ready work: rows: %w", err)
	}
	return issueIDs, nil
}

// getChildrenOfDeferredParentsInTx returns IDs of issues whose parent has a
// future defer_until. Works within an existing transaction.
//
//nolint:gosec // G201: depTable is selected from a hardcoded list below.
func getChildrenOfDeferredParentsInTx(ctx context.Context, tx *sql.Tx) ([]string, error) {
	hasDeferredParent := false
	for _, issueTable := range []string{"issues", "wisps"} {
		//nolint:gosec // G201: issueTable is hardcoded to "issues" or "wisps"
		var exists int
		err := tx.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT 1 FROM %s
			WHERE defer_until IS NOT NULL
			  AND defer_until > UTC_TIMESTAMP()
			LIMIT 1
		`, issueTable)).Scan(&exists)
		if err == nil {
			hasDeferredParent = true
			break
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if issueTable == "wisps" && isTableNotExistError(err) {
			continue
		}
		return nil, fmt.Errorf("deferred parents: check future-deferred parents from %s: %w", issueTable, err)
	}
	if !hasDeferredParent {
		return nil, nil
	}

	var childIDs []string
	for _, depTable := range []string{"dependencies", "wisp_dependencies"} {
		for _, issueTable := range []string{"issues", "wisps"} {
			targetCol := "depends_on_issue_id"
			if issueTable == "wisps" {
				targetCol = "depends_on_wisp_id"
			}
			rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
				SELECT dep.issue_id
				FROM %s dep
				JOIN %s parent ON parent.id = dep.%s
				WHERE dep.type = 'parent-child'
				  AND parent.defer_until IS NOT NULL
				  AND parent.defer_until > UTC_TIMESTAMP()
			`, depTable, issueTable, targetCol))
			if err != nil {
				if depTable == "wisp_dependencies" && isTableNotExistError(err) {
					break
				}
				if issueTable == "wisps" && isTableNotExistError(err) {
					continue
				}
				return nil, fmt.Errorf("deferred parents: get deferred children from %s/%s: %w", depTable, issueTable, err)
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("deferred parents: scan deferred child from %s/%s: %w", depTable, issueTable, err)
				}
				childIDs = append(childIDs, id)
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("deferred parents: child rows from %s/%s: %w", depTable, issueTable, err)
			}
		}
	}
	return childIDs, nil
}

//nolint:gosec // G201: depTable is hardcoded to "dependencies" or "wisp_dependencies"
func getParentedIDSetInTx(ctx context.Context, tx *sql.Tx, issueIDs []string) (map[string]struct{}, error) {
	parented := make(map[string]struct{})
	if len(issueIDs) == 0 {
		return parented, nil
	}
	for _, depTable := range []string{"dependencies", "wisp_dependencies"} {
		for start := 0; start < len(issueIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(issueIDs) {
				end = len(issueIDs)
			}
			placeholders, args := buildSQLInClause(issueIDs[start:end])
			query := fmt.Sprintf(`
				SELECT issue_id FROM %s
				WHERE type = 'parent-child' AND issue_id IN (%s)
			`, depTable, placeholders)
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				if depTable == "wisp_dependencies" && isTableNotExistError(err) {
					break
				}
				return nil, fmt.Errorf("get parented IDs from %s: %w", depTable, err)
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("get parented IDs: scan: %w", err)
				}
				parented[id] = struct{}{}
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("get parented IDs: rows from %s: %w", depTable, err)
			}
		}
	}
	return parented, nil
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
				WHERE type = 'parent-child' AND %s IN (%s)
			`, depTable, DepTargetExpr, placeholders)
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				// wisp_dependencies table may not exist on pre-migration databases.
				if optionalBlockedTable(depTable) && isTableNotExistError(err) {
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
