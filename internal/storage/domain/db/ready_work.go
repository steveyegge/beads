package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dberrors"
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

type readyWorkPredicates struct {
	whereSQL         string
	orderBySQL       string
	limitSQL         string
	args             []any
	deferredChildIDs []string
}

type readyWorkOrder struct {
	sql  string
	args []any
}

func readyWorkPageSize(limit int) int {
	if limit <= 0 {
		return 0
	}
	const minPageSize = 100
	if limit < minPageSize {
		return minPageSize
	}
	return limit
}

func buildReadyWorkOrder(policy types.SortPolicy) readyWorkOrder {
	switch policy {
	case types.SortPolicyOldest:
		return readyWorkOrder{sql: "ORDER BY created_at ASC, id ASC"}
	case types.SortPolicyPriority:
		return readyWorkOrder{sql: "ORDER BY priority ASC, created_at DESC, id ASC"}
	case types.SortPolicyHybrid, "":
		recentCutoff := time.Now().UTC().Add(-48 * time.Hour)
		return readyWorkOrder{
			sql: `ORDER BY
			CASE WHEN created_at >= ? THEN 0 ELSE 1 END ASC,
			CASE WHEN created_at >= ? THEN priority ELSE 999 END ASC,
			created_at ASC, id ASC`,
			args: []any{recentCutoff, recentCutoff},
		}
	default:
		return readyWorkOrder{sql: "ORDER BY priority ASC, created_at DESC, id ASC"}
	}
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

func (r *issueSQLRepositoryImpl) buildReadyWorkPredicates(ctx context.Context, filter types.WorkFilter, tables filterTables) (*readyWorkPredicates, error) {
	var statusClause string
	if filter.Status != "" {
		statusClause = "status = ?"
	} else {
		statusClause = "status IN ('open', 'in_progress')"
	}
	whereClauses := []string{
		statusClause,
		"(pinned = 0 OR pinned IS NULL)",
		"is_blocked = 0",
	}
	if !filter.IncludeEphemeral {
		whereClauses = append(whereClauses, "(ephemeral = 0 OR ephemeral IS NULL)")
	}
	var args []any
	if filter.Status != "" {
		args = append(args, string(filter.Status))
	}

	if filter.Priority != nil {
		whereClauses = append(whereClauses, "priority = ?")
		args = append(args, *filter.Priority)
	}
	if filter.Type != "" {
		whereClauses = append(whereClauses, "issue_type = ?")
		args = append(args, filter.Type)
	} else {
		excludeTypes := readyWorkExcludeTypes(filter.ExcludeTypes)
		placeholders := make([]string, len(excludeTypes))
		for i, t := range excludeTypes {
			placeholders[i] = "?"
			args = append(args, string(t))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("issue_type NOT IN (%s)", strings.Join(placeholders, ",")))
	}
	if filter.Unassigned {
		whereClauses = append(whereClauses, "(assignee IS NULL OR assignee = '')")
	} else if filter.Assignee != nil {
		whereClauses = append(whereClauses, "assignee = ?")
		args = append(args, *filter.Assignee)
	}

	var deferredChildIDs []string
	if !filter.IncludeDeferred {
		whereClauses = append(whereClauses, "(defer_until IS NULL OR defer_until <= UTC_TIMESTAMP())")
		var dcErr error
		deferredChildIDs, dcErr = r.getChildrenOfDeferredParents(ctx)
		if dcErr != nil {
			return nil, fmt.Errorf("get ready work: compute deferred parent children: %w", dcErr)
		}
		if len(deferredChildIDs) > 0 {
			for start := 0; start < len(deferredChildIDs); start += queryBatchSize {
				end := start + queryBatchSize
				if end > len(deferredChildIDs) {
					end = len(deferredChildIDs)
				}
				placeholders, batchArgs := buildInPlaceholders(deferredChildIDs[start:end])
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
	if len(filter.ExcludeLabels) > 0 {
		placeholders := make([]string, len(filter.ExcludeLabels))
		for i, label := range filter.ExcludeLabels {
			placeholders[i] = "?"
			args = append(args, label)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("id NOT IN (SELECT issue_id FROM %s WHERE label IN (%s))", tables.Labels, strings.Join(placeholders, ", ")))
	}

	if filter.ParentID != nil {
		parentID := *filter.ParentID
		descendantIDs, descErr := r.getDescendantIDs(ctx, parentID, 0)
		if descErr != nil {
			return nil, fmt.Errorf("get parent descendants: %w", descErr)
		}
		parentClauses := []string{fmt.Sprintf("(id LIKE CONCAT(?, '.%%') AND id NOT IN (SELECT issue_id FROM %s WHERE type = 'parent-child'))", tables.Dependencies)}
		args = append(args, parentID)
		for start := 0; start < len(descendantIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(descendantIDs) {
				end = len(descendantIDs)
			}
			placeholders, batchArgs := buildInPlaceholders(descendantIDs[start:end])
			parentClauses = append(parentClauses, fmt.Sprintf("id IN (%s)", placeholders))
			args = append(args, batchArgs...)
		}
		whereClauses = append(whereClauses, "("+strings.Join(parentClauses, " OR ")+")")
	}

	if filter.MoleculeID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(id IN (SELECT issue_id FROM %s WHERE type = 'parent-child' AND %s = ?) OR (id LIKE CONCAT(?, '.%%') AND id NOT IN (SELECT issue_id FROM %s WHERE type = 'parent-child')))", tables.Dependencies, depTargetExpr, tables.Dependencies))
		args = append(args, filter.MoleculeID, filter.MoleculeID)
	}

	if filter.HasMetadataKey != "" {
		if err := storage.ValidateMetadataKey(filter.HasMetadataKey); err != nil {
			return nil, err
		}
		whereClauses = append(whereClauses, "JSON_EXTRACT(metadata, ?) IS NOT NULL")
		args = append(args, storage.JSONMetadataPath(filter.HasMetadataKey))
	}

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

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	orderBy := buildReadyWorkOrder(filter.SortPolicy)
	args = append(args, orderBy.args...)

	var limitSQL string
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf("LIMIT %d", filter.Limit)
	}

	return &readyWorkPredicates{
		whereSQL:         whereSQL,
		orderBySQL:       orderBy.sql,
		limitSQL:         limitSQL,
		args:             args,
		deferredChildIDs: deferredChildIDs,
	}, nil
}

//nolint:gosec // G201: whereSQL/orderBySQL built from hardcoded strings and ? placeholders
func (r *issueSQLRepositoryImpl) getReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	preds, err := r.buildReadyWorkPredicates(ctx, filter, issuesFilterTables)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT id FROM issues
		%s
		%s
		%s
	`, preds.whereSQL, preds.orderBySQL, preds.limitSQL)

	issueIDs, err := r.queryReadyIDPage(ctx, query, preds.args)
	if err != nil {
		return nil, err
	}

	issues, err := r.GetByIDs(ctx, issueIDs, domain.IssueTableOpts{})
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

	wisps, wErr := r.getReadyWisps(ctx, filter, preds.deferredChildIDs)
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

func (r *issueSQLRepositoryImpl) getReadyWorkWithCounts(ctx context.Context, filter types.WorkFilter) ([]*types.IssueWithCounts, error) {
	wispDepsExist, err := r.optionalTableExists(ctx, "wisp_dependencies")
	if err != nil {
		return nil, fmt.Errorf("get ready work with counts: wisp dependency probe: %w", err)
	}

	issuePreds, err := r.buildReadyWorkPredicates(ctx, filter, issuesFilterTables)
	if err != nil {
		return nil, err
	}
	out, err := r.runSearchQuery(ctx, issuesFilterTables, issuePreds.whereSQL, issuePreds.orderBySQL, issuePreds.limitSQL, issuePreds.args, wispDepsExist, false)
	if err != nil {
		return nil, err
	}

	empty, probeErr := r.wispsTableEmptyOrMissing(ctx)
	if probeErr != nil {
		return nil, fmt.Errorf("get ready work with counts: wisp probe: %w", probeErr)
	}
	if empty || !wispDepsExist {
		return out, nil
	}

	wispPreds, err := r.buildReadyWorkPredicates(ctx, filter, wispsFilterTables)
	if err != nil {
		return nil, err
	}
	wisps, err := r.runSearchQuery(ctx, wispsFilterTables, wispPreds.whereSQL, wispPreds.orderBySQL, wispPreds.limitSQL, wispPreds.args, true, false)
	if err != nil {
		if dberrors.IsTableNotExist(err) {
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
			return nil, fmt.Errorf("get ready work with counts: id %q exists in both issues and wisps", w.Issue.ID)
		}
		out = append(out, w)
	}
	sortIssuesWithCountsByPolicy(out, filter.SortPolicy)
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func sortIssuesWithCountsByPolicy(items []*types.IssueWithCounts, policy types.SortPolicy) {
	if len(items) <= 1 {
		return
	}
	issues := make([]*types.Issue, 0, len(items))
	for _, item := range items {
		if item == nil || item.Issue == nil {
			continue
		}
		issues = append(issues, item.Issue)
	}
	if len(issues) != len(items) {
		return
	}
	sortReadyIssues(issues, policy)
	byID := make(map[string]int, len(issues))
	for i, iss := range issues {
		byID[iss.ID] = i
	}
	sorted := make([]*types.IssueWithCounts, len(items))
	for _, item := range items {
		sorted[byID[item.Issue.ID]] = item
	}
	copy(items, sorted)
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

func (r *issueSQLRepositoryImpl) getReadyWisps(ctx context.Context, filter types.WorkFilter, deferredChildIDs []string) ([]*types.Issue, error) {
	empty, err := r.wispsTableEmptyOrMissing(ctx)
	if err != nil {
		return nil, fmt.Errorf("search wisps (ready work): probe: %w", err)
	}
	if empty {
		return nil, nil
	}

	wispFilter := readyWorkWispIssueFilter(filter)
	if filter.Limit <= 0 {
		wispFilter.Limit = 0
		wisps, err := r.searchTable(ctx, "", wispFilter, wispsFilterTables)
		if err != nil {
			if dberrors.IsTableNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("search wisps (ready work): %w", err)
		}
		return r.filterReadyWisps(ctx, filter, wisps, deferredChildIDs)
	}

	pageSize := readyWorkPageSize(filter.Limit)
	orderBy := buildReadyWorkOrder(filter.SortPolicy)
	ready := make([]*types.Issue, 0, filter.Limit)
	for offset := 0; len(ready) < filter.Limit; offset += pageSize {
		pageIDs, err := r.queryReadyWispIssueIDPage(ctx, wispFilter, !filter.IncludeDeferred, orderBy, pageSize, offset)
		if err != nil {
			if dberrors.IsTableNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("search wisps (ready work): %w", err)
		}
		if len(pageIDs) == 0 {
			break
		}

		pageWisps, err := r.getWispIssuesByIDsInOrder(ctx, pageIDs)
		if err != nil {
			return nil, fmt.Errorf("search wisps (ready work): %w", err)
		}
		pageReady, err := r.filterReadyWisps(ctx, filter, pageWisps, deferredChildIDs)
		if err != nil {
			return nil, err
		}
		for _, wisp := range pageReady {
			ready = append(ready, wisp)
			if len(ready) >= filter.Limit {
				break
			}
		}
		if len(pageIDs) < pageSize {
			break
		}
	}
	return ready, nil
}

func (r *issueSQLRepositoryImpl) queryReadyWispIssueIDPage(ctx context.Context, filter types.IssueFilter, excludeDeferred bool, orderBy readyWorkOrder, limit, offset int) ([]string, error) {
	fromSQL, labelWhere, labelArgs, labelDriven, filterForClauses := buildLabelDrivenSearch(filter, wispsFilterTables)
	whereClauses, args, err := buildIssueFilterClauses("", filterForClauses, wispsFilterTables)
	if err != nil {
		return nil, err
	}
	if len(labelWhere) > 0 {
		whereClauses = append(labelWhere, whereClauses...)
		args = append(labelArgs, args...)
	}
	if excludeDeferred {
		whereClauses = append(whereClauses, "(defer_until IS NULL OR defer_until <= UTC_TIMESTAMP())")
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	selectSQL := "SELECT "
	if labelDriven {
		selectSQL = "SELECT DISTINCT "
	}
	args = append(args, orderBy.args...)
	//nolint:gosec // G201: SQL fragments are fixed table/column names and parameterized filters; limit/offset are ints.
	query := fmt.Sprintf(`%sid FROM %s %s %s LIMIT %d OFFSET %d`,
		selectSQL, fromSQL, whereSQL, orderBy.sql, limit, offset)

	rows, err := r.runner.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search wisps: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("search wisps: scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search wisps: rows: %w", err)
	}
	return ids, nil
}

func (r *issueSQLRepositoryImpl) getWispIssuesByIDsInOrder(ctx context.Context, ids []string) ([]*types.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	issues, err := r.GetByIDs(ctx, ids, domain.IssueTableOpts{UseWispsTable: true})
	if err != nil {
		return nil, err
	}
	issueMap := make(map[string]*types.Issue, len(issues))
	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}
	ordered := make([]*types.Issue, 0, len(ids))
	for _, id := range ids {
		if issue, ok := issueMap[id]; ok {
			ordered = append(ordered, issue)
		}
	}
	return ordered, nil
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

func (r *issueSQLRepositoryImpl) filterReadyWisps(ctx context.Context, filter types.WorkFilter, wisps []*types.Issue, deferredChildIDs []string) ([]*types.Issue, error) {
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
		descendantIDs, err := r.getDescendantIDs(ctx, parentID, 0)
		if err != nil {
			return nil, fmt.Errorf("get wisp parent descendants: %w", err)
		}
		descendantSet := make(map[string]struct{}, len(descendantIDs))
		for _, id := range descendantIDs {
			descendantSet[id] = struct{}{}
		}
		parentedSet, err := r.getParentedIDSet(ctx, wispIDs)
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
		for _, id := range deferredChildIDs {
			excluded[id] = struct{}{}
		}
	}

	for start := 0; start < len(wispIDs); start += queryBatchSize {
		end := start + queryBatchSize
		if end > len(wispIDs) {
			end = len(wispIDs)
		}
		placeholders, args := buildInPlaceholders(wispIDs[start:end])
		//nolint:gosec // G201: only IN-clause placeholders are formatted in.
		rows, err := r.runner.QueryContext(ctx, fmt.Sprintf(`
			SELECT id FROM wisps WHERE id IN (%s) AND is_blocked = 1
		`, placeholders), args...)
		if err != nil {
			return nil, fmt.Errorf("get ready work: filter blocked wisps: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan blocked wisp: %w", err)
			}
			excluded[id] = struct{}{}
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("blocked wisp rows: %w", err)
		}
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

func (r *issueSQLRepositoryImpl) queryReadyIDPage(ctx context.Context, query string, args []any) ([]string, error) {
	rows, err := r.runner.QueryContext(ctx, query, args...)
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

//nolint:gosec // G201: depTable/issueTable are hardcoded values.
func (r *issueSQLRepositoryImpl) getChildrenOfDeferredParents(ctx context.Context) ([]string, error) {
	hasDeferredParent := false
	for _, issueTable := range []string{"issues", "wisps"} {
		var exists int
		err := r.runner.QueryRowContext(ctx, fmt.Sprintf(`
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
		if issueTable == "wisps" && dberrors.IsTableNotExist(err) {
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
			rows, err := r.runner.QueryContext(ctx, fmt.Sprintf(`
				SELECT dep.issue_id
				FROM %s dep
				JOIN %s parent ON parent.id = dep.%s
				WHERE dep.type = 'parent-child'
				  AND parent.defer_until IS NOT NULL
				  AND parent.defer_until > UTC_TIMESTAMP()
			`, depTable, issueTable, targetCol))
			if err != nil {
				if depTable == "wisp_dependencies" && dberrors.IsTableNotExist(err) {
					break
				}
				if issueTable == "wisps" && dberrors.IsTableNotExist(err) {
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

//nolint:gosec // G201: depTable is hardcoded.
func (r *issueSQLRepositoryImpl) getParentedIDSet(ctx context.Context, issueIDs []string) (map[string]struct{}, error) {
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
			placeholders, args := buildInPlaceholders(issueIDs[start:end])
			query := fmt.Sprintf(`
				SELECT issue_id FROM %s
				WHERE type = 'parent-child' AND issue_id IN (%s)
			`, depTable, placeholders)
			rows, err := r.runner.QueryContext(ctx, query, args...)
			if err != nil {
				if depTable == "wisp_dependencies" && dberrors.IsTableNotExist(err) {
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

func (r *issueSQLRepositoryImpl) getDescendantIDs(ctx context.Context, rootID string, maxDepth int) ([]string, error) {
	if rootID == "" {
		return nil, nil
	}

	queryDescendants := func(includeWisps bool) ([]string, bool, error) {
		edgeQuery := fmt.Sprintf(`
			SELECT issue_id, %s FROM dependencies WHERE type = 'parent-child'
		`, depTargetExpr)
		if includeWisps {
			edgeQuery += fmt.Sprintf(`
			UNION ALL
			SELECT issue_id, %s FROM wisp_dependencies WHERE type = 'parent-child'
		`, depTargetExpr)
		}

		//nolint:gosec // G201: edgeQuery is built from hardcoded SQL plus depTargetExpr (no user input)
		query := fmt.Sprintf(`
			WITH RECURSIVE
			parent_edges(issue_id, depends_on_id) AS (
				%s
			),
			descendants(id, depth, path) AS (
				SELECT issue_id, 1, CONCAT(',', ?, ',', issue_id, ',')
				FROM parent_edges
				WHERE depends_on_id = ?
				UNION ALL
				SELECT e.issue_id, d.depth + 1, CONCAT(d.path, e.issue_id, ',')
				FROM parent_edges e
				JOIN descendants d ON e.depends_on_id = d.id
				WHERE (? <= 0 OR d.depth < ?)
				  AND LOCATE(CONCAT(',', e.issue_id, ','), d.path) = 0
			)
			SELECT id, depth FROM descendants WHERE id <> ?
		`, edgeQuery)

		rows, err := r.runner.QueryContext(ctx, query, rootID, rootID, maxDepth, maxDepth, rootID)
		if err != nil {
			return nil, false, err
		}
		defer func() { _ = rows.Close() }()

		var result []string
		reachedMaxDepth := false
		for rows.Next() {
			var id string
			var depth int
			if err := rows.Scan(&id, &depth); err != nil {
				return nil, false, fmt.Errorf("scan descendant: %w", err)
			}
			result = append(result, id)
			if maxDepth > 0 && depth >= maxDepth {
				reachedMaxDepth = true
			}
		}
		if err := rows.Err(); err != nil {
			return nil, false, fmt.Errorf("descendant rows: %w", err)
		}
		return result, reachedMaxDepth, nil
	}

	result, reachedMaxDepth, err := queryDescendants(true)
	if err != nil {
		if !dberrors.IsTableNotExist(err) {
			return nil, err
		}
		result, reachedMaxDepth, err = queryDescendants(false)
		if err != nil {
			return nil, err
		}
	}
	if reachedMaxDepth {
		return nil, fmt.Errorf("parent descendant traversal for %s reached max depth %d", rootID, maxDepth)
	}
	return result, nil
}
