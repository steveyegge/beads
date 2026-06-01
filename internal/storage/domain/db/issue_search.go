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
	"github.com/steveyegge/beads/internal/types"
)

const queryBatchSize = 200

type filterTables struct {
	Main         string
	Labels       string
	Dependencies string
	Comments     string
}

var (
	issuesFilterTables = filterTables{Main: "issues", Labels: "labels", Dependencies: "dependencies", Comments: "comments"}
	wispsFilterTables  = filterTables{Main: "wisps", Labels: "wisp_labels", Dependencies: "wisp_dependencies", Comments: "wisp_comments"}
)

func (r *issueSQLRepositoryImpl) searchAcrossIssuesAndWisps(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	if filter.Ephemeral != nil && *filter.Ephemeral {
		results, err := r.searchTable(ctx, query, filter, wispsFilterTables)
		if err != nil && !dberrors.IsTableNotExist(err) {
			return nil, fmt.Errorf("search wisps (ephemeral filter): %w", err)
		}
		if len(results) > 0 {
			return results, nil
		}
	}

	results, err := r.searchTable(ctx, query, filter, issuesFilterTables)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}

	if filter.SkipWisps {
		return results, nil
	}

	if filter.Ephemeral == nil || !*filter.Ephemeral {
		empty, probeErr := r.wispsTableEmptyOrMissing(ctx)
		if probeErr != nil {
			return nil, fmt.Errorf("search wisps (merge): probe: %w", probeErr)
		}
		if empty {
			return results, nil
		}
		wispResults, wispErr := r.searchTable(ctx, query, filter, wispsFilterTables)
		if wispErr != nil && !dberrors.IsTableNotExist(wispErr) {
			return nil, fmt.Errorf("search wisps (merge): %w", wispErr)
		}
		if len(wispResults) > 0 {
			seen := make(map[string]bool, len(results))
			for _, issue := range results {
				seen[issue.ID] = true
			}
			for _, issue := range wispResults {
				if seen[issue.ID] {
					return nil, fmt.Errorf("id %q exists in both issues and wisps", issue.ID)
				}
				results = append(results, issue)
			}
		}
	}

	return results, nil
}

func (r *issueSQLRepositoryImpl) searchTable(ctx context.Context, query string, filter types.IssueFilter, tables filterTables) ([]*types.Issue, error) {
	fromSQL, labelWhere, labelArgs, labelDriven, filterForClauses := buildLabelDrivenSearch(filter, tables)
	whereClauses, args, err := buildIssueFilterClauses(query, filterForClauses, tables)
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

	if filter.Limit > 0 && !filter.NoIDShrink {
		return r.searchTablePatternB(ctx, fromSQL, whereSQL, args, filter, tables, labelDriven)
	}

	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	selectSQL := "SELECT "
	if labelDriven {
		selectSQL = "SELECT DISTINCT "
	}
	//nolint:gosec // G201: SQL fragments from fixed table names and parameterized filters.
	querySQL := fmt.Sprintf(`%s%s FROM %s %s ORDER BY priority ASC, created_at DESC, id ASC %s`,
		selectSQL, issueSelectColumns, fromSQL, whereSQL, limitSQL)

	rows, err := r.runner.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", tables.Main, err)
	}

	var issues []*types.Issue
	seen := make(map[string]bool)
	for rows.Next() {
		issue, scanErr := scanIssue(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("search %s: scan: %w", tables.Main, scanErr)
		}
		if seen[issue.ID] {
			continue
		}
		seen[issue.ID] = true
		issues = append(issues, issue)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search %s: rows: %w", tables.Main, err)
	}

	if err := r.hydrateIssues(ctx, issues, tables, filter.IncludeDependencies, filter.SkipLabels); err != nil {
		return nil, fmt.Errorf("search %s: hydrate: %w", tables.Main, err)
	}

	return issues, nil
}

