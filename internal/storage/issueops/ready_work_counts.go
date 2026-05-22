package issueops

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// GetReadyWorkWithCountsInTx returns ready-work issues with all per-issue
// hydration (labels, dependency records, dep/dependent/comment counts,
// parent ID) attached as a single SQL statement per side (issues + wisps).
// Wisps are merged in via a second mega-query whenever the wisps table is
// non-empty; the wisp branch is skipped entirely after one cheap probe on
// projects that never use wisps.
//
// No per-call hydration fallbacks: both the issues and wisps sides use the
// same six-LEFT-JOIN shape.
func GetReadyWorkWithCountsInTx(ctx context.Context, tx *sql.Tx, filter types.WorkFilter) ([]*types.IssueWithCounts, error) {
	issuePreds, err := buildReadyWorkPredicates(ctx, tx, filter, IssuesFilterTables)
	if err != nil {
		return nil, err
	}
	out, err := runMegaQueryInTx(ctx, tx, IssuesFilterTables, issuePreds.whereSQL, issuePreds.orderBySQL, issuePreds.limitSQL, issuePreds.args)
	if err != nil {
		return nil, err
	}

	empty, probeErr := wispsTableEmptyOrMissingInTx(ctx, tx)
	if probeErr != nil {
		return nil, fmt.Errorf("get ready work with counts: wisp probe: %w", probeErr)
	}
	if empty {
		return out, nil
	}

	wispPreds, err := buildReadyWorkPredicates(ctx, tx, filter, WispsFilterTables)
	if err != nil {
		return nil, err
	}
	wisps, err := runMegaQueryInTx(ctx, tx, WispsFilterTables, wispPreds.whereSQL, wispPreds.orderBySQL, wispPreds.limitSQL, wispPreds.args)
	if err != nil {
		if isTableNotExistError(err) {
			return out, nil
		}
		return nil, err
	}
	if len(wisps) == 0 {
		return out, nil
	}

	// Merge wisp results in, dedup, re-sort by SortPolicy, then trim.
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

// sortIssuesWithCountsByPolicy applies the same WorkFilter.SortPolicy
// ordering used by sortReadyIssues, but operating on []*IssueWithCounts so
// the merged issues+wisps slice stays consistent with the SQL ORDER BY each
// side produced.
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

// readyWorkIssueColumns is IssueSelectColumns rewritten with the "i." alias
// used by the mega-query. Kept in sync with IssueSelectColumns by deriving it
// at init time from the canonical constant.
var readyWorkIssueColumns = func() string {
	raw := strings.ReplaceAll(IssueSelectColumns, "\n", " ")
	raw = strings.ReplaceAll(raw, "\t", " ")
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		parts[i] = "i." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}()

// readyWorkDepJSONObject is the JSON_OBJECT(...) expression embedded inside
// JSON_ARRAYAGG to serialize a Dependency row in the mega-query. The field
// names mirror types.Dependency json tags so the result can be Unmarshal'd
// directly into []*types.Dependency.
//
//   - created_at is DATETIME; Dolt's default JSON serialization renders it
//     as "2006-01-02 15:04:05.000000" which Go's time.Time RFC3339 unmarshal
//     cannot parse, so DATE_FORMAT it to RFC3339.
//   - metadata is JSON; Dolt would embed the parsed JSON value, but
//     types.Dependency.Metadata is a string. CAST AS CHAR converts the JSON
//     value back into its string form so the outer JSON_OBJECT serializes
//     it as a quoted string.
const readyWorkDepJSONObject = `JSON_OBJECT(
	'issue_id', issue_id,
	'depends_on_id', COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external),
	'type', type,
	'created_at', DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'),
	'created_by', created_by,
	'metadata', CAST(metadata AS CHAR),
	'thread_id', thread_id
)`

// scanReadyWorkRowWithCounts scans a single mega-query row, hydrating Labels
// and Dependencies on the embedded Issue from the JSON aggregates and
// populating the per-issue count fields.
func scanReadyWorkRowWithCounts(rows *sql.Rows) (*types.IssueWithCounts, error) {
	// Build a scanner that drains IssueSelectColumns first via ScanIssueFrom,
	// then consumes the appended aggregate columns.
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
	issue, err := ScanIssueFrom(composite)
	if err != nil {
		return nil, fmt.Errorf("scan issue with counts: %w", err)
	}

	if labelsJSON.Valid && labelsJSON.String != "" {
		var labels []string
		if err := json.Unmarshal([]byte(labelsJSON.String), &labels); err != nil {
			return nil, fmt.Errorf("scan issue with counts: parse labels_json: %w", err)
		}
		// The pre-aggregated JSON_ARRAYAGG does not preserve input order;
		// alphabetize so the JSON output is stable and matches the legacy
		// per-batch label SELECT which used ORDER BY issue_id, label.
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

// compositeReadyRow adapts *sql.Rows + a tail of extra destination pointers
// to the IssueScanner interface expected by ScanIssueFrom. ScanIssueFrom
// calls Scan with the IssueSelectColumns destinations; we append the
// aggregate-column destinations transparently.
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