func (r *issueSQLRepositoryImpl) searchTablePatternB(ctx context.Context, fromSQL, whereSQL string, args []any, filter types.IssueFilter, tables filterTables, labelDriven bool) ([]*types.Issue, error) {
	idSelect := "SELECT "
	if labelDriven {
		idSelect = "SELECT DISTINCT "
	}
	//nolint:gosec // G201: SQL fragments from fixed table names and parameterized filters.
	idQuery := fmt.Sprintf(`%s%s.id FROM %s %s ORDER BY %s.priority ASC, %s.created_at DESC, %s.id ASC LIMIT %d`,
		idSelect, tables.Main, fromSQL, whereSQL, tables.Main, tables.Main, tables.Main, filter.Limit)

	rows, err := r.runner.QueryContext(ctx, idQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search %s (id scan): %w", tables.Main, err)
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("search %s (id scan): scan: %w", tables.Main, err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search %s (id scan): rows: %w", tables.Main, err)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	fetchArgs := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		fetchArgs[i] = id
	}
	//nolint:gosec // G201: tables.Main is "issues" or "wisps"; placeholders are ?.
	fetchSQL := fmt.Sprintf(`SELECT %s FROM %s WHERE id IN (%s)`,
		issueSelectColumns, tables.Main, strings.Join(placeholders, ","))

	fetchRows, err := r.runner.QueryContext(ctx, fetchSQL, fetchArgs...)
	if err != nil {
		return nil, fmt.Errorf("search %s (hydrate): %w", tables.Main, err)
	}

	issueMap := make(map[string]*types.Issue, len(ids))
	for fetchRows.Next() {
		issue, scanErr := scanIssue(fetchRows)
		if scanErr != nil {
			_ = fetchRows.Close()
			return nil, fmt.Errorf("search %s (hydrate): scan: %w", tables.Main, scanErr)
		}
		issueMap[issue.ID] = issue
	}
	_ = fetchRows.Close()
	if err := fetchRows.Err(); err != nil {
		return nil, fmt.Errorf("search %s (hydrate): rows: %w", tables.Main, err)
	}

	issues := make([]*types.Issue, 0, len(ids))
	for _, id := range ids {
		if issue, ok := issueMap[id]; ok {
			issues = append(issues, issue)
		}
	}

	if err := r.hydrateIssues(ctx, issues, tables, filter.IncludeDependencies, filter.SkipLabels); err != nil {
		return nil, fmt.Errorf("search %s (pattern B): hydrate: %w", tables.Main, err)
	}

	return issues, nil
}

func (r *issueSQLRepositoryImpl) hydrateIssues(ctx context.Context, issues []*types.Issue, tables filterTables, includeDeps bool, skipLabels bool) error {
	if len(issues) == 0 {
		return nil
	}

	ids := make([]string, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID
	}

	if !skipLabels {
		labelMap, err := r.getLabelsFromTable(ctx, tables.Labels, ids)
		if err != nil {
			return fmt.Errorf("hydrate labels: %w", err)
		}
		for _, issue := range issues {
			if labels, ok := labelMap[issue.ID]; ok {
				issue.Labels = labels
			}
		}
	}

	if includeDeps {
		depMap, err := r.getDependencyRecordsFromTable(ctx, tables.Dependencies, ids)
		if err != nil {
			return fmt.Errorf("hydrate dependencies: %w", err)
		}
		for _, issue := range issues {
			if deps, ok := depMap[issue.ID]; ok {
				issue.Dependencies = deps
			}
		}
	}

	return nil
}

//nolint:gosec // G201: labelTable is "labels" or "wisp_labels" (hardcoded by callers).
func (r *issueSQLRepositoryImpl) getLabelsFromTable(ctx context.Context, labelTable string, ids []string) (map[string][]string, error) {
	result := make(map[string][]string)
	for start := 0; start < len(ids); start += queryBatchSize {
		end := start + queryBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			placeholders[i] = "?"
			args[i] = id
		}
		rows, err := r.runner.QueryContext(ctx, fmt.Sprintf(
			`SELECT issue_id, label FROM %s WHERE issue_id IN (%s) ORDER BY issue_id, label`,
			labelTable, strings.Join(placeholders, ",")), args...)
		if err != nil {
			return nil, fmt.Errorf("get labels from %s: %w", labelTable, err)
		}
		for rows.Next() {
			var issueID, label string
			if err := rows.Scan(&issueID, &label); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("get labels: scan: %w", err)
			}
			result[issueID] = append(result[issueID], label)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get labels: rows: %w", err)
		}
	}
	return result, nil
}

//nolint:gosec // G201: depTable is "dependencies" or "wisp_dependencies" (hardcoded by callers).
func (r *issueSQLRepositoryImpl) getDependencyRecordsFromTable(ctx context.Context, depTable string, ids []string) (map[string][]*types.Dependency, error) {
	result := make(map[string][]*types.Dependency)
	for start := 0; start < len(ids); start += queryBatchSize {
		end := start + queryBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			placeholders[i] = "?"
			args[i] = id
		}
		rows, err := r.runner.QueryContext(ctx, fmt.Sprintf(
			`SELECT issue_id, %s AS depends_on_id, type, created_at, created_by, metadata, thread_id
			 FROM %s WHERE issue_id IN (%s) ORDER BY issue_id`,
			depTargetExpr, depTable, strings.Join(placeholders, ",")), args...)
		if err != nil {
			return nil, fmt.Errorf("get dep records from %s: %w", depTable, err)
		}
		for rows.Next() {
			dep, scanErr := scanDepRow(rows)
			if scanErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("get dep records: scan: %w", scanErr)
			}
			result[dep.IssueID] = append(result[dep.IssueID], dep)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get dep records: rows: %w", err)
		}
	}
	return result, nil
}

func scanDepRow(rows *sql.Rows) (*types.Dependency, error) {
	var dep types.Dependency
	var createdAt sql.NullTime
	var createdBy, metadata, threadID sql.NullString
	if err := rows.Scan(&dep.IssueID, &dep.DependsOnID, &dep.Type, &createdAt, &createdBy, &metadata, &threadID); err != nil {
		return nil, err
	}
	if createdAt.Valid {
		dep.CreatedAt = createdAt.Time
	}
	if createdBy.Valid {
		dep.CreatedBy = createdBy.String
	}
	if metadata.Valid {
		dep.Metadata = metadata.String
	}
	if threadID.Valid {
		dep.ThreadID = threadID.String
	}
	return &dep, nil
}

func (r *issueSQLRepositoryImpl) wispsTableEmptyOrMissing(ctx context.Context) (bool, error) {
	var probe int
	err := r.runner.QueryRowContext(ctx, "SELECT 1 FROM wisps LIMIT 1").Scan(&probe)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, sql.ErrNoRows):
		return true, nil
	case dberrors.IsTableNotExist(err):
		return true, nil
	default:
		return false, err
	}
}

func buildLabelDrivenSearch(filter types.IssueFilter, tables filterTables) (string, []string, []any, bool, types.IssueFilter) {
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
	var args []any

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

func buildIssueFilterClauses(query string, filter types.IssueFilter, tables filterTables) ([]string, []any, error) {
	var whereClauses []string
	var args []any

	if query != "" {
		lowerQuery := strings.ToLower(query)
		if looksLikeIssueID(query) {
			whereClauses = append(whereClauses, "(id = ? OR id LIKE ? OR LOWER(title) LIKE ? OR LOWER(external_ref) LIKE ?)")
			args = append(args, lowerQuery, lowerQuery+"%", "%"+lowerQuery+"%", "%"+lowerQuery+"%")
		} else {
			whereClauses = append(whereClauses, "(LOWER(title) LIKE ? OR id LIKE ?)")
			pattern := "%" + lowerQuery + "%"
			args = append(args, pattern, pattern)
		}
	}

	if filter.TitleSearch != "" {
		whereClauses = append(whereClauses, "LOWER(title) LIKE ?")
		args = append(args, "%"+strings.ToLower(filter.TitleSearch)+"%")
	}
	if filter.TitleContains != "" {
		whereClauses = append(whereClauses, "LOWER(title) LIKE ?")
		args = append(args, "%"+strings.ToLower(filter.TitleContains)+"%")
	}
	if filter.DescriptionContains != "" {
		whereClauses = append(whereClauses, "LOWER(description) LIKE ?")
		args = append(args, "%"+strings.ToLower(filter.DescriptionContains)+"%")
	}
	if filter.NotesContains != "" {
		whereClauses = append(whereClauses, "LOWER(notes) LIKE ?")
		args = append(args, "%"+strings.ToLower(filter.NotesContains)+"%")
	}
	if filter.ExternalRefContains != "" {
		whereClauses = append(whereClauses, "LOWER(external_ref) LIKE ?")
		args = append(args, "%"+strings.ToLower(filter.ExternalRefContains)+"%")
	}

	if filter.Status != nil {
		whereClauses = append(whereClauses, "status = ?")
		args = append(args, *filter.Status)
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, string(s))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(filter.ExcludeStatus) > 0 {
		placeholders := make([]string, len(filter.ExcludeStatus))
		for i, s := range filter.ExcludeStatus {
			placeholders[i] = "?"
			args = append(args, string(s))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("status NOT IN (%s)", strings.Join(placeholders, ",")))
	}

	if filter.IssueType != nil {
		whereClauses = append(whereClauses, "issue_type = ?")
		args = append(args, *filter.IssueType)
	}
	if len(filter.ExcludeTypes) > 0 {
		placeholders := make([]string, len(filter.ExcludeTypes))
		for i, t := range filter.ExcludeTypes {
			placeholders[i] = "?"
			args = append(args, string(t))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("issue_type NOT IN (%s)", strings.Join(placeholders, ",")))
	}

	if filter.Assignee != nil {
		whereClauses = append(whereClauses, "assignee = ?")
		args = append(args, *filter.Assignee)
	}

	if filter.Priority != nil {
		whereClauses = append(whereClauses, "priority = ?")
		args = append(args, *filter.Priority)
	}
	if filter.PriorityMin != nil {
		whereClauses = append(whereClauses, "priority >= ?")
		args = append(args, *filter.PriorityMin)
	}
	if filter.PriorityMax != nil {
		whereClauses = append(whereClauses, "priority <= ?")
		args = append(args, *filter.PriorityMax)
	}

	if len(filter.IDs) > 0 {
		placeholders := make([]string, len(filter.IDs))
		for i, id := range filter.IDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("id IN (%s)", strings.Join(placeholders, ", ")))
	}
	if filter.IDPrefix != "" {
		whereClauses = append(whereClauses, "id LIKE ?")
		args = append(args, filter.IDPrefix+"%")
	}
	if filter.SpecIDPrefix != "" {
		whereClauses = append(whereClauses, "spec_id LIKE ?")
		args = append(args, filter.SpecIDPrefix+"%")
	}

	if filter.ParentID != nil {
		parentID := *filter.ParentID
		whereClauses = append(whereClauses, fmt.Sprintf("(id IN (SELECT issue_id FROM %s WHERE type = 'parent-child' AND %s = ?) OR (id LIKE CONCAT(?, '.%%') AND id NOT IN (SELECT issue_id FROM %s WHERE type = 'parent-child')))", tables.Dependencies, depTargetExpr, tables.Dependencies))
		args = append(args, parentID, parentID)
	}
	if filter.NoParent {
		whereClauses = append(whereClauses, fmt.Sprintf("id NOT IN (SELECT issue_id FROM %s WHERE type = 'parent-child')", tables.Dependencies))
	}

	if filter.MolType != nil {
		whereClauses = append(whereClauses, "mol_type = ?")
		args = append(args, string(*filter.MolType))
	}
	if filter.WispType != nil {
		whereClauses = append(whereClauses, "wisp_type = ?")
		args = append(args, string(*filter.WispType))
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
		whereClauses = append(whereClauses, fmt.Sprintf("id IN (SELECT issue_id FROM %s WHERE label IN (%s))", tables.Labels, strings.Join(placeholders, ", ")))
	}
	if len(filter.ExcludeLabels) > 0 {
		placeholders := make([]string, len(filter.ExcludeLabels))
		for i, label := range filter.ExcludeLabels {
			placeholders[i] = "?"
			args = append(args, label)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("id NOT IN (SELECT issue_id FROM %s WHERE label IN (%s))", tables.Labels, strings.Join(placeholders, ", ")))
	}
	if filter.NoLabels {
		whereClauses = append(whereClauses, fmt.Sprintf("id NOT IN (SELECT DISTINCT issue_id FROM %s)", tables.Labels))
	}

	if filter.Pinned != nil {
		if *filter.Pinned {
			whereClauses = append(whereClauses, "pinned = 1")
		} else {
			whereClauses = append(whereClauses, "(pinned = 0 OR pinned IS NULL)")
		}
	}
	if filter.SourceRepo != nil {
		whereClauses = append(whereClauses, "source_repo = ?")
		args = append(args, *filter.SourceRepo)
	}
	if filter.Ephemeral != nil {
		if *filter.Ephemeral {
			whereClauses = append(whereClauses, "ephemeral = 1")
		} else {
			whereClauses = append(whereClauses, "(ephemeral = 0 OR ephemeral IS NULL)")
		}
	}
	if filter.IsTemplate != nil {
		if *filter.IsTemplate {
			whereClauses = append(whereClauses, "is_template = 1")
		} else {
			whereClauses = append(whereClauses, "(is_template = 0 OR is_template IS NULL)")
		}
	}

	if filter.EmptyDescription {
		whereClauses = append(whereClauses, "(description IS NULL OR description = '')")
	}
	if filter.NoAssignee {
		whereClauses = append(whereClauses, "(assignee IS NULL OR assignee = '')")
	}

	if filter.CreatedAfter != nil {
		whereClauses = append(whereClauses, "created_at > ?")
		args = append(args, filter.CreatedAfter.Format(time.RFC3339))
	}
	if filter.CreatedBefore != nil {
		whereClauses = append(whereClauses, "created_at < ?")
		args = append(args, filter.CreatedBefore.Format(time.RFC3339))
	}
	if filter.UpdatedAfter != nil {
		whereClauses = append(whereClauses, "updated_at > ?")
		args = append(args, filter.UpdatedAfter.Format(time.RFC3339))
	}
	if filter.UpdatedBefore != nil {
		whereClauses = append(whereClauses, "updated_at < ?")
		args = append(args, filter.UpdatedBefore.Format(time.RFC3339))
	}
	if filter.ClosedAfter != nil {
		whereClauses = append(whereClauses, "closed_at > ?")
		args = append(args, filter.ClosedAfter.Format(time.RFC3339))
	}
	if filter.ClosedBefore != nil {
		whereClauses = append(whereClauses, "closed_at < ?")
		args = append(args, filter.ClosedBefore.Format(time.RFC3339))
	}
	if filter.StartedAfter != nil {
		whereClauses = append(whereClauses, "started_at > ?")
		args = append(args, filter.StartedAfter.Format(time.RFC3339))
	}
	if filter.StartedBefore != nil {
		whereClauses = append(whereClauses, "started_at < ?")
		args = append(args, filter.StartedBefore.Format(time.RFC3339))
	}
	if filter.DeferAfter != nil {
		whereClauses = append(whereClauses, "defer_until > ?")
		args = append(args, filter.DeferAfter.Format(time.RFC3339))
	}
	if filter.DeferBefore != nil {
		whereClauses = append(whereClauses, "defer_until < ?")
		args = append(args, filter.DeferBefore.Format(time.RFC3339))
	}
	if filter.DueAfter != nil {
		whereClauses = append(whereClauses, "due_at > ?")
		args = append(args, filter.DueAfter.Format(time.RFC3339))
	}
	if filter.DueBefore != nil {
		whereClauses = append(whereClauses, "due_at < ?")
		args = append(args, filter.DueBefore.Format(time.RFC3339))
	}

	if filter.Deferred {
		whereClauses = append(whereClauses, "(defer_until IS NOT NULL OR status = ?)")
		args = append(args, types.StatusDeferred)
	}
	if filter.Overdue {
		whereClauses = append(whereClauses, "due_at IS NOT NULL AND due_at < ? AND status != ?")
		args = append(args, time.Now().UTC().Format(time.RFC3339), types.StatusClosed)
	}

	if filter.HasMetadataKey != "" {
		if err := storage.ValidateMetadataKey(filter.HasMetadataKey); err != nil {
			return nil, nil, err
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
				return nil, nil, err
			}
			whereClauses = append(whereClauses, "JSON_UNQUOTE(JSON_EXTRACT(metadata, ?)) = ?")
			args = append(args, storage.JSONMetadataPath(k), filter.MetadataFields[k])
		}
	}

	return whereClauses, args, nil
}

func looksLikeIssueID(query string) bool {
	idx := strings.Index(query, "-")
	if idx <= 0 || idx >= len(query)-1 {
		return false
	}
	if strings.Contains(query, " ") {
		return false
	}
	for _, c := range query {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
